package main

import (
	"fmt"
	"os"
	"path/filepath"

	"sdk_version_control/internal/logger"
	"sdk_version_control/internal/sdk"
)

type StorageInfo struct {
	SdkType      string `json:"sdkType"`
	DisplayName  string `json:"displayName"`
	SdkDir       string `json:"sdkDir"`
	TotalSize    int64  `json:"totalSize"`
	VersionCount int    `json:"versionCount"`
	ActiveVer    string `json:"activeVer"`
}

func (a *App) UninstallVersion(sdkType string, version string) error {
	if err := validatePathSegment(sdkType); err != nil {
		return err
	}
	if err := validatePathSegment(version); err != nil {
		return err
	}

	logger.Info("Uninstalling %s version: %s", sdkType, version)

	active := a.cfg.GetActiveVersion(sdkType)
	if active == version {
		logger.Warn("Cannot uninstall active version: %s %s", sdkType, version)
		return fmt.Errorf("cannot uninstall the currently active version, please switch to another version first")
	}

	versionDir := a.cfg.SdkVersionDir(sdkType, version)
	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		logger.Error("Version directory does not exist: %s", versionDir)
		return fmt.Errorf("version directory does not exist: %s", version)
	}

	if err := os.RemoveAll(versionDir); err != nil {
		logger.Error("Failed to delete version directory %s: %v", versionDir, err)
		return fmt.Errorf("failed to delete version directory: %w", err)
	}

	logger.Info("Successfully uninstalled %s version %s", sdkType, version)
	return nil
}

func (a *App) GetStorageInfo() []StorageInfo {
	var infos []StorageInfo
	for _, t := range sdk.AllSdkTypes() {
		sdkDir := a.cfg.SdkDir(string(t))
		entries, err := os.ReadDir(sdkDir)
		if err != nil {
			continue
		}

		var totalSize int64
		var versionCount int
		for _, e := range entries {
			if e.IsDir() {
				versionCount++
				totalSize += dirSize(filepath.Join(sdkDir, e.Name()))
			}
		}

		if versionCount > 0 {
			infos = append(infos, StorageInfo{
				SdkType:      string(t),
				DisplayName:  sdk.SdkDisplayName(t),
				SdkDir:       sdkDir,
				TotalSize:    totalSize,
				VersionCount: versionCount,
				ActiveVer:    a.cfg.GetActiveVersion(string(t)),
			})
		}
	}
	return infos
}

func (a *App) GetTmpCacheSize() int64 {
	return dirSize(a.cfg.TmpDir())
}

func (a *App) CleanTmpCache() error {
	tmpDir := a.cfg.TmpDir()
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		logger.Error("Failed to read cache directory %s: %v", tmpDir, err)
		return fmt.Errorf("failed to read cache directory: %w", err)
	}
	logger.Info("Cleaning temporary cache: %d items in %s", len(entries), tmpDir)
	for _, e := range entries {
		os.RemoveAll(filepath.Join(tmpDir, e.Name()))
	}
	logger.Info("Temporary cache cleaned")
	return nil
}

func (a *App) CleanInactiveVersions(sdkType string) error {
	if err := validatePathSegment(sdkType); err != nil {
		return err
	}

	active := a.cfg.GetActiveVersion(sdkType)
	sdkDir := a.cfg.SdkDir(sdkType)
	entries, err := os.ReadDir(sdkDir)
	if err != nil {
		logger.Error("Failed to read directory %s: %v", sdkDir, err)
		return fmt.Errorf("failed to read directory: %w", err)
	}

	var cleaned int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == active {
			continue
		}
		logger.Info("Removing inactive version: %s %s", sdkType, e.Name())
		if err := os.RemoveAll(filepath.Join(sdkDir, e.Name())); err != nil {
			logger.Error("Failed to delete %s: %v", e.Name(), err)
			return fmt.Errorf("failed to delete %s: %w", e.Name(), err)
		}
		cleaned++
	}
	logger.Info("Cleaned %d inactive versions for %s", cleaned, sdkType)
	return nil
}

func dirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		size += info.Size()
		return nil
	})
	return size
}
