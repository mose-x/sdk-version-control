package sdk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// versionCache is a two-tier (in-memory + on-disk) cache of remote version
// lists keyed by SDK type. It makes the "refresh versions" UX feel instant:
// the frontend always gets a list back immediately (from memory, or from the
// on-disk cache after an app restart), while the backend refreshes in the
// background and pushes an event when a fresher list lands.
//
// On-disk cache survives restarts, so even the first launch after a restart
// can show a stale-but-useful list instead of a spinner. No TTL is enforced --
// a stale cache is strictly better than nothing, and every explicit "refresh"
// click triggers a background fetch that overwrites the cache.
//
// The on-disk root dir is set once via InitVersionCacheDir before first use;
// until then the cache is memory-only (calls degrade gracefully).
type versionCache struct {
	mu      sync.RWMutex
	entries map[SdkType]versionCacheEntry
	dir     string
}

type versionCacheEntry struct {
	Versions  []VersionInfo `json:"versions"`
	FetchedAt time.Time     `json:"fetched_at"`
}

var globalVersionCache = &versionCache{entries: make(map[SdkType]versionCacheEntry)}

// InitVersionCacheDir sets the on-disk root directory for the version cache.
// Must be called once at startup (after the config/data dir is known). Safe to
// call multiple times; the latest dir wins. If dir is empty, the cache stays
// memory-only.
func InitVersionCacheDir(dir string) {
	globalVersionCache.mu.Lock()
	defer globalVersionCache.mu.Unlock()
	globalVersionCache.dir = dir
}

func (c *versionCache) cacheFile(t SdkType) string {
	if c.dir == "" {
		return ""
	}
	return filepath.Join(c.dir, "versions_"+string(t)+".json")
}

// GetCachedVersions returns the cached version list for sdkType and whether a
// cache entry exists. It checks memory first; on a memory miss it falls back
// to the on-disk JSON file (so a freshly restarted app still serves the last
// known list). The returned slice is a defensive copy.
func GetCachedVersions(t SdkType) ([]VersionInfo, bool) {
	globalVersionCache.mu.RLock()
	e, ok := globalVersionCache.entries[t]
	globalVersionCache.mu.RUnlock()
	if ok {
		return copyVersions(e.Versions), true
	}
	// Memory miss: try the on-disk file (populates memory as a side effect).
	return globalVersionCache.loadFromDisk(t)
}

// loadFromDisk reads the on-disk cache file for t. On success it also stores
// the entry in memory (so subsequent reads skip the disk) and returns the list.
// H5: Re-checks memory after acquiring the write lock to prevent TOCTOU:
// a concurrent SetCachedVersions might have populated the in-memory entry
// between the RUnlock in GetCachedVersions and the Lock here; in that case
// the fresher in-memory entry wins over the stale disk data.
func (c *versionCache) loadFromDisk(t SdkType) ([]VersionInfo, bool) {
	path := c.cacheFile(t)
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var e versionCacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, false
	}
	c.mu.Lock()
	// Re-check memory: a concurrent SetCachedVersions might have populated it.
	if memEntry, ok := c.entries[t]; ok {
		c.mu.Unlock()
		return copyVersions(memEntry.Versions), true
	}
	c.entries[t] = e
	c.mu.Unlock()
	return copyVersions(e.Versions), true
}

// SetCachedVersions stores versions for sdkType, replacing any existing entry.
// It writes to both memory and disk (so the cache survives a restart). The
// slice is copied so later mutation by the caller does not affect the cache.
// A disk write failure is logged but does not affect the in-memory cache --
// the next refresh will retry the write.
func SetCachedVersions(t SdkType, versions []VersionInfo) {
	e := versionCacheEntry{Versions: copyVersions(versions), FetchedAt: time.Now()}
	globalVersionCache.mu.Lock()
	globalVersionCache.entries[t] = e
	dir := globalVersionCache.dir
	globalVersionCache.mu.Unlock()
	if dir == "" {
		return
	}
	path := globalVersionCache.cacheFile(t)
	if path == "" {
		return
	}
	// Best-effort persist; a write failure only means the next restart starts
	// from the previous on-disk cache, which is acceptable.
	_ = os.MkdirAll(dir, 0755)
	if data, err := json.Marshal(e); err == nil {
		_ = os.WriteFile(path, data, 0644)
	}
}

func copyVersions(in []VersionInfo) []VersionInfo {
	if in == nil {
		return nil
	}
	out := make([]VersionInfo, len(in))
	copy(out, in)
	return out
}

// refreshState throttles background version refreshes per SDK. Every cache hit
// in GetRemoteVersions triggers a background refresh; without throttling, rapidly
// switching SDK panels (py -> rust -> py -> ...) would spawn a goroutine per
// switch, each emitting an "install:versions-refreshed" event. That floods the
// frontend with events and amplifies any stale-listener timing issue into
// visible list corruption. A 30s cooldown per SDK keeps the list fresh enough
// for normal use while capping the goroutine/event rate.
var refreshState = struct {
	sync.Mutex
	last map[SdkType]time.Time
}{last: make(map[SdkType]time.Time)}

// ShouldRefreshVersions reports whether a background refresh should be started
// for t now. It returns false when t was refreshed within the cooldown window
// (so rapid re-entry is a no-op), and records the time when it returns true.
func ShouldRefreshVersions(t SdkType) bool {
	const cooldown = 30 * time.Second
	refreshState.Lock()
	defer refreshState.Unlock()
	last, ok := refreshState.last[t]
	if ok && time.Since(last) < cooldown {
		return false
	}
	refreshState.last[t] = time.Now()
	return true
}

// LookupCachedDownloadURL returns the cached download URL and filename for the
// given SDK type + version, or ("", "", false) when not cached. It lets
// GetDownloadURL skip a fresh FetchRemoteVersions round-trip when the version
// list was already fetched (e.g. the user just opened the version panel, which
// populated the cache, then clicked Install on one of the listed versions).
//
// On a cache miss the caller MUST fall back to FetchRemoteVersions -- a cache
// miss is the normal first-install path and is not an error.
func LookupCachedDownloadURL(t SdkType, version string) (string, string, bool) {
	versions, ok := GetCachedVersions(t)
	if !ok {
		return "", "", false
	}
	for _, v := range versions {
		if v.Version == version {
			return v.DownloadURL, v.FileName, true
		}
	}
	return "", "", false
}
