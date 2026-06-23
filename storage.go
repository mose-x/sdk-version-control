package main

import (
	"fmt"
	"os"
	"path/filepath"

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

	active := a.cfg.GetActiveVersion(sdkType)
	if active == version {
		return fmt.Errorf("不能卸载当前活跃版本，请先切换到其他版本")
	}

	versionDir := a.cfg.SdkVersionDir(sdkType, version)
	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		return fmt.Errorf("版本目录不存在: %s", version)
	}

	if err := os.RemoveAll(versionDir); err != nil {
		return fmt.Errorf("删除版本目录失败: %w", err)
	}

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
		return fmt.Errorf("读取缓存目录失败: %w", err)
	}
	for _, e := range entries {
		os.RemoveAll(filepath.Join(tmpDir, e.Name()))
	}
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
		return fmt.Errorf("读取目录失败: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == active {
			continue
		}
		if err := os.RemoveAll(filepath.Join(sdkDir, e.Name())); err != nil {
			return fmt.Errorf("删除 %s 失败: %w", e.Name(), err)
		}
	}
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
