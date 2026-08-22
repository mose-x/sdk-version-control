//go:build windows

package shimmanager

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"svc/internal/logger"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	userEnvKey = `Environment`
)

// generateRcContent generates the .svc.rc content (Windows).
// On Windows this is a reference config file; the actual PATH and env vars
// are managed via the registry (PATH has only one entry: the shims dir).
func (m *Manager) generateRcContent(envVars []envVarEntry) string {
	svcHome := m.cfg.SvcDir()
	home := m.cfg.HomeDir()

	var lines []string
	lines = append(lines, "# ~/.svc.rc - Managed by SVC. You may edit manually.")
	lines = append(lines, "# On Windows, PATH and env vars are also set in the registry.")
	lines = append(lines, "# This file serves as a reference and backup of your SVC configuration.")
	lines = append(lines, "")

	if svcHome == filepath.Join(home, ".svc") {
		lines = append(lines, "SVC_HOME=%USERPROFILE%\\.svc")
	} else {
		lines = append(lines, fmt.Sprintf("SVC_HOME=%s", svcHome))
	}
	lines = append(lines, fmt.Sprintf("SHIMS_DIR=%s", filepath.Join(svcHome, "shims")))

	if len(envVars) > 0 {
		lines = append(lines, "")
		lines = append(lines, "# SDK environment variables (updated on version switch)")
		for _, e := range envVars {
			lines = append(lines, fmt.Sprintf("%s=%s", e.Key, e.Value))
		}
	}

	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

// ensurePathEntry adds the shims directory to the user PATH in the registry (one-time).
// Only one PATH entry is ever added; it is never removed or duplicated.
func (m *Manager) ensurePathEntry() error {
	shimsDir := m.cfg.ShimsDir()

	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer k.Close()

	// Item 2: probe the value type first and NEVER overwrite a PATH that is
	// not REG_SZ/REG_EXPAND_SZ. The old code treated every GetStringValue
	// error as "empty PATH" and then wrote REG_SZ, which silently replaced /
	// downgraded the user's real PATH (the default user PATH is
	// REG_EXPAND_SZ; downgrading it to REG_SZ stops %VAR% expansion).
	currentPath, valType, existed, err := readUserPathValue(k)
	if err != nil {
		return err
	}

	newPath := mergePathEntry(currentPath, shimsDir)
	if newPath == currentPath {
		return nil // Already in PATH
	}

	if err := writeUserPathValue(k, newPath, valType, existed); err != nil {
		return fmt.Errorf("failed to set PATH: %w", err)
	}

	broadcastEnvChange()
	logger.Info("Added shims directory to user PATH: %s", shimsDir)
	return nil
}

// readUserPathValue reads the user PATH value together with its registry
// type. Returns existed=false (empty PATH, no error) when the value does
// not exist — a legitimate state on fresh profiles. Any other read problem
// (access denied, unsupported type such as REG_MULTI_SZ/REG_DWORD) is an
// error: callers must abort instead of overwriting a value they could not
// understand.
func readUserPathValue(k registry.Key) (val string, valType uint32, existed bool, err error) {
	// GetValue with a nil buffer only queries size + type.
	_, valType, err = k.GetValue("Path", nil)
	if errors.Is(err, registry.ErrNotExist) {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, fmt.Errorf("failed to probe user PATH type: %w", err)
	}
	if valType != registry.SZ && valType != registry.EXPAND_SZ {
		return "", 0, false, fmt.Errorf("user PATH has unsupported registry value type %d; refusing to modify it", valType)
	}
	val, _, err = k.GetStringValue("Path")
	if err != nil {
		return "", 0, false, fmt.Errorf("failed to read user PATH: %w", err)
	}
	return val, valType, true, nil
}

// writeUserPathValue writes the user PATH preserving the value type: keep
// REG_EXPAND_SZ when the original value was REG_EXPAND_SZ or when the new
// value contains %VAR% references, otherwise write REG_SZ. Writing REG_SZ
// over an ExpandString silently breaks %VAR% expansion in PATH entries.
func writeUserPathValue(k registry.Key, val string, origType uint32, existed bool) error {
	if (existed && origType == registry.EXPAND_SZ) || strings.Contains(val, "%") {
		return k.SetExpandStringValue("Path", val)
	}
	return k.SetStringValue("Path", val)
}

// SetEnvVars writes environment variables (JAVA_HOME, GOROOT, etc.) to the registry.
// This is called on version switch to keep IDE-compatible env vars up to date.
func (m *Manager) SetEnvVars(envVars []envVarEntry) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer k.Close()

	for _, e := range envVars {
		if err := k.SetExpandStringValue(e.Key, e.Value); err != nil {
			logger.Warn("Failed to set %s: %v", e.Key, err)
		}
	}

	broadcastEnvChange()
	return nil
}

