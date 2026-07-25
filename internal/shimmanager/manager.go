package shimmanager

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"sdk_version_control/internal/config"
	"sdk_version_control/internal/logger"
	"sdk_version_control/internal/shim"
)

// Manager handles shim creation/removal and .svc.rc file management.
type Manager struct {
	cfg *config.Config
}

// New creates a shim Manager.
func New(cfg *config.Config) *Manager {
	return &Manager{cfg: cfg}
}

// EnsureSetup performs one-time initialization:
// 1. Creates the shims directory
// 2. Copies the app binary as the base shim (svc-shim)
// 3. On Unix: adds source line to shell rc (only once)
// 4. On Windows: adds shims dir to registry PATH (only once)
func (m *Manager) EnsureSetup() error {
	shimsDir := m.cfg.ShimsDir()
	if err := os.MkdirAll(shimsDir, 0755); err != nil {
		return fmt.Errorf("failed to create shims directory: %w", err)
	}

	if err := m.ensureShimBinary(); err != nil {
		return fmt.Errorf("failed to install shim binary: %w", err)
	}

	return m.ensurePathEntry()
}

// ensureShimBinary copies the current app binary to ~/.svc/shims/svc-shim
// if it doesn't exist or is outdated (compared by file size).
func (m *Manager) ensureShimBinary() error {
	appPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine app path: %w", err)
	}

	shimName := "svc-shim"
	if runtime.GOOS == "windows" {
		shimName = "svc-shim.exe"
	}
	shimPath := filepath.Join(m.cfg.ShimsDir(), shimName)

	// Check if shim binary exists and matches app binary size
	appInfo, err := os.Stat(appPath)
	if err != nil {
		return fmt.Errorf("cannot stat app binary: %w", err)
	}

	if shimInfo, err := os.Stat(shimPath); err == nil {
		if shimInfo.Size() == appInfo.Size() {
			// Already up-to-date
			return nil
		}
		logger.Info("Shim binary is outdated, updating...")
		os.Remove(shimPath)
	}

	// Copy the app binary to the shim path
	if err := copyFile(appPath, shimPath, 0755); err != nil {
		return fmt.Errorf("failed to copy shim binary: %w", err)
	}

	logger.Info("Shim binary installed at: %s", shimPath)
	return nil
}

// ConfigureSdk creates shims for all executables in the SDK's bin directory,
// updates shims.json with the command→sdkType mapping, and updates .svc.rc.
func (m *Manager) ConfigureSdk(sdkType string, versionDir string, binDir string, extraEnvVars map[string]string) error {
	// Compute the bin directory path
	binPath := versionDir
	if binDir != "" {
		binPath = filepath.Join(versionDir, binDir)
	}

	// Scan the bin directory for executables and create shims
	createdShims, err := m.createShimsForDir(binPath, sdkType)
	if err != nil {
		logger.Warn("Failed to create some shims for %s: %v", sdkType, err)
	}

	// Update shims.json with the SDK config
	if err := m.updateShimConfig(sdkType, binDir, extraEnvVars, createdShims); err != nil {
		return fmt.Errorf("failed to update shim config: %w", err)
	}

	// Update .svc.rc with env vars
	if err := m.updateRcFile(); err != nil {
		logger.Warn("Failed to update .svc.rc: %v", err)
	}

	logger.Info("Configured shims for %s: %d commands", sdkType, len(createdShims))
	return nil
}

// RemoveSdk removes all shims for the given SDK type and updates config files.
func (m *Manager) RemoveSdk(sdkType string, extraEnvVars map[string]string) error {
	// Find and remove all shims belonging to this SDK type
	commands, err := m.getCommandsForSdkType(sdkType)
	if err != nil {
		return err
	}

	for _, cmd := range commands {
		m.removeShim(cmd)
	}

	// Remove SDK type from shims.json
	if err := m.removeSdkFromConfig(sdkType); err != nil {
		return fmt.Errorf("failed to remove SDK from shim config: %w", err)
	}

	// Update .svc.rc (remove env vars for this SDK)
	if err := m.updateRcFile(); err != nil {
		logger.Warn("Failed to update .svc.rc: %v", err)
	}

	logger.Info("Removed shims for %s: %d commands", sdkType, len(commands))
	return nil
}

