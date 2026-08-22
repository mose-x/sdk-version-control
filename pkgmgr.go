package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"sdk_version_control/internal/helpers"
	"sdk_version_control/internal/sdk"
)

func (a *App) GetPackageManagers(sdkType string) []sdk.PackageManagerInfo {
	if err := helpers.ValidatePathSegment(sdkType); err != nil {
		return nil
	}
	active := a.cfg.GetActiveVersion(sdkType)
	if active == "" {
		return nil
	}

	switch sdk.SdkType(sdkType) {
	case sdk.NodeJS:
		return []sdk.PackageManagerInfo{
			a.detectPM("npm", "npm", []string{"--version"}, sdk.NodeJS),
			a.detectPM("yarn", "yarn", []string{"--version"}, sdk.NodeJS),
			a.detectPM("pnpm", "pnpm", []string{"--version"}, sdk.NodeJS),
		}
	case sdk.PHP:
		return []sdk.PackageManagerInfo{
			a.detectPM("composer", "composer", []string{"--version"}, sdk.PHP),
		}
	case sdk.Python:
		if runtime.GOOS == "windows" {
			return []sdk.PackageManagerInfo{
				a.detectPM("pip", "python", []string{"-m", "pip", "--version"}, sdk.Python),
			}
		}
		return []sdk.PackageManagerInfo{
			a.detectPM("pip", "pip", []string{"--version"}, sdk.Python),
		}
	default:
		return nil
	}
}

func (a *App) detectPM(name, cmd string, args []string, parent sdk.SdkType) sdk.PackageManagerInfo {
	scopedPath := a.buildSdkPath(parent)
	fullPath := resolveInPath(cmd, scopedPath)
	if fullPath == cmd {
		return sdk.PackageManagerInfo{Name: name, Installed: false, ParentSdk: parent}
	}
	// H3: Bound version detection so a hung package manager doesn't block.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := helpers.CreateCmdContext(ctx, fullPath, args...)
	c.Env = helpers.ReplacePathEnv(os.Environ(), scopedPath)
	out, err := c.CombinedOutput()
	if err != nil {
		return sdk.PackageManagerInfo{Name: name, Installed: false, ParentSdk: parent}
	}
	ver := strings.TrimSpace(string(out))
	if strings.Contains(ver, "Composer version") {
		parts := strings.Fields(ver)
		if len(parts) >= 3 {
			ver = parts[2]
		}
	}
	if name == "pip" {
		ver = parsePipVersion(string(out))
	}
	return sdk.PackageManagerInfo{Name: name, Version: ver, Installed: true, ParentSdk: parent}
}

