package pathmgr

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"sdk_version_control/internal/config"
)

const (
	systemEnvKey = `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
	userEnvKey   = `Environment`
)

// WindowsPathManager Windows 平台的 PATH 管理（通过用户级注册表 HKCU）
type WindowsPathManager struct {
	cfg *config.Config
}

// NewPathManager 创建 Windows 平台的 PathManager
func NewPathManager(cfg *config.Config) PathManager {
	return &WindowsPathManager{cfg: cfg}
}

func (m *WindowsPathManager) ConfigureSdk(sdkType string, versionDir string, binDir string, extraEnvVars map[string]string) error {
	binPath := versionDir
	if binDir != "" {
		binPath = filepath.Join(versionDir, binDir)
	}

	if err := m.addToUserPath(binPath, sdkType); err != nil {
		return fmt.Errorf("添加到用户PATH失败: %w", err)
	}

	for key, relPath := range extraEnvVars {
		value := versionDir
		if relPath != "" {
			value = filepath.Join(versionDir, relPath)
		}
		if err := m.setUserEnvVar(key, value); err != nil {
			return fmt.Errorf("设置用户%s失败: %w", key, err)
		}
	}

	broadcastEnvChange()
	return nil
}

func (m *WindowsPathManager) RemoveSdk(sdkType string, extraEnvVars map[string]string) error {
	if err := m.removeSvcPathsFromUserPath(sdkType); err != nil {
		return err
	}
	for key := range extraEnvVars {
		m.removeUserEnvVar(key)
	}
	broadcastEnvChange()
	return nil
}

func (m *WindowsPathManager) GetCurrentConfig() (map[string]string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.READ)
	if err != nil {
		return nil, err
	}
	defer k.Close()

	result := make(map[string]string)
	if path, _, err := k.GetStringValue("Path"); err == nil {
		for _, p := range strings.Split(path, ";") {
			if strings.Contains(p, ".svc") {
				result["PATH"] = p
			}
		}
	}
	return result, nil
}

func (m *WindowsPathManager) addToUserPath(binPath string, sdkType string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer k.Close()

	currentPath, _, err := k.GetStringValue("Path")
	if err != nil {
		currentPath = ""
	}

	sdkDir := m.cfg.SdkDir(sdkType)
	parts := strings.Split(currentPath, ";")
	var filtered []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, sdkDir) {
			continue
		}
		filtered = append(filtered, p)
	}

	filtered = append([]string{binPath}, filtered...)
	newPath := strings.Join(filtered, ";")

	return k.SetStringValue("Path", newPath)
}

func (m *WindowsPathManager) removeSvcPathsFromUserPath(sdkType string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer k.Close()

	currentPath, _, err := k.GetStringValue("Path")
	if err != nil {
		return nil
	}

	sdkDir := m.cfg.SdkDir(sdkType)
	parts := strings.Split(currentPath, ";")
	var filtered []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, sdkDir) {
			continue
		}
		filtered = append(filtered, p)
	}

	newPath := strings.Join(filtered, ";")
	return k.SetStringValue("Path", newPath)
}

func (m *WindowsPathManager) setUserEnvVar(key, value string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(key, value)
}

func (m *WindowsPathManager) removeUserEnvVar(key string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.ALL_ACCESS)
	if err != nil {
		return
	}
	defer k.Close()
	k.DeleteValue(key)
}

// CleanExternalPaths 清理非 SVC 管理的、匹配同 SDK 类型和版本的外部 PATH 条目
func (m *WindowsPathManager) CleanExternalPaths(sdkType string, version string, sourcePath string) error {
	if err := m.cleanExternalFromKey(sdkType, version, sourcePath); err == nil {
		broadcastEnvChange()
	}
	return nil
}

func (m *WindowsPathManager) cleanExternalFromKey(sdkType string, version string, sourcePath string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer k.Close()

	currentPath, _, err := k.GetStringValue("Path")
	if err != nil {
		return nil
	}

	svcDir := m.cfg.SvcDir()
	sourcePathClean := filepath.Clean(sourcePath)
	parts := strings.Split(currentPath, ";")
	var filtered []string
	removed := false
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(p, ".svc") {
			filtered = append(filtered, p)
			continue
		}
		if strings.HasPrefix(p, svcDir) {
			filtered = append(filtered, p)
			continue
		}
		if sourcePathClean != "" && strings.EqualFold(filepath.Clean(p), sourcePathClean) {
			removed = true
			continue
		}
		if sourcePathClean != "" {
			sourceRoot := DetectSdkRoot(sourcePathClean, sdkType)
			pRoot := DetectSdkRoot(p, sdkType)
			if sourceRoot != "" && pRoot != "" && strings.EqualFold(filepath.Clean(pRoot), filepath.Clean(sourceRoot)) {
				removed = true
				continue
			}
		}
		detected := detectSdkTypeByBin(p)
		if detected == "" {
			detected = detectSdkTypeFromPath(p)
		}
		if detected == sdkType && version != "" && strings.Contains(p, version) {
			removed = true
			continue
		}
		filtered = append(filtered, p)
	}

	if !removed {
		return fmt.Errorf("no external paths removed")
	}

	newPath := strings.Join(filtered, ";")
	return k.SetStringValue("Path", newPath)
}

func (m *WindowsPathManager) GetAllPathEntries() ([]PathEntry, error) {
	var entries []PathEntry
	seen := make(map[string]bool)

	// 先读用户级（主要配置位置）
	userEntries := m.readPathFromKey(registry.CURRENT_USER, userEnvKey)
	for _, e := range userEntries {
		if !seen[e.Path] {
			entries = append(entries, e)
			seen[e.Path] = true
		}
	}

	// 也读系统级（用于展示，标记为非管理）
	systemEntries := m.readPathFromKey(registry.LOCAL_MACHINE, systemEnvKey)
	for _, e := range systemEntries {
		if !seen[e.Path] {
			entries = append(entries, e)
			seen[e.Path] = true
		}
	}

	return DeduplicateEntries(entries), nil
}

func (m *WindowsPathManager) readPathFromKey(root registry.Key, keyPath string) []PathEntry {
	k, err := registry.OpenKey(root, keyPath, registry.READ)
	if err != nil {
		return nil
	}
	defer k.Close()

	pathVal, _, err := k.GetStringValue("Path")
	if err != nil {
		return nil
	}

	var entries []PathEntry
	for _, p := range strings.Split(pathVal, ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		isManaged := strings.Contains(p, ".svc")
		sdkType := ""
		if isManaged {
			sdkType = detectSdkTypeFromPath(p)
		} else {
			sdkType = detectSdkTypeByBin(p)
		}
		entries = append(entries, PathEntry{
			Path:      p,
			IsManaged: isManaged,
			SdkType:   sdkType,
		})
	}
	return entries
}

// DetectSystemConflicts 检测 HKLM 系统级是否有匹配该 SDK 的环境变量配置
func (m *WindowsPathManager) DetectSystemConflicts(sdkType string, extraEnvVarKeys []string) []string {
	var conflicts []string

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, systemEnvKey, registry.READ)
	if err == nil {
		defer k.Close()

		if pathVal, _, err := k.GetStringValue("Path"); err == nil {
			sdkDir := m.cfg.SdkDir(sdkType)
			for _, p := range strings.Split(pathVal, ";") {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				if strings.HasPrefix(p, sdkDir) {
					conflicts = append(conflicts, fmt.Sprintf("PATH: %s", p))
					continue
				}
				detected := detectSdkTypeByBin(p)
				if detected == "" {
					detected = detectSdkTypeFromPath(p)
				}
				if detected == sdkType {
					conflicts = append(conflicts, fmt.Sprintf("PATH: %s", p))
				}
			}
		}

		for _, key := range extraEnvVarKeys {
			if val, _, err := k.GetStringValue(key); err == nil && val != "" {
				conflicts = append(conflicts, fmt.Sprintf("%s=%s", key, val))
			}
		}
	}

	return conflicts
}

// broadcastEnvChange 广播环境变量变更消息
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
