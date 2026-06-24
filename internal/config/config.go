package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"sdk_version_control/internal/logger"
)

const (
	svcDirName   = ".svc"
	configFile   = "config.json"
	tmpDirName   = "tmp"
	envShFile    = "env.sh"
)

// Config manages the ~/.svc directory and application configuration
type Config struct {
	mu      sync.RWMutex
	homeDir string
	svcDir  string
	data    *ConfigData
}

// ConfigData holds data persisted to config.json
type ConfigData struct {
	ActiveVersions map[string]string `json:"activeVersions"` // sdkType -> version
}

// NewConfig creates a configuration manager
func NewConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	svcDir := filepath.Join(home, svcDirName)
	c := &Config{
		homeDir: home,
		svcDir:  svcDir,
		data: &ConfigData{
			ActiveVersions: make(map[string]string),
		},
	}
	if err := c.init(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) init() error {
	// Create the ~/.svc directory
	dirs := []string{
		c.svcDir,
		filepath.Join(c.svcDir, tmpDirName),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	// Create each SDK subdirectory
	for _, name := range []string{
		"nodejs", "jdk", "go", "python", "rust", "ruby", "dotnet", "php", "perl",
		"maven", "gradle",
		"flutter", "android", "dart",
	} {
		if err := os.MkdirAll(filepath.Join(c.svcDir, name), 0755); err != nil {
			return err
		}
	}
	// Load the config file
	return c.load()
}

func (c *Config) load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	path := filepath.Join(c.svcDir, configFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c.save()
		}
		return err
	}
	return json.Unmarshal(data, c.data)
}

func (c *Config) save() error {
	data, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.svcDir, configFile), data, 0644)
}

// HomeDir returns the user's home directory
func (c *Config) HomeDir() string {
	return c.homeDir
}

// SvcDir returns the ~/.svc path
func (c *Config) SvcDir() string {
	return c.svcDir
}

// SetSvcDir sets a custom install directory
func (c *Config) SetSvcDir(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.svcDir = dir
}

// DefaultSvcDir returns the default install directory
func DefaultSvcDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home, err = os.Getwd()
		if err != nil {
			return svcDirName
		}
	}
	return filepath.Join(home, svcDirName)
}

// TmpDir returns the temporary download directory
func (c *Config) TmpDir() string {
	return filepath.Join(c.svcDir, tmpDirName)
}

// SdkDir returns the storage directory of the specified SDK
func (c *Config) SdkDir(sdkType string) string {
	return filepath.Join(c.svcDir, sdkType)
}

// SdkVersionDir returns the install directory of the specified SDK version
func (c *Config) SdkVersionDir(sdkType string, version string) string {
	return filepath.Join(c.SdkDir(sdkType), version)
}

// GetActiveVersion returns the currently active version
func (c *Config) GetActiveVersion(sdkType string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data.ActiveVersions[sdkType]
}

// SetActiveVersion sets the currently active version
func (c *Config) SetActiveVersion(sdkType string, version string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data.ActiveVersions[sdkType] = version
	return c.save()
}

// GetInstalledVersions returns all locally installed versions
func (c *Config) GetInstalledVersions(sdkType string) []string {
	dir := c.SdkDir(sdkType)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Error("Failed to read SDK directory (%s): %v", dir, err)
		}
		return nil
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	return versions
}

// EnvShPath returns the env.sh file path (used on Linux/macOS)
func (c *Config) EnvShPath() string {
	return filepath.Join(c.svcDir, envShFile)
}

// IsWindows reports whether the current OS is Windows
func IsWindows() bool {
	return runtime.GOOS == "windows"
}

// IsDarwin reports whether the current OS is macOS
func IsDarwin() bool {
	return runtime.GOOS == "darwin"
}

// IsLinux reports whether the current OS is Linux
func IsLinux() bool {
	return runtime.GOOS == "linux"
}
