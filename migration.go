package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"sdk_version_control/internal/config"
	"sdk_version_control/internal/logger"
	"sdk_version_control/internal/pathmgr"
	"sdk_version_control/internal/sdk"
)

// isSystemPath returns true if the path is a known OS system directory
// where SVC data should never be placed (would corrupt the OS).
func isSystemPath(path string) bool {
	cleaned := strings.ToLower(filepath.Clean(path))
	if runtime.GOOS == "windows" {
		systemRoots := []string{`c:\windows`, `c:\program files`, `c:\program files (x86)`, `c:\programdata`, `c:\system32`}
		for _, root := range systemRoots {
			if cleaned == root || strings.HasPrefix(cleaned, root+`\`) {
				return true
			}
		}
	} else {
		systemRoots := []string{"/usr", "/bin", "/sbin", "/etc", "/var", "/boot", "/dev", "/proc", "/sys", "/root", "/lib"}
		for _, root := range systemRoots {
			if cleaned == root || strings.HasPrefix(cleaned, root+"/") {
				return true
			}
		}
	}
	return false
}

func (a *App) GetDefaultInstallPath() string {
	return config.DefaultSvcDir()
}

func (a *App) GetInstallPath() string {
	return a.cfg.SvcDir()
}

func (a *App) MigrateInstallPath(newPath string) error {
	oldDir := a.cfg.SvcDir()
	newDir := filepath.Clean(newPath)

	// N2: Reject system directories that would corrupt the OS if overwritten.
	if isSystemPath(newDir) {
		return fmt.Errorf("cannot migrate to system directory: %s", newDir)
	}

	logger.Info("Starting install path migration: %s -> %s", oldDir, newDir)

	if oldDir == newDir {
		logger.Info("Source and target are the same, skipping migration")
		return nil
	}

	if info, err := os.Stat(newDir); err == nil && info.IsDir() {
		logger.Error("Target directory already exists: %s", newDir)
		return fmt.Errorf("target directory already exists: %s", newDir)
	}

	// Backup old directory to desktop, failure does not block migration (only logs warning)
	backupPath, backupErr := pathmgr.BackupDir(oldDir)
	if backupErr != nil {
		logger.Warn("Failed to backup old install directory: %v", backupErr)
	} else {
		logger.Info("Old install directory backed up to: %s", backupPath)
	}

	logger.Info("Copying files from %s to %s", oldDir, newDir)
	if err := pathmgr.CopyDir(oldDir, newDir); err != nil {
		logger.Error("Failed to copy directory: %v", err)
		return fmt.Errorf("failed to copy directory: %w", err)
	}
	logger.Info("File copy completed")

	installedSDKs := make(map[string]string)
	for _, sdkType := range sdk.AllSdkTypes() {
		activeVersion := a.cfg.GetActiveVersion(string(sdkType))
		if activeVersion != "" {
			installedSDKs[string(sdkType)] = activeVersion
		}
	}

	// Switch the config to the new directory. The shell rc source line points
	// to the fixed ~/.svc.rc location, so it never needs updating; only the
	// SVC_HOME variable inside .svc.rc changes.
	a.cfg.SetSvcDir(newDir)

	// Re-run shim setup at the new location: recreates the shims dir, refreshes
	// the shim binary, and regenerates .svc.rc with the new SVC_HOME.
	if err := a.shimMgr.EnsureSetup(); err != nil {
		logger.Warn("Shim setup at new location failed: %v", err)
	}

	// Re-create shims for every active SDK at the new install path.
	logger.Info("Re-configuring %d active SDKs at new location", len(installedSDKs))
	for sdkTypeStr, activeVersion := range installedSDKs {
		f := a.registry.Get(sdk.SdkType(sdkTypeStr))
		if f == nil {
			continue
		}
		versionDir := a.cfg.SdkVersionDir(sdkTypeStr, activeVersion)
		logger.Info("Re-configuring: %s %s", sdkTypeStr, activeVersion)
		if err := a.pathMgr.ConfigureSdk(sdkTypeStr, versionDir, f.GetBinDirs(), f.GetExtraEnvVars()); err != nil {
			logger.Warn("Failed to re-configure %s: %v", sdkTypeStr, err)
		}
	}

	// N3: Persist the new installPath BEFORE deleting the old dir.
	// settings.json lives at ~/.svc/ (fixed location, not inside install dir).
	// If oldDir was ~/.svc itself, RemoveAll would delete settings.json;
	// saving first ensures the new path is recorded even if deletion fails.
	// If oldDir != ~/.svc, settings.json is untouched by RemoveAll.
	s := a.settings.Get()
	s.InstallPath = newDir
	if err := a.settings.Update(s); err != nil {
		logger.Error("Failed to save settings: %v", err)
	}

	logger.Info("Removing old install directory: %s", oldDir)
	if err := os.RemoveAll(oldDir); err != nil {
		logger.Warn("Failed to delete old install directory (%s): %v", oldDir, err)
	}

	logger.Info("Install path migration completed successfully")
	return nil
}