// RemoveEnvVars removes environment variables from the registry.
func (m *Manager) RemoveEnvVars(keys []string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.ALL_ACCESS)
	if err != nil {
		return
	}
	defer k.Close()

	for _, key := range keys {
		k.DeleteValue(key)
	}

	broadcastEnvChange()
}

// applyEnvVarsToSystem writes env vars (JAVA_HOME, GOROOT, ...) to the registry
// so IDEs that read the registry see them. The shim runtime sets the same vars
// via os.Setenv before exec, so CLI-via-shim already works without this; this
// is purely for tooling that reads the registry instead of inheriting the
// shim's environment.
func (m *Manager) applyEnvVarsToSystem(envVars []envVarEntry) {
	if len(envVars) == 0 {
		return
	}
	if err := m.SetEnvVars(envVars); err != nil {
		logger.Warn("Failed to write env vars to registry: %v", err)
	}
}

// writeFishEnvFile is a no-op on Windows: env.sh.fish is a Unix-shell
// concept, and on Windows PATH/env vars are managed via the registry.
// Exists only to mirror the Unix contract for updateRcFile. Item 6.
func (m *Manager) writeFishEnvFile(envVars []envVarEntry) {}

// removeEnvVarsFromSystem deletes env vars from the registry when an SDK is
// removed, so a leftover JAVA_HOME doesn't point at a now-uninstalled JDK.
func (m *Manager) removeEnvVarsFromSystem(keys []string) {
	if len(keys) == 0 {
		return
	}
	m.RemoveEnvVars(keys)
}

// detectConfiguredShells returns the list of configured shells (Windows).
// On Windows, "configured" means the shims dir is in the registry PATH.
func (m *Manager) detectConfiguredShells() []string {
	shimsDir := m.cfg.ShimsDir()

	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.READ)
	if err != nil {
		return nil
	}
	defer k.Close()

	path, _, err := k.GetStringValue("Path")
	if err != nil {
		return nil
	}

	for _, p := range strings.Split(path, ";") {
		if strings.EqualFold(filepath.Clean(strings.TrimSpace(p)), filepath.Clean(shimsDir)) {
			return []string{"registry"}
		}
	}
	return nil
}

// removeAllSourceLines removes the shims dir from the registry PATH (Windows).
func (m *Manager) removeAllSourceLines() error {
	shimsDir := m.cfg.ShimsDir()

	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer k.Close()

	// Item 2: same type-safety rules as ensurePathEntry. A missing value is a
	// clean no-op; an unsupported type or read error must abort rather than
	// be clobbered by a REG_SZ write.
	currentPath, valType, existed, err := readUserPathValue(k)
	if err != nil {
		return err
	}
	if !existed {
		return nil
	}

	newPath := filterPathEntry(currentPath, shimsDir)
	if newPath == currentPath {
		return nil // shims dir not present; nothing to change
	}

	if err := writeUserPathValue(k, newPath, valType, existed); err != nil {
		return err
	}

	broadcastEnvChange()
	return nil
}

// addSourceLine adds the shims dir to PATH (Windows equivalent of addSourceLine).
func (m *Manager) addSourceLine(shellName string) error {
	if shellName == "powershell" || shellName == "registry" {
		return m.ensurePathEntry()
	}
	return nil
}

// broadcastEnvChange broadcasts the environment variable change notification
func broadcastEnvChange() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	sendMessageTimeout := user32.NewProc("SendMessageTimeoutW")
	HWND_BROADCAST := uintptr(0xFFFF)
	WM_SETTINGCHANGE := uintptr(0x001A)
	SMTO_ABORTIFHUNG := uintptr(0x0002)

	envStr, _ := syscall.UTF16PtrFromString("Environment")
	sendMessageTimeout.Call(
		HWND_BROADCAST,
		WM_SETTINGCHANGE,
		0,
		uintptr(unsafe.Pointer(envStr)),
		SMTO_ABORTIFHUNG,
		5000,
		0,
	)
}
