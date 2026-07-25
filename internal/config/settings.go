package config

import (
	"encoding/json"
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
	Theme           string            `json:"theme"`           // "system", "dark", "light"
	Language        string            `json:"language"`        // "zh", "en"
	Proxy           ProxySettings     `json:"proxy"`
	Endpoints       map[string]string `json:"endpoints"`       // sdkType -> custom endpoint URL
	InstallPath     string            `json:"installPath"`     // custom install directory, empty = default ~/.svc
	GithubMirror    string            `json:"githubMirror"`    // GitHub mirror URL, empty = no replacement
	DownloadThreads int               `json:"downloadThreads"` // download thread count, 0 = default 4
}

// SettingsManager manages application settings
type SettingsManager struct {
	mu       sync.RWMutex
	svcDir   string
	settings AppSettings
}

// NewSettingsManager creates a settings manager
func NewSettingsManager(svcDir string) *SettingsManager {
	sm := &SettingsManager{
		svcDir: svcDir,
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

func (s *SettingsManager) load() {
	path := filepath.Join(s.svcDir, settingsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &s.settings); err != nil {
		logger.Warn("Failed to parse settings file (%s): %v, using default settings", path, err)
	}
}

func (s *SettingsManager) save() error {
	data, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.svcDir, settingsFile), data, 0644)
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
