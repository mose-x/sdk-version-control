//go:build windows

package shimmanager

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"sdk_version_control/internal/logger"

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

	currentPath, _, err := k.GetStringValue("Path")
	if err != nil {
		currentPath = ""
	}

	// Check if shims dir is already in PATH
	for _, p := range strings.Split(currentPath, ";") {
		p = strings.TrimSpace(p)
		if strings.EqualFold(filepath.Clean(p), filepath.Clean(shimsDir)) {
			return nil // Already in PATH
		}
	}

	// Add shims dir to PATH (prepend)
	newPath := shimsDir + ";" + currentPath
	if err := k.SetStringValue("Path", newPath); err != nil {
		return fmt.Errorf("failed to set PATH: %w", err)
	}

	broadcastEnvChange()
	logger.Info("Added shims directory to user PATH: %s", shimsDir)
	return nil
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

	currentPath, _, err := k.GetStringValue("Path")
	if err != nil {
		return nil
	}

	var filtered []string
	for _, p := range strings.Split(currentPath, ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.EqualFold(filepath.Clean(p), filepath.Clean(shimsDir)) {
			continue
		}
		filtered = append(filtered, p)
	}

	newPath := strings.Join(filtered, ";")
	if err := k.SetStringValue("Path", newPath); err != nil {
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
