//go:build !windows

package pathmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"sdk_version_control/internal/config"
	"sdk_version_control/internal/logger"
)

// UnixPathManager handles PATH management on Linux/macOS (via user-level shell config files)
type UnixPathManager struct {
	cfg *config.Config
}

// NewPathManager creates a PathManager for Linux/macOS
func NewPathManager(cfg *config.Config) PathManager {
	return &UnixPathManager{cfg: cfg}
}

func (m *UnixPathManager) ConfigureSdk(sdkType string, versionDir string, binDir string, extraEnvVars map[string]string) error {
	binPath := versionDir
	if binDir != "" {
		binPath = filepath.Join(versionDir, binDir)
	}

	if err := m.writeEnvSh(sdkType, binPath, extraEnvVars, versionDir); err != nil {
		return fmt.Errorf("failed to write env.sh: %w", err)
	}

	if err := m.writeFishEnvSh(sdkType, binPath, extraEnvVars, versionDir); err != nil {
		logger.Warn("Failed to write fish config: %v", err)
	}

	if err := m.ensureSourceLine(); err != nil {
		return fmt.Errorf("failed to configure shell: %w", err)
	}

	return nil
}

func (m *UnixPathManager) RemoveSdk(sdkType string, extraEnvVars map[string]string) error {
	err1 := m.writeEnvSh(sdkType, "", nil, "")
	err2 := m.writeFishEnvSh(sdkType, "", nil, "")
	if err1 != nil {
		return err1
	}
	return err2
}

func (m *UnixPathManager) GetCurrentConfig() (map[string]string, error) {
	data, err := os.ReadFile(m.cfg.EnvShPath())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	result := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "export ") {
			parts := strings.SplitN(line[7:], "=", 2)
			if len(parts) == 2 {
				result[parts[0]] = strings.Trim(parts[1], "\"")
			}
		}
	}
	return result, nil
}

