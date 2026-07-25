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

// ConfigureSdk creates shims for all executables across the SDK's bin
// directories, updates shims.json with the command→sdkType mapping, and
// updates .svc.rc. binDirs are relative to versionDir; "" means versionDir
// itself. Earlier binDirs win on command-name conflicts (first match wins).
func (m *Manager) ConfigureSdk(sdkType string, versionDir string, binDirs []string, extraEnvVars map[string]string) error {
	// Scan all bin directories for executables and create shims.
	// Commands are deduped by name across dirs (first dir wins).
	createdShims, err := m.createShimsForDirs(versionDir, binDirs, sdkType)
	if err != nil {
		logger.Warn("Failed to create some shims for %s: %v", sdkType, err)
	}

	// Update shims.json with the SDK config
	if err := m.updateShimConfig(sdkType, binDirs, extraEnvVars, createdShims); err != nil {
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

// createShimsForDirs scans all bin directories (relative to versionDir) and
// creates a shim for each executable. Commands are deduped by name across all
// dirs: the first dir (in binDirs order) that provides a command wins. This
// matters for SDKs like Rust where cargo/bin hardlinks to rustc/bin/rustc;
// we want cargo/bin's copy to be the one shimmed (it is the merged entry).
func (m *Manager) createShimsForDirs(versionDir string, binDirs []string, sdkType string) ([]string, error) {
	created := make(map[string]bool)
	var result []string
	for _, binDir := range binDirs {
		binPath := versionDir
		if binDir != "" {
			binPath = filepath.Join(versionDir, binDir)
		}
		entries, err := os.ReadDir(binPath)
		if err != nil {
			logger.Warn("Cannot read bin directory %s: %v", binPath, err)
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()

			cmdName, ext, ok := classifyExecutable(name)
			if !ok {
				continue
			}
			if runtime.GOOS != "windows" {
				info, err := entry.Info()
				if err != nil {
					continue
				}
				if info.Mode().Perm()&0111 == 0 {
					continue
				}
			}
			if created[cmdName] {
				continue
			}
			if err := m.createShimFor(cmdName, ext); err != nil {
				logger.Warn("Failed to create shim for %s: %v", name, err)
				continue
			}
			created[cmdName] = true
			result = append(result, cmdName)
		}
	}

	return result, nil
}

// classifyExecutable splits a filename into command name (without extension),
// extension, and whether it is shim-able on the current OS.
func classifyExecutable(name string) (cmdName, ext string, ok bool) {
	if runtime.GOOS == "windows" {
		lower := strings.ToLower(name)
		switch {
		case strings.HasSuffix(lower, ".exe"):
			return name[:len(name)-len(".exe")], ".exe", true
		case strings.HasSuffix(lower, ".cmd"):
			return name[:len(name)-len(".cmd")], ".cmd", true
		case strings.HasSuffix(lower, ".bat"):
			return name[:len(name)-len(".bat")], ".bat", true
		default:
			return "", "", false
		}
	}
	return name, "", true
}

// createShimFor creates a shim for a command.
//   - "" or ".exe" (Unix, or Windows .exe): hardlink to the base shim binary.
//     On Unix, if the base shim is itself a symlink (rare), resolve it first
//     so the hardlink points at the real file, not a possibly-stale symlink
//     node. This matters when SDKs ship commands as symlinks (pip -> pip3);
//     a hardlink to a symlink survives the link being retargeted only if the
//     hardlink is to the symlink itself, but resolving avoids edge cases.
//   - ".cmd"/".bat" (Windows): write a wrapper batch script that delegates to
//     svc-shim.exe with the command name as argv[1], so the shim runtime can
//     route it to the active SDK version. A hardlink cannot be used here
//     because cmd.exe would try to interpret the PE binary as a batch script.
func (m *Manager) createShimFor(cmdName, ext string) error {
	if ext == "" || ext == ".exe" {
		shimPath := m.getShimBinaryPath()
		linkPath := filepath.Join(m.cfg.ShimsDir(), cmdName+ext)
		os.Remove(linkPath)
		// Resolve symlinks on Unix so the hardlink targets the real file.
		// On Windows os.Stat already follows; SymlinkTarget is not needed.
		target := shimPath
		if runtime.GOOS != "windows" {
			if resolved, err := filepath.EvalSymlinks(shimPath); err == nil {
				target = resolved
			}
		}
		if err := os.Link(target, linkPath); err == nil {
			return nil
		}
		return copyFile(target, linkPath, 0755)
	}

	// Windows .cmd / .bat wrapper. %~dp0 resolves to the shims directory.
	wrapperPath := filepath.Join(m.cfg.ShimsDir(), cmdName+ext)
	content := fmt.Sprintf("@echo off\r\n\"%%~dp0svc-shim.exe\" %s %%*\r\n", cmdName)
	return os.WriteFile(wrapperPath, []byte(content), 0644)
}

// removeShim removes a shim and any platform-specific variants for the command.
func (m *Manager) removeShim(name string) {
	dir := m.cfg.ShimsDir()
	os.Remove(filepath.Join(dir, name))
	if runtime.GOOS == "windows" {
		os.Remove(filepath.Join(dir, name+".exe"))
		os.Remove(filepath.Join(dir, name+".cmd"))
		os.Remove(filepath.Join(dir, name+".bat"))
	}
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
func (m *Manager) updateShimConfig(sdkType string, binDirs []string, envVars map[string]string, commands []string) error {
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
		BinDirs: binDirs,
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
