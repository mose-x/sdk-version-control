package shimmanager

import (
	"bytes"
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

	// Ensure the python3 -> python alias is registered + shim created on every
	// startup, not only when the shim binary is updated. Without this, a stale
	// shims.json (missing python3) with a current shim binary would leave
	// python3 unrouteable forever (ensurePython3Alias previously only ran
	// inside rebuildCommandShims, which is skipped when needUpdate=false).
	m.ensurePython3Shim()

	return m.ensurePathEntry()
}

// ensurePython3Shim registers the python3 -> python command alias in
// shims.json (if Python is configured but python3 is missing) and creates the
// python3 shim file. Runs on every startup (EnsureSetup), independent of
// whether the shim binary was updated, so a stale shims.json self-heals. If
// persisting the alias fails, the shim file is NOT created (avoids leaving an
// orphan hardlink that routes to "unknown command"). Windows CPython ships
// python.exe (no python3.exe); without this alias `python3` resolves to the
// Windows Store stub.
func (m *Manager) ensurePython3Shim() {
	cfg := m.loadShimConfig()
	if !m.ensurePython3Alias(&cfg) {
		return
	}
	// Persist BEFORE creating the shim file: if save fails, the on-disk
	// shims.json won't have python3, so a shim file would be an unrouteable
	// orphan (hardlink exists, but shim.Run can't resolve the command).
	if err := m.saveShimConfig(m.cfg.ShimsConfigPath(), cfg); err != nil {
		logger.Warn("Failed to persist python3 alias: %v", err)
		return
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	if err := m.createShimFor("python3", ext); err != nil {
		logger.Warn("Failed to create python3 shim: %v", err)
	}
}

// ensureShimBinary installs ~/.svc/shims/svc-shim[.exe] if it is missing or
// outdated (compared by file size + modtime). When the base shim is actually
// replaced, all existing command shims (node.exe, go.exe, ...) are rebuilt so
// they point at the new binary instead of lingering as hardlinks to the
// previous version's svc-shim. On Windows, os.Remove on svc-shim.exe leaves
// existing hardlinks (node.exe) pointing at the old inode, so the old binary
// keeps running until the hardlink is explicitly recreated.
//
// On Windows the shim is the embedded console-subsystem binary
// (embeddedShimBinary), NOT a copy of the app binary: the app binary is built
// with -H windowsgui (no console), so hardlinking node.exe to it makes
// `node -v` print nothing and leaves cmd.exe's prompt stuck. A console
// subsystem shim gets a real stdio handle from cmd.exe and the prompt redraws
// on exit, exactly like the real node.exe. If the embed is empty (dev build
// without the prebuild step), it falls back to copying the app binary; the
// AttachConsole fallback in the shim package then keeps output working, with
// the known prompt-hang trade-off.
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

	appInfo, err := os.Stat(appPath)
	if err != nil {
		return fmt.Errorf("cannot stat app binary: %w", err)
	}

	// On Windows, prefer the embedded console-subsystem shim binary over a
	// copy of the GUI-subsystem app binary. Falls back to the app binary when
	// the embed is empty (dev build without the prebuild step).
	useEmbedded := runtime.GOOS == "windows" && len(embeddedShimBinary) > 0

	expectedSize := appInfo.Size()
	if useEmbedded {
		expectedSize = int64(len(embeddedShimBinary))
	}

	needUpdate := true
	if shimInfo, err := os.Stat(shimPath); err == nil {
		if shimInfo.Size() == expectedSize && !shimInfo.ModTime().Before(appInfo.ModTime()) {
			// Size + modtime match, but that's not enough: two builds can
			// land at the same file size with different bytes (e.g. a
			// rebuild after a tiny code change). Without a content check,
			// needUpdate=false skips rebuildCommandShims, and on Windows the
			// existing command hardlinks (node.exe, ...) keep pointing at
			// the OLD svc-shim inode. Compare the on-disk shim bytes to the
			// expected bytes; only when they match can we skip the update.
			var expected []byte
			if useEmbedded {
				expected = embeddedShimBinary
			} else if b, err := os.ReadFile(appPath); err == nil {
				expected = b
			}
			if filesEqual(shimPath, expected) {
				needUpdate = false
			}
		}
	}

	if !needUpdate {
		return nil
	}

	logger.Info("Shim binary is outdated, updating...")
	os.Remove(shimPath)

	if useEmbedded {
		if err := os.WriteFile(shimPath, embeddedShimBinary, 0755); err != nil {
			return fmt.Errorf("failed to write embedded shim binary: %w", err)
		}
	} else {
		if err := copyFile(appPath, shimPath, 0755); err != nil {
			return fmt.Errorf("failed to copy shim binary: %w", err)
		}
	}

	logger.Info("Shim binary installed at: %s", shimPath)

	// Rebuild every existing command shim so its hardlink/copy targets the
	// freshly written svc-shim. Without this, command shims created by a
	// previous app version keep pointing at the old binary on Windows
	// (Remove+rewrite of svc-shim.exe leaves prior hardlinks intact on the
	// old inode), so users keep running the stale shim after an update.
	m.rebuildCommandShims()
	return nil
}

