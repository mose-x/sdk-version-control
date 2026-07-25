package shim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ShimConfig is the shims.json structure that maps commands to SDK types
type ShimConfig struct {
	Commands map[string]string       `json:"commands"` // command name -> sdkType
	SdkTypes map[string]SdkShimEntry `json:"sdkTypes"` // sdkType -> config
}

// SdkShimEntry describes how to route a command for a given SDK type
type SdkShimEntry struct {
	BinDir  string            `json:"binDir"`  // relative bin dir (e.g. "bin", "go/bin")
	EnvVars map[string]string `json:"envVars"` // key -> relative path ("" = versionDir itself)
}

// svcConfig is the config.json structure (partial, only what the shim needs)
type svcConfig struct {
	ActiveVersions map[string]string `json:"activeVersions"`
}

// settings is the settings.json structure (partial)
type settings struct {
	InstallPath string `json:"installPath"`
}

// Run executes the shim: looks up the real binary and execs it.
// This is called when the app binary is invoked via a hardlink named after a command.
func Run() {
	name := shimName()
	if name == "" {
		fmt.Fprintln(os.Stderr, "shim: cannot determine command name")
		os.Exit(1)
	}

	svcHome, err := resolveSvcHome()
	if err != nil {
		fmt.Fprintf(os.Stderr, "shim: cannot resolve SVC home: %v\n", err)
		os.Exit(1)
	}

	shimCfg, err := loadShimConfig(svcHome)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shim: cannot load shim config: %v\n", err)
		os.Exit(1)
	}

	sdkType, ok := shimCfg.Commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "shim: unknown command %q\n", name)
		os.Exit(1)
	}

	sdkCfg, ok := shimCfg.SdkTypes[sdkType]
	if !ok {
		fmt.Fprintf(os.Stderr, "shim: no config for SDK type %q\n", sdkType)
		os.Exit(1)
	}

	version, err := loadActiveVersion(svcHome, sdkType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shim: cannot load active version: %v\n", err)
		os.Exit(1)
	}
	if version == "" {
		fmt.Fprintf(os.Stderr, "shim: no active version set for %s\n", sdkType)
		os.Exit(1)
	}

	versionDir := filepath.Join(svcHome, sdkType, version)
	binPath := versionDir
	if sdkCfg.BinDir != "" {
		binPath = filepath.Join(versionDir, sdkCfg.BinDir)
	}

	realBinary := filepath.Join(binPath, name)
	if runtime.GOOS == "windows" {
		realBinary += ".exe"
	}

	if _, err := os.Stat(realBinary); err != nil {
		fmt.Fprintf(os.Stderr, "shim: executable not found: %s\n", realBinary)
		os.Exit(1)
	}

	// Set environment variables (JAVA_HOME, GOROOT, etc.)
	for key, relPath := range sdkCfg.EnvVars {
		val := versionDir
		if relPath != "" {
			val = filepath.Join(versionDir, relPath)
		}
		os.Setenv(key, val)
	}

	execBinary(realBinary)
}

// shimName extracts the command name from argv[0].
// e.g. /home/user/.svc/shims/go -> "go"
//      C:\Users\user\.svc\shims\go.exe -> "go"
func shimName() string {
	if len(os.Args) == 0 {
		return ""
	}
	name := filepath.Base(os.Args[0])
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(name, ".exe")
	}
	return name
}

// resolveSvcHome finds the .svc directory.
// It reads settings.json for a custom install path, falling back to ~/.svc.
func resolveSvcHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	defaultDir := filepath.Join(home, ".svc")

	// Try reading settings.json for a custom install path
	settingsPath := filepath.Join(defaultDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return defaultDir, nil
	}
	var s settings
	if err := json.Unmarshal(data, &s); err != nil {
		return defaultDir, nil
	}
	if s.InstallPath != "" {
		return s.InstallPath, nil
	}
	return defaultDir, nil
}

func loadShimConfig(svcHome string) (*ShimConfig, error) {
	path := filepath.Join(svcHome, "shims.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg ShimConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func loadActiveVersion(svcHome string, sdkType string) (string, error) {
	path := filepath.Join(svcHome, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var cfg svcConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", err
	}
	return cfg.ActiveVersions[sdkType], nil
}
