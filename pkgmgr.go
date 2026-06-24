package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sdk_version_control/internal/sdk"
)

func (a *App) GetPackageManagers(sdkType string) []sdk.PackageManagerInfo {
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
	c := createCmd(fullPath, args...)
	c.Env = append(os.Environ(), "PATH="+scopedPath)
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
	return sdk.PackageManagerInfo{Name: name, Version: ver, Installed: true, ParentSdk: parent}
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
		return a.runScopedCommand("npm", sdk.NodeJS, "install", "-g", "yarn")
	case "pnpm":
		if a.cfg.GetActiveVersion("nodejs") == "" {
			return fmt.Errorf("please install Node.js first")
		}
		return a.runScopedCommand("npm", sdk.NodeJS, "install", "-g", "pnpm")
	case "composer":
		if a.cfg.GetActiveVersion("php") == "" {
			return fmt.Errorf("please install PHP first")
		}
		return fmt.Errorf("Composer requires manual download: https://getcomposer.org/download/")
	default:
		return fmt.Errorf("unknown package manager: %s", name)
	}
}

func (a *App) UpdatePackageManager(name string) error {
	switch name {
	case "npm":
		return a.runScopedCommand("npm", sdk.NodeJS, "install", "-g", "npm@latest")
	case "yarn":
		return a.runScopedCommand("npm", sdk.NodeJS, "install", "-g", "yarn@latest")
	case "pnpm":
		return a.runScopedCommand("npm", sdk.NodeJS, "install", "-g", "pnpm@latest")
	case "composer":
		return a.runScopedCommand("composer", sdk.PHP, "self-update")
	default:
		return fmt.Errorf("unknown package manager: %s", name)
	}
}

// buildSdkPath builds a PATH containing only the bin directory of the specified SDK's active version
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
	binDir := filepath.Join(versionDir, f.GetBinDir())
	return binDir
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

// runScopedCommand runs a command within the PATH scope of the specified SDK
func (a *App) runScopedCommand(name string, parent sdk.SdkType, args ...string) error {
	scopedPath := a.buildSdkPath(parent)
	fullPath := resolveInPath(name, scopedPath)
	cmd := createCmd(fullPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "PATH="+scopedPath)
	return cmd.Run()
}
