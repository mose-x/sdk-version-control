package shim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ShimConfig is the shims.json structure that maps commands to SDK types
type ShimConfig struct {
	Commands map[string]string       `json:"commands"` // command name -> sdkType
	SdkTypes map[string]SdkShimEntry `json:"sdkTypes"` // sdkType -> config
}

// SdkShimEntry describes how to route a command for a given SDK type
type SdkShimEntry struct {
	BinDirs []string          `json:"binDirs"` // relative bin dirs (e.g. ["bin"], ["go/bin"], ["cargo/bin","rustc/bin"]); "" = versionDir itself
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

// commandAliases maps a command to an alternate name to try when the real
// binary isn't found in the SDK's bin dirs. python3 -> python: Windows CPython
// ships python.exe but no python3.exe (python3 is a Unix convention); the
// shimmanager registers python3 as an alias of python, and this fallback
// resolves it to the real python binary so `python3` works on Windows.
var commandAliases = map[string]string{
	"python3": "python",
}

// Run executes the shim: looks up the real binary and execs it.
// This is called when the app binary is invoked via a hardlink named after a
// command, or via a .cmd/.bat wrapper that delegates to svc-shim.exe.
func Run() {
	// On Windows the binary is built with -H windowsgui (no console), so
	// stdio handles are invalid until we attach to the parent's console.
	// This must happen before any fmt.Fprintln(os.Stderr, ...) so shim
	// diagnostics are visible, and before execBinary so the spawned target
	// inherits valid handles.
	attachParentConsole()

	inv := parseInvocation()
	if inv.Command == "" {
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

	sdkType, ok := shimCfg.Commands[inv.Command]
	if !ok {
		fmt.Fprintf(os.Stderr, "shim: unknown command %q\n", inv.Command)
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

	realBinary := resolveRealBinaryMulti(versionDir, sdkCfg.BinDirs, inv.Command)
	if realBinary == "" {
		// python3 -> python: Windows CPython ships python.exe but no python3.exe
		// (a Unix convention). The shimmanager registers python3 as an alias of
		// python; resolve it to the real python binary. Matches Unix where
		// python3 exists as a real executable/symlink.
		if alias, ok := commandAliases[inv.Command]; ok {
			realBinary = resolveRealBinaryMulti(versionDir, sdkCfg.BinDirs, alias)
		}
	}
	if realBinary == "" {
		fmt.Fprintf(os.Stderr, "shim: executable not found for %q under %s\n", inv.Command, versionDir)
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

	execBinary(realBinary, inv.Args)
}

// resolveRealBinaryMulti finds the real executable for a command by trying
// each binDir (relative to versionDir) in order. First match wins. This
// supports SDKs that ship commands across multiple bin directories (e.g. Rust
// tarball has cargo/bin, rustc/bin, rustfmt-preview/bin).
func resolveRealBinaryMulti(versionDir string, binDirs []string, name string) string {
	for _, binDir := range binDirs {
		binPath := versionDir
		if binDir != "" {
			binPath = filepath.Join(versionDir, binDir)
		}
		if p := resolveRealBinary(binPath, name); p != "" {
			return p
		}
	}
	return ""
}

// resolveRealBinary finds the real executable for a command name in binPath.
// On Unix: returns <binPath>/<name>. On Windows: tries .exe, .cmd, .bat in
// order and returns the first existing match (commands may ship as any of
// these depending on the SDK, e.g. npm.cmd, gradle.bat).
func resolveRealBinary(binPath, name string) string {
	if runtime.GOOS == "windows" {
		for _, ext := range []string{".exe", ".cmd", ".bat"} {
			candidate := filepath.Join(binPath, name+ext)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
		return ""
	}
	candidate := filepath.Join(binPath, name)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
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