// createShimsForDir scans a directory and creates a shim (hardlink) for each executable.
func (m *Manager) createShimsForDir(dir string, sdkType string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot read bin directory %s: %w", dir, err)
	}

	var created []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		// On Windows, only shim .exe files; skip .cmd/.bat (they'd need wrappers)
		if runtime.GOOS == "windows" {
			if !strings.HasSuffix(strings.ToLower(name), ".exe") {
				continue
			}
		} else {
			// On Unix, check if the file is executable
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if info.Mode().Perm()&0111 == 0 {
				continue
			}
		}

		if err := m.createShim(name); err != nil {
			logger.Warn("Failed to create shim for %s: %v", name, err)
			continue
		}
		created = append(created, name)
	}

	return created, nil
}

// createShim creates a hardlink from ~/.svc/shims/{name} → ~/.svc/shims/svc-shim
func (m *Manager) createShim(name string) error {
	shimPath := m.getShimBinaryPath()
	linkPath := filepath.Join(m.cfg.ShimsDir(), name)

	// Remove existing shim if present
	os.Remove(linkPath)

	// Try hardlink first (same directory, should always work)
	if err := os.Link(shimPath, linkPath); err == nil {
		return nil
	}

	// Fallback: copy the shim binary
	return copyFile(shimPath, linkPath, 0755)
}

// removeShim removes a single shim file.
func (m *Manager) removeShim(name string) {
	linkPath := filepath.Join(m.cfg.ShimsDir(), name)
	os.Remove(linkPath)
}

// getShimBinaryPath returns the path to the base shim binary.
func (m *Manager) getShimBinaryPath() string {
	name := "svc-shim"
	if runtime.GOOS == "windows" {
		name = "svc-shim.exe"
	}
	return filepath.Join(m.cfg.ShimsDir(), name)
}

// updateShimConfig updates shims.json with the SDK type config and its commands.
func (m *Manager) updateShimConfig(sdkType string, binDir string, envVars map[string]string, commands []string) error {
	cfgPath := m.cfg.ShimsConfigPath()
	cfg := m.loadShimConfig()

	if cfg.Commands == nil {
		cfg.Commands = make(map[string]string)
	}
	if cfg.SdkTypes == nil {
		cfg.SdkTypes = make(map[string]shim.SdkShimEntry)
	}

	// Remove old commands for this SDK type (in case executables changed)
	for cmd, st := range cfg.Commands {
		if st == sdkType {
			delete(cfg.Commands, cmd)
		}
	}

	// Add new commands
	for _, cmd := range commands {
		// Strip .exe extension for the command name (the shim handles this)
		cmdName := cmd
		if runtime.GOOS == "windows" {
			cmdName = strings.TrimSuffix(cmdName, ".exe")
		}
		cfg.Commands[cmdName] = sdkType
	}

	// Update SDK type config
	cfg.SdkTypes[sdkType] = shim.SdkShimEntry{
		BinDir:  binDir,
		EnvVars: envVars,
	}

	return m.saveShimConfig(cfgPath, cfg)
}

// removeSdkFromConfig removes an SDK type and its commands from shims.json.
func (m *Manager) removeSdkFromConfig(sdkType string) error {
	cfgPath := m.cfg.ShimsConfigPath()
	cfg := m.loadShimConfig()

	for cmd, st := range cfg.Commands {
		if st == sdkType {
			delete(cfg.Commands, cmd)
		}
	}
	delete(cfg.SdkTypes, sdkType)

	return m.saveShimConfig(cfgPath, cfg)
}

// getCommandsForSdkType returns all command names registered for a given SDK type.
func (m *Manager) getCommandsForSdkType(sdkType string) ([]string, error) {
	cfg := m.loadShimConfig()
	var commands []string
	for cmd, st := range cfg.Commands {
		if st == sdkType {
			commands = append(commands, cmd)
		}
	}
	return commands, nil
}

// loadShimConfig reads shims.json, returning an empty config if not found.
func (m *Manager) loadShimConfig() shim.ShimConfig {
	cfgPath := m.cfg.ShimsConfigPath()
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return shim.ShimConfig{}
	}
	var cfg shim.ShimConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return shim.ShimConfig{}
	}
	return cfg
}

// saveShimConfig writes shims.json.
func (m *Manager) saveShimConfig(path string, cfg shim.ShimConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// copyFile copies a file from src to dst with the given permissions.
func copyFile(src, dst string, perm os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, perm)
}