func parsePipVersion(raw string) string {
	ver := strings.TrimSpace(raw)
	if strings.HasPrefix(ver, "pip ") {
		parts := strings.Fields(ver)
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	return ver
}

// nodeSupportsCorepack returns true if the Node.js version is >= 16.9.0
// (corepack was introduced in Node.js 16.9.0). Falls back to false on parse error.
func nodeSupportsCorepack(version string) bool {
	version = strings.TrimPrefix(version, "v")
	parts := strings.Split(version, ".")
	major, _ := strconv.Atoi(parts[0])
	// M5: Any major > 16 supports corepack. Checked before the len(parts) < 2
	// guard so single-part versions like "18" (no minor) still return true
	// instead of falling through to false.
	if major > 16 {
		return true
	}
	if len(parts) < 2 {
		return false
	}
	minor, _ := strconv.Atoi(parts[1])
	if major == 16 && minor >= 9 {
		return true
	}
	return false
}

func (a *App) InstallPackageManager(name string) error {
	switch name {
	case "npm":
		if a.cfg.GetActiveVersion("nodejs") == "" {
			return fmt.Errorf("please install Node.js first")
		}
		return fmt.Errorf("npm is installed with Node.js, please install Node.js first")
	case "yarn":
		if a.cfg.GetActiveVersion("nodejs") == "" {
			return fmt.Errorf("please install Node.js first")
		}
		if nodeSupportsCorepack(a.cfg.GetActiveVersion("nodejs")) {
			if err := a.runScopedCommand("corepack", sdk.NodeJS, "enable"); err != nil {
				return err
			}
			return a.runScopedCommand("corepack", sdk.NodeJS, "prepare", "yarn@latest", "--activate")
		}
		return a.runScopedCommand("npm", sdk.NodeJS, "install", "-g", "yarn")
	case "pnpm":
		if a.cfg.GetActiveVersion("nodejs") == "" {
			return fmt.Errorf("please install Node.js first")
		}
		if nodeSupportsCorepack(a.cfg.GetActiveVersion("nodejs")) {
			if err := a.runScopedCommand("corepack", sdk.NodeJS, "enable"); err != nil {
				return err
			}
			return a.runScopedCommand("corepack", sdk.NodeJS, "prepare", "pnpm@latest", "--activate")
		}
		return a.runScopedCommand("npm", sdk.NodeJS, "install", "-g", "pnpm")
	case "composer":
		if a.cfg.GetActiveVersion("php") == "" {
			return fmt.Errorf("please install PHP first")
		}
		return fmt.Errorf("Composer requires manual download: https://getcomposer.org/download/")
	case "pip":
		if a.cfg.GetActiveVersion("python") == "" {
			return fmt.Errorf("please install Python first")
		}
		return fmt.Errorf("pip is installed with Python, please install Python first")
	default:
		return fmt.Errorf("unknown package manager: %s", name)
	}
}

func (a *App) UpdatePackageManager(name string) error {
	switch name {
	case "npm":
		return a.runScopedCommand("npm", sdk.NodeJS, "install", "-g", "npm@latest")
	case "yarn":
		if nodeSupportsCorepack(a.cfg.GetActiveVersion("nodejs")) {
			return a.runScopedCommand("corepack", sdk.NodeJS, "prepare", "yarn@latest", "--activate")
		}
		return a.runScopedCommand("npm", sdk.NodeJS, "install", "-g", "yarn@latest")
	case "pnpm":
		if nodeSupportsCorepack(a.cfg.GetActiveVersion("nodejs")) {
			return a.runScopedCommand("corepack", sdk.NodeJS, "prepare", "pnpm@latest", "--activate")
		}
		return a.runScopedCommand("npm", sdk.NodeJS, "install", "-g", "pnpm@latest")
	case "composer":
		return a.runScopedCommand("composer", sdk.PHP, "self-update")
	case "pip":
		return a.runScopedCommand("python", sdk.Python, "-m", "pip", "install", "--upgrade", "pip")
	default:
		return fmt.Errorf("unknown package manager: %s", name)
	}
}

// buildSdkPath builds a PATH containing only the bin directories of the specified SDK's active version
func (a *App) buildSdkPath(parent sdk.SdkType) string {
	active := a.cfg.GetActiveVersion(string(parent))
	if active == "" {
		return ""
	}
	f := a.registry.Get(parent)
	if f == nil {
		return ""
	}
	versionDir := a.cfg.SdkVersionDir(string(parent), active)
	var paths []string
	for _, binDir := range f.GetBinDirs() {
		if binDir == "" {
			paths = append(paths, versionDir)
		} else {
			paths = append(paths, filepath.Join(versionDir, binDir))
		}
	}
	sep := ":"
	if os.PathListSeparator == ';' {
		sep = ";"
	}
	return strings.Join(paths, sep)
}

// resolveInPath looks up a command in the specified PATH (bypasses system PATH)
func resolveInPath(cmd, searchPath string) string {
	if searchPath == "" {
		return cmd
	}
	sep := ";"
	exts := []string{""}
	if os.PathListSeparator == ':' {
		sep = ":"
	} else {
		exts = []string{"", ".exe", ".cmd", ".bat"}
	}
	for _, dir := range strings.Split(searchPath, sep) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		for _, ext := range exts {
			p := filepath.Join(dir, cmd+ext)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p
			}
		}
	}
	return cmd
}

// scopedCommandTimeout bounds package-manager install/update commands so a
// hung process doesn't block forever. 180s (not 60s): corepack prepare and
// npm install -g routinely exceed a minute on slow networks or registries,
// and a mid-install timeout leaves the package manager in a half-done state.
const scopedCommandTimeout = 180 * time.Second

// newScopedCommandContext returns a context bounded by scopedCommandTimeout
// for runScopedCommand. Extracted so the bound is unit-testable.
func newScopedCommandContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), scopedCommandTimeout)
}

// runScopedCommand runs a command within the PATH scope of the specified SDK
func (a *App) runScopedCommand(name string, parent sdk.SdkType, args ...string) error {
	scopedPath := a.buildSdkPath(parent)
	fullPath := resolveInPath(name, scopedPath)
	// H3: Bound install/update commands so a hung process doesn't block forever.
	ctx, cancel := newScopedCommandContext()
	defer cancel()
	cmd := helpers.CreateCmdContext(ctx, fullPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = helpers.ReplacePathEnv(os.Environ(), scopedPath)
	return cmd.Run()
}