func (m *UnixPathManager) GetAllPathEntries() ([]PathEntry, error) {
	pathEnv := os.Getenv("PATH")
	var entries []PathEntry
	for _, p := range strings.Split(pathEnv, ":") {
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
	return DeduplicateEntries(entries), nil
}

// CleanExternalPaths cleans non-SVC-managed external PATH entries from env.sh and env.sh.fish matching the same SDK type and version
func (m *UnixPathManager) CleanExternalPaths(sdkType string, version string, sourcePath string) {
	envPath := m.cfg.EnvShPath()
	m.cleanExternalFromEnvSh(envPath, sdkType, version, sourcePath)

	fishEnvPath := filepath.Join(m.cfg.SvcDir(), "env.sh.fish")
	m.cleanExternalFromFishSh(fishEnvPath, sdkType, version, sourcePath)
}

func (m *UnixPathManager) cleanExternalFromEnvSh(envPath string, sdkType string, version string, sourcePath string) {
	existing := m.parseEnvSh(envPath)
	pathVal, ok := existing["PATH"]
	if !ok {
		return
	}

	svcDir := m.cfg.SvcDir()
	parts := strings.Split(pathVal, ":")
	var filtered []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "$PATH" {
			if p == "$PATH" {
				filtered = append(filtered, p)
			}
			continue
		}
		if m.isExternalMatch(p, sdkType, version, svcDir, sourcePath) {
			continue
		}
		filtered = append(filtered, p)
	}

	if len(filtered) <= 1 {
		delete(existing, "PATH")
	} else {
		existing["PATH"] = strings.Join(filtered, ":")
	}

	var lines []string
	lines = append(lines, "# SVC Environment Configuration - Auto-generated, do not edit manually")
	for k, v := range existing {
		lines = append(lines, fmt.Sprintf("export %s=\"%s\"", k, v))
	}
	os.WriteFile(envPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

func (m *UnixPathManager) cleanExternalFromFishSh(fishEnvPath string, sdkType string, version string, sourcePath string) {
	existing := m.parseFishEnv(fishEnvPath)
	pathVal, ok := existing["PATH"]
	if !ok {
		return
	}

	svcDir := m.cfg.SvcDir()
	parts := strings.Split(pathVal, ":")
	var filtered []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if m.isExternalMatch(p, sdkType, version, svcDir, sourcePath) {
			continue
		}
		filtered = append(filtered, p)
	}

	if len(filtered) == 0 {
		delete(existing, "PATH")
	} else {
		existing["PATH"] = strings.Join(filtered, ":")
	}

	var lines []string
	lines = append(lines, "# SVC Environment Configuration - Auto-generated, do not edit manually")
	for k, v := range existing {
		if k == "PATH" {
			lines = append(lines, fmt.Sprintf("set -gx PATH \"%s\" $PATH", v))
		} else {
			lines = append(lines, fmt.Sprintf("set -gx %s \"%s\"", k, v))
		}
	}
	os.WriteFile(fishEnvPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

func (m *UnixPathManager) isExternalMatch(path string, sdkType string, version string, svcDir string, sourcePath string) bool {
	if strings.Contains(path, ".svc") || strings.HasPrefix(path, svcDir) {
		return false
	}
	if sourcePath != "" && filepath.Clean(path) == filepath.Clean(sourcePath) {
		return true
	}
	if sourcePath != "" {
		sourceRoot := DetectSdkRoot(sourcePath, sdkType)
		pRoot := DetectSdkRoot(path, sdkType)
		if sourceRoot != "" && pRoot != "" && filepath.Clean(pRoot) == filepath.Clean(sourceRoot) {
			return true
		}
	}
	detected := detectSdkTypeFromPath(path)
	if detected == "" {
		detected = detectSdkTypeByBin(path)
	}
	if detected != sdkType {
		return false
	}
	if version != "" && strings.Contains(path, version) {
		return true
	}
	return false
}

func (m *UnixPathManager) writeEnvSh(sdkType string, binPath string, extraEnvVars map[string]string, versionDir string) error {
	envPath := m.cfg.EnvShPath()
	existing := m.parseEnvSh(envPath)

	sdkDir := m.cfg.SdkDir(sdkType)
	pathParts := strings.Split(existing["PATH"], ":")
	var filteredPath []string
	for _, p := range pathParts {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, sdkDir) {
			continue
		}
		if p == "$PATH" {
			continue
		}
		filteredPath = append(filteredPath, p)
	}

	if binPath != "" {
		filteredPath = append([]string{binPath}, filteredPath...)
	}
	filteredPath = append(filteredPath, "$PATH")

	if len(filteredPath) > 1 {
		existing["PATH"] = strings.Join(filteredPath, ":")
	} else {
		delete(existing, "PATH")
	}

	for key := range extraEnvVars {
		if binPath != "" {
			value := versionDir
			if relPath, ok := extraEnvVars[key]; ok && relPath != "" {
				value = filepath.Join(versionDir, relPath)
			}
			existing[key] = value
		} else {
			delete(existing, key)
		}
	}

	var lines []string
	lines = append(lines, "# SVC Environment Configuration - Auto-generated, do not edit manually")
	for k, v := range existing {
		lines = append(lines, fmt.Sprintf("export %s=\"%s\"", k, v))
	}

	return os.WriteFile(envPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

func (m *UnixPathManager) writeFishEnvSh(sdkType string, binPath string, extraEnvVars map[string]string, versionDir string) error {
	fishEnvPath := filepath.Join(m.cfg.SvcDir(), "env.sh.fish")
	existing := m.parseFishEnv(fishEnvPath)

	sdkDir := m.cfg.SdkDir(sdkType)

	if oldPath, ok := existing["PATH"]; ok {
		parts := strings.Split(oldPath, ":")
		var filtered []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" && !strings.HasPrefix(p, sdkDir) {
				filtered = append(filtered, p)
			}
		}
		if binPath != "" {
			filtered = append([]string{binPath}, filtered...)
		}
		if len(filtered) > 0 {
			existing["PATH"] = strings.Join(filtered, ":")
		} else {
			delete(existing, "PATH")
		}
	} else if binPath != "" {
		existing["PATH"] = binPath
	}

	for key := range extraEnvVars {
		if binPath != "" {
			value := versionDir
			if relPath, ok := extraEnvVars[key]; ok && relPath != "" {
				value = filepath.Join(versionDir, relPath)
			}
			existing[key] = value
		} else {
			delete(existing, key)
		}
	}

	var lines []string
	lines = append(lines, "# SVC Environment Configuration - Auto-generated, do not edit manually")
	for k, v := range existing {
		if k == "PATH" {
			lines = append(lines, fmt.Sprintf("set -gx PATH \"%s\" $PATH", v))
		} else {
			lines = append(lines, fmt.Sprintf("set -gx %s \"%s\"", k, v))
		}
	}

	return os.WriteFile(fishEnvPath, []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

func (m *UnixPathManager) parseEnvSh(path string) map[string]string {
	existing := make(map[string]string)
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "export ") {
				parts := strings.SplitN(line[7:], "=", 2)
				if len(parts) == 2 {
					existing[parts[0]] = strings.Trim(parts[1], "\"")
				}
			}
		}
	}
	return existing
}

func (m *UnixPathManager) parseFishEnv(path string) map[string]string {
	existing := make(map[string]string)
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "set -gx ") {
				parts := strings.Fields(line[8:])
				if len(parts) >= 2 {
					key := parts[0]
					if parts[1] == "$PATH" {
						continue
					}
					existing[key] = strings.Trim(parts[1], "\"")
				}
			}
		}
	}
	return existing
}

