package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"sdk_version_control/internal/config"
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

	if oldDir == newDir {
		return nil
	}

	if info, err := os.Stat(newDir); err == nil && info.IsDir() {
		return fmt.Errorf("目标目录已存在: %s", newDir)
	}

	if err := pathmgr.CopyDir(oldDir, newDir); err != nil {
		return fmt.Errorf("复制目录失败: %w", err)
	}

	installedSDKs := make(map[string]string)
	for _, sdkType := range sdk.AllSdkTypes() {
		activeVersion := a.cfg.GetActiveVersion(string(sdkType))
		if activeVersion != "" {
			installedSDKs[string(sdkType)] = activeVersion
		}
	}

	for sdkTypeStr := range installedSDKs {
		f := a.registry.Get(sdk.SdkType(sdkTypeStr))
		if f != nil {
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
		a.pathMgr.ConfigureSdk(sdkTypeStr, versionDir, f.GetBinDir(), f.GetExtraEnvVars())
	}

	s := a.settings.Get()
	s.InstallPath = newDir
	a.settings.Update(s)

	if err := os.RemoveAll(oldDir); err != nil {
		log.Printf("警告: 删除旧安装目录失败 (%s): %v", oldDir, err)
	}

	return nil
}
