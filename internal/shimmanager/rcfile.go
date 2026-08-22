package shimmanager

import (
	"os"
	"path/filepath"
	"sort"

	"svc/internal/config"
	"svc/internal/shim"
)

// envVarEntry represents a single environment variable to write to .svc.rc
type envVarEntry struct {
	Key   string
	Value string // absolute path
}

// collectEnvVars reads shims.json and config.json to build the list of
// environment variables that should appear in .svc.rc.
func (m *Manager) collectEnvVars() []envVarEntry {
	cfg, _ := m.loadShimConfig()
	svcHome := m.cfg.SvcDir()

	var entries []envVarEntry

	// Collect SDK env vars in deterministic order
	sdkTypes := make([]string, 0, len(cfg.SdkTypes))
	for st := range cfg.SdkTypes {
		sdkTypes = append(sdkTypes, st)
	}
	sort.Strings(sdkTypes)

	for _, sdkType := range sdkTypes {
		sdkCfg := cfg.SdkTypes[sdkType]
		version := m.cfg.GetActiveVersion(sdkType)
		if version == "" {
			continue
		}
		versionDir := filepath.Join(svcHome, sdkType, version)

		// Env var keys in deterministic order
		keys := make([]string, 0, len(sdkCfg.EnvVars))
		for k := range sdkCfg.EnvVars {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, key := range keys {
			relPath := sdkCfg.EnvVars[key]
			val := versionDir
			if relPath != "" {
				val = filepath.Join(versionDir, relPath)
			}
			entries = append(entries, envVarEntry{Key: key, Value: val})
		}
	}

	return entries
}

// updateRcFile regenerates the .svc.rc file based on current config.
func (m *Manager) updateRcFile() error {
	envVars := m.collectEnvVars()
	content := m.generateRcContent(envVars)
	// Mirror env vars into the OS-level store on platforms that have one
	// (Windows registry). No-op on Unix, where .svc.rc is the only store.
	m.applyEnvVarsToSystem(envVars)
	// Item 6: keep env.sh.fish in sync too (fish users get PATH + env vars).
	// No-op on Windows.
	m.writeFishEnvFile(envVars)
	rcPath := m.cfg.RcFilePath()
	return os.WriteFile(rcPath, []byte(content), 0644)
}

// RefreshRcFile regenerates .svc.rc to reflect the latest active versions.
// Call this after SetActiveVersion so .svc.rc env var lines (JAVA_HOME,
// GOROOT, etc.) point at the newly-active version instead of the previous one.
// ConfigureSdk calls updateRcFile internally, but it runs before
// SetActiveVersion in the install/switch flow, so the env vars it writes are
// stale until this method is called.
func (m *Manager) RefreshRcFile() error {
	return m.updateRcFile()
}

// GetRcFileContent returns the current .svc.rc content (for display in UI).
func (m *Manager) GetRcFileContent() (string, error) {
	data, err := os.ReadFile(m.cfg.RcFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// GetShimConfig returns the current shims.json config (for display in UI).
func (m *Manager) GetShimConfig() shim.ShimConfig {
	cfg, _ := m.loadShimConfig()
	return cfg
}

// GetConfiguredShells returns the list of shell config files that contain
// the SVC source line (for display in UI).
func (m *Manager) GetConfiguredShells() []string {
	return m.detectConfiguredShells()
}

// RemoveAllShells removes the SVC source line from all shell config files.
func (m *Manager) RemoveAllShells() error {
	return m.removeAllSourceLines()
}

// AddShell adds the SVC source line to a specific shell config file.
func (m *Manager) AddShell(shellName string) error {
	return m.addSourceLine(shellName)
}

// AvailableShells returns the list of shells that can be configured.
func (m *Manager) AvailableShells() []config.ShellInfo {
	return config.AvailableShells()
}
