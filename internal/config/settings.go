package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"sdk_version_control/internal/logger"
)

const settingsFile = "settings.json"

// ProxySettings proxy configuration
type ProxySettings struct {
	Enabled  bool   `json:"enabled"`  // whether proxy is enabled
	Mode     string `json:"mode"`     // "system" | "custom"
	URL      string `json:"url"`      // custom proxy URL
	Protocol string `json:"protocol"` // "http" | "socks5" (used when custom proxy has no scheme)
}

// AppSettings application settings
type AppSettings struct {
	Theme           string            `json:"theme"`    // "system", "dark", "light"
	Language        string            `json:"language"` // "zh", "en"
	Proxy           ProxySettings     `json:"proxy"`
	Endpoints       map[string]string `json:"endpoints"`       // sdkType -> custom endpoint URL
	InstallPath     string            `json:"installPath"`     // custom install directory, empty = default ~/.svc
	GithubMirror    string            `json:"githubMirror"`    // GitHub mirror URL, empty = no replacement
	GitHubToken     string            `json:"githubToken"`     // base64-encoded GitHub PAT, raises API rate limit 60->5000/h
	DownloadThreads int               `json:"downloadThreads"` // download thread count, 0 = default 4
}

// SettingsManager manages application settings.
//
// settings.json is ALWAYS stored at ~/.svc/settings.json regardless of the
// custom install path, because the shim runtime (resolveSvcHome in
// internal/shim/shim.go) reads it from that fixed location to discover the
// install path. If settings.json moved with the install dir, the shim would
// not be able to find it after a migration from the default ~/.svc to a
// custom path.
type SettingsManager struct {
	mu       sync.RWMutex
	homeDir  string // user home dir; settings.json lives at <homeDir>/.svc/settings.json
	settings AppSettings
}

// NewSettingsManager creates a settings manager. homeDir is the user's home
// directory (not the svcDir, which may change at runtime via SetSvcDir).
func NewSettingsManager(homeDir string) *SettingsManager {
	sm := &SettingsManager{
		homeDir: homeDir,
		settings: AppSettings{
			Theme:           "system",
			Language:        "zh",
			Proxy:           ProxySettings{Enabled: false, Mode: "system"},
			DownloadThreads: 4,
		},
	}
	sm.load()
	return sm
}

// settingsPath returns the fixed path to settings.json (~/.svc/settings.json).
func (s *SettingsManager) settingsPath() string {
	return filepath.Join(s.homeDir, ".svc", settingsFile)
}

func (s *SettingsManager) load() {
	path := s.settingsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	// M9: Unmarshal into a temp struct and only copy on success. Unmarshaling
	// directly into s.settings can leave it partially-populated when JSON
	// parsing fails midway (some fields applied from the file, others left at
	// their zero value), corrupting the in-memory defaults. On error, keep the
	// defaults that NewSettingsManager already set.
	var tmp AppSettings
	if err := json.Unmarshal(data, &tmp); err != nil {
		logger.Warn("Failed to parse settings file (%s): %v, using default settings", path, err)
		return
	}
	s.settings = tmp
}

func (s *SettingsManager) save() error {
	// Ensure ~/.svc exists (it may have been removed during migration from
	// the default path to a custom path). settings.json must always live here
	// so the shim runtime can discover the install path.
	dir := filepath.Join(s.homeDir, ".svc")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		return err
	}
	// H5: atomic write via temp file + os.Rename (matching config.go). A
	// partial write to settings.json (process killed mid-write) would
	// corrupt the file and break the shim runtime's install-path discovery.
	path := s.settingsPath()
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// Get returns current settings
func (s *SettingsManager) Get() AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// Update updates settings
func (s *SettingsManager) Update(settings AppSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = settings
	return s.save()
}
