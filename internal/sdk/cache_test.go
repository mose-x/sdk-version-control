package sdk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resetGlobalCacheEntry removes the in-memory entry for t and (when a cache
// dir is active) its on-disk file, so cache tests start from a clean state
// regardless of test ordering.
func resetGlobalCacheEntry(t *testing.T, sdkType SdkType) {
	t.Helper()
	globalVersionCache.mu.Lock()
	delete(globalVersionCache.entries, sdkType)
	globalVersionCache.mu.Unlock()
	if path := globalVersionCache.cacheFile(sdkType); path != "" {
		_ = os.Remove(path)
	}
}

// TestSetCachedVersionsEmptyDoesNotOverwrite guards the defensive rule that
// an empty fetch result must never clobber the last good version list, in
// memory or on disk. Regression test for the sdk_ops empty-list-overwrite
// issue, defended at the cache layer.
func TestSetCachedVersionsEmptyDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	InitVersionCacheDir(dir)
	t.Cleanup(func() {
		resetGlobalCacheEntry(t, Dart)
		InitVersionCacheDir("")
	})
	resetGlobalCacheEntry(t, Dart)

	good := []VersionInfo{{Version: "3.5.0"}, {Version: "3.4.1"}}
	SetCachedVersions(Dart, good)

	// Both nil and empty-slice forms must be refused.
	SetCachedVersions(Dart, nil)
	SetCachedVersions(Dart, []VersionInfo{})

	got, ok := GetCachedVersions(Dart)
	if !ok {
		t.Fatal("expected cache entry to still exist after empty SetCachedVersions")
	}
	if len(got) != 2 || got[0].Version != "3.5.0" {
		t.Errorf("cache was clobbered: got %+v", got)
	}

	// The on-disk copy must still hold the good list (restart-recovery path).
	data, err := os.ReadFile(filepath.Join(dir, "versions_"+string(Dart)+".json"))
	if err != nil {
		t.Fatalf("on-disk cache missing: %v", err)
	}
	if !strings.Contains(string(data), "3.5.0") || !strings.Contains(string(data), "3.4.1") {
		t.Errorf("on-disk cache lost the good list: %s", data)
	}
}

// TestVersionCacheDiskRoundTrip verifies a non-empty SetCachedVersions writes
// to disk and that a fresh memory state re-loads from that file.
func TestVersionCacheDiskRoundTrip(t *testing.T) {
	dir := t.TempDir()
	InitVersionCacheDir(dir)
	t.Cleanup(func() {
		resetGlobalCacheEntry(t, Dart)
		InitVersionCacheDir("")
	})
	resetGlobalCacheEntry(t, Dart)

	SetCachedVersions(Dart, []VersionInfo{{Version: "3.5.0", DownloadURL: "https://example.invalid/x.zip"}})

	// Simulate an app restart: drop the in-memory entry, keep the disk file.
	globalVersionCache.mu.Lock()
	delete(globalVersionCache.entries, Dart)
	globalVersionCache.mu.Unlock()

	got, ok := GetCachedVersions(Dart)
	if !ok || len(got) != 1 || got[0].Version != "3.5.0" {
		t.Fatalf("expected disk-backed cache to reload, got ok=%v list=%+v", ok, got)
	}
	if got[0].DownloadURL != "https://example.invalid/x.zip" {
		t.Errorf("DownloadURL not preserved through disk round-trip: %+v", got[0])
	}
}

// TestCacheFileNoDir exercises cacheFile before any cache dir is configured:
// it must return "" (memory-only mode) instead of joining against an empty
// base dir.
func TestCacheFileNoDir(t *testing.T) {
	c := &versionCache{entries: make(map[SdkType]versionCacheEntry)}
	if got := c.cacheFile(Dart); got != "" {
		t.Errorf("cacheFile with no dir configured = %q; want empty", got)
	}
	c.mu.Lock()
	c.dir = t.TempDir()
	c.mu.Unlock()
	got := c.cacheFile(Dart)
	if got == "" || filepath.Base(got) != "versions_dart.json" {
		t.Errorf("cacheFile after dir set = %q; want .../versions_dart.json", got)
	}
}