// rebuildCommandShims recreates the shim file for every command registered in
// shims.json. The command→sdkType mapping and binDirs are preserved; only the
// on-disk shim file (hardlink to svc-shim, or .cmd/.bat wrapper) is rebuilt.
func (m *Manager) rebuildCommandShims() {
	cfg := m.loadShimConfig()
	if len(cfg.Commands) == 0 {
		return
	}
	// NOTE: the python3 alias is ensured separately by ensurePython3Shim
	// (called from EnsureSetup on every startup, independent of whether the
	// shim binary was updated). This function only rebuilds the on-disk shim
	// files for commands already registered in shims.json.
	rebuilt := 0
	for cmd := range cfg.Commands {
		// classifyExecutable needs the on-disk filename; reconstruct the
		// extension the original shim used. .exe hardlink on Windows, no
		// extension on Unix. .cmd/.bat wrappers are Windows-only and are
		// recreated below by re-deriving the extension from any existing
		// variant.
		ext := ""
		if runtime.GOOS == "windows" {
			// Prefer .exe; fall back to .cmd/.bat if that's what existed.
			for _, candidate := range []string{".exe", ".cmd", ".bat"} {
				if _, err := os.Stat(filepath.Join(m.cfg.ShimsDir(), cmd+candidate)); err == nil {
					ext = candidate
					break
				}
			}
			if ext == "" {
				ext = ".exe"
			}
		}
		if err := m.createShimFor(cmd, ext); err != nil {
			logger.Warn("Failed to rebuild shim for %s: %v", cmd, err)
			continue
		}
		rebuilt++
	}
	if rebuilt > 0 {
		logger.Info("Rebuilt %d command shims to match updated svc-shim", rebuilt)
	}
}

// ensurePython3Alias registers the python3 -> python command alias in cfg if
// Python is configured but python3 isn't registered yet. Returns true if cfg
// was modified (caller persists it and the rebuild loop creates the shim file).
// Windows CPython ships python.exe (no python3.exe); without this alias
// `python3` resolves to the Windows Store stub. Called from
// rebuildCommandShims so a self-update + restart auto-adds python3 without
// reinstalling Python.
func (m *Manager) ensurePython3Alias(cfg *shim.ShimConfig) bool {
	if _, ok := cfg.SdkTypes["python"]; !ok {
		return false
	}
	if _, ok := cfg.Commands["python3"]; ok {
		return false
	}
	if cfg.Commands == nil {
		cfg.Commands = make(map[string]string)
	}
	cfg.Commands["python3"] = "python"
	return true
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

	// Drop the SDK's env vars from the OS-level store (Windows registry) so a
	// leftover JAVA_HOME doesn't point at a now-uninstalled JDK. No-op on
	// Unix where .svc.rc regeneration is the only path. extraEnvVars is the
	// map originally passed to ConfigureSdk; its keys are the env var names.
	keys := make([]string, 0, len(extraEnvVars))
	for k := range extraEnvVars {
		keys = append(keys, k)
	}
	m.removeEnvVarsFromSystem(keys)

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

	// Python on Windows ships python.exe but no python3.exe (python3 is a Unix
	// convention). Alias python3 -> python so `python3` works on Windows like on
	// Unix. On Unix the bin usually already has a python3 symlink (created
	// above), so the !created guard skips it.
	if sdkType == "python" && !created["python3"] {
		ext := ""
		if runtime.GOOS == "windows" {
			ext = ".exe"
		}
		if err := m.createShimFor("python3", ext); err != nil {
			logger.Warn("Failed to create python3 shim: %v", err)
		} else {
			created["python3"] = true
			result = append(result, "python3")
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
	// Purge ALL variants of cmdName first. The previous code only removed
	// the target variant (os.Remove(cmdName+ext)); switching extensions
	// (e.g. a .cmd wrapper existed and now we create .exe, or vice versa)
	// left the old variant on disk as a stale, conflicting shim. removeShim
	// already does multi-variant removal on Windows (bare+.exe+.cmd+.bat)
	// and bare-only on Unix, so reuse it.
	m.removeShim(cmdName)

	if ext == "" || ext == ".exe" {
		shimPath := m.getShimBinaryPath()
		linkPath := filepath.Join(m.cfg.ShimsDir(), cmdName+ext)
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

// filesEqual reports whether the bytes at path equal expected. Used by
// ensureShimBinary to catch same-size-different-content binaries that the
// size+modtime heuristic would miss (a rebuild with coincidentally identical
// file size but different bytes). A read error is treated as not-equal so the
// caller falls through to the safe "rebuild" path.
func filesEqual(path string, expected []byte) bool {
	got, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Equal(got, expected)
}