func (m *UnixPathManager) ensureSourceLine() error {
	envPath := m.cfg.EnvShPath()
	posixSourceLine := fmt.Sprintf("[ -f %s ] && source %s", envPath, envPath)

	shellFiles := m.getShellConfigFiles()
	var lastErr error
	written := 0
	for _, file := range shellFiles {
		if err := m.appendIfNotExists(file, posixSourceLine, envPath); err != nil {
			lastErr = err
			continue
		}
		written++
	}

	fishEnvPath := filepath.Join(m.cfg.SvcDir(), "env.sh.fish")
	fishSourceLine := fmt.Sprintf("test -f %s; and source %s", fishEnvPath, fishEnvPath)
	fishFiles := m.getFishConfigFiles()
	for _, file := range fishFiles {
		if err := m.appendIfNotExists(file, fishSourceLine, fishEnvPath); err != nil {
			lastErr = err
			continue
		}
		written++
	}

	omzFiles := m.getOhMyZshConfigFiles()
	if len(omzFiles) > 0 {
		omzContent := fmt.Sprintf("[ -f %s ] && source %s", envPath, envPath)
		for _, file := range omzFiles {
			dir := filepath.Dir(file)
			if err := os.MkdirAll(dir, 0755); err != nil {
				continue
			}
			if err := os.WriteFile(file, []byte(omzContent+"\n"), 0644); err != nil {
				lastErr = err
			} else {
				written++
			}
		}
	}

	if written == 0 && lastErr != nil {
		return fmt.Errorf("unable to write to any shell config file: %w", lastErr)
	}
	return nil
}

func (m *UnixPathManager) getShellConfigFiles() []string {
	home := m.cfg.HomeDir()
	files := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".zshenv"),
	}
	if runtime.GOOS == "darwin" {
		files = append(files, filepath.Join(home, ".bash_profile"))
	}
	if runtime.GOOS == "linux" {
		files = append(files, filepath.Join(home, ".profile"))
	}
	return files
}

func (m *UnixPathManager) getFishConfigFiles() []string {
	home := m.cfg.HomeDir()
	return []string{
		filepath.Join(home, ".config", "fish", "conf.d", "svc.fish"),
	}
}

func (m *UnixPathManager) getOhMyZshConfigFiles() []string {
	home := m.cfg.HomeDir()
	omzDir := filepath.Join(home, ".oh-my-zsh")
	if _, err := os.Stat(omzDir); err != nil {
		return nil
	}
	return []string{
		filepath.Join(omzDir, "custom", "env", "svc.zsh"),
	}
}

func (m *UnixPathManager) appendIfNotExists(file, line, checkStr string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	content := string(data)
	if strings.Contains(content, checkStr) {
		return nil
	}

	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString("\n" + line + "\n")
	return err
}

// DetectSystemConflicts checks whether system-level config files contain matching env var configs for the SDK
func (m *UnixPathManager) DetectSystemConflicts(sdkType string, envKeys []string) []string {
	var conflicts []string
	sdkDir := m.cfg.SdkDir(sdkType)

	systemFiles := m.getSystemConfigFiles()
	for _, file := range systemFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.Contains(line, "PATH") || strings.Contains(line, "path") {
				for _, word := range strings.Fields(line) {
					word = strings.Trim(word, "\"':=")
					if word == "" || word == "$PATH" {
						continue
					}
					if strings.HasPrefix(word, sdkDir) {
						conflicts = append(conflicts, fmt.Sprintf("%s: %s", filepath.Base(file), word))
						continue
					}
					detected := detectSdkTypeByBin(word)
					if detected == "" {
						detected = detectSdkTypeFromPath(word)
					}
					if detected == sdkType {
						conflicts = append(conflicts, fmt.Sprintf("%s: %s", filepath.Base(file), word))
					}
				}
			}
			for _, key := range envKeys {
				if strings.Contains(line, key) {
					conflicts = append(conflicts, fmt.Sprintf("%s: %s", filepath.Base(file), line))
				}
			}
		}
	}

	return conflicts
}

func (m *UnixPathManager) getSystemConfigFiles() []string {
	var files []string
	if runtime.GOOS == "darwin" {
		files = append(files,
			"/etc/paths",
			"/etc/profile",
			"/etc/zshenv",
		)
		// Files in /etc/paths.d/
		if entries, err := os.ReadDir("/etc/paths.d"); err == nil {
			for _, e := range entries {
				files = append(files, filepath.Join("/etc/paths.d", e.Name()))
			}
		}
	}
	if runtime.GOOS == "linux" {
		files = append(files,
			"/etc/environment",
			"/etc/profile",
			"/etc/bash.bashrc",
			"/etc/zsh/zshenv",
		)
		if entries, err := os.ReadDir("/etc/profile.d"); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".sh") {
					files = append(files, filepath.Join("/etc/profile.d", e.Name()))
				}
			}
		}
	}
	return files
}
