package main

import (
	"fmt"
	"os"
	"path/filepath"

	"sdk_version_control/internal/config"
	"sdk_version_control/internal/logger"
	"sdk_version_control/internal/pathmgr"
	"sdk_version_control/internal/sdk"
)

func (a *App) GetDefaultInstallPath() string {
	return config.DefaultSvcDir()
}

func (a *App) GetInstallPath() string {
	return a.cfg.SvcDir()
}

func (a *App) MigrateInstallPath(newPath string) error {
	oldDir := a.cfg.SvcDir()
	newDir := filepath.Clean(newPath)

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

	logger.Info("Updating PATH configuration for %d active SDKs", len(installedSDKs))

	for sdkTypeStr := range installedSDKs {
		f := a.registry.Get(sdk.SdkType(sdkTypeStr))
		if f != nil {
			logger.Info("Removing old PATH entry for: %s", sdkTypeStr)
			a.pathMgr.RemoveSdk(sdkTypeStr, f.GetExtraEnvVars())
		}
	}

	a.cfg.SetSvcDir(newDir)

	for sdkTypeStr, activeVersion := range installedSDKs {
		f := a.registry.Get(sdk.SdkType(sdkTypeStr))
		if f == nil {
			continue
		}
		versionDir := a.cfg.SdkVersionDir(sdkTypeStr, activeVersion)
		logger.Info("Adding new PATH entry for: %s %s", sdkTypeStr, activeVersion)
		a.pathMgr.ConfigureSdk(sdkTypeStr, versionDir, f.GetBinDir(), f.GetExtraEnvVars())
	}

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
