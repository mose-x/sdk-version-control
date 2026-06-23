package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"sdk_version_control/internal/extractor"
	"sdk_version_control/internal/pathmgr"
	"sdk_version_control/internal/sdk"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) SelectLocalFile() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("应用未初始化")
	}
	return wailsRuntime.OpenFileDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "选择压缩包文件",
		Filters: []wailsRuntime.FileFilter{
			{DisplayName: "压缩包", Pattern: "*.zip;*.tar.gz;*.tgz;*.tar.xz;*.7z"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
}

func (a *App) SelectLocalDir() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("应用未初始化")
	}
	return wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "选择 SDK 目录",
	})
}

func (a *App) ImportLocalSdk(sdkTypeStr string, localPath string) error {
	if err := validatePathSegment(sdkTypeStr); err != nil {
		return err
	}
	sdkType := sdk.SdkType(sdkTypeStr)
	f := a.registry.Get(sdkType)
	if f == nil {
		return fmt.Errorf("未知的SDK类型: %s", sdkTypeStr)
	}

	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("路径不存在: %s", localPath)
	}

	var sourceDir string

	if info.IsDir() {
		sourceDir = pathmgr.DetectSdkRoot(localPath, sdkTypeStr)
	} else {
		tmpDir := filepath.Join(a.cfg.TmpDir(), "import_"+filepath.Base(localPath))
		if err := os.RemoveAll(tmpDir); err != nil {
			return fmt.Errorf("清理临时目录失败: %w", err)
		}
		if err := os.MkdirAll(tmpDir, 0755); err != nil {
			return fmt.Errorf("创建临时目录失败: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		ext, err := extractor.NewExtractor(filepath.Base(localPath))
		if err != nil {
			return fmt.Errorf("不支持的压缩格式: %w", err)
		}
		if err := ext.Extract(localPath, tmpDir); err != nil {
			return fmt.Errorf("解压失败: %w", err)
		}
		if err := extractor.StripTopDir(tmpDir); err != nil {
			return fmt.Errorf("解压失败: %w", err)
		}
		sourceDir = pathmgr.DetectSdkRoot(tmpDir, sdkTypeStr)
	}

	var versionName string
	if ver, err := a.detectVersionFromDir(sourceDir, f); err == nil && ver != "" {
		versionName = ver
	} else {
		if err != nil {
			log.Printf("运行验证命令获取版本失败 (%s): %v", sdkTypeStr, err)
		}
		dirName := filepath.Base(sourceDir)
		versionName = pathmgr.ExtractVersion(dirName)
		if versionName == "" || versionName == "." || versionName == dirName {
			versionName = "imported"
		}
	}

	targetDir := a.cfg.SdkVersionDir(sdkTypeStr, versionName)
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("清理目标目录失败: %w", err)
	}

	if err := pathmgr.CopyDir(sourceDir, targetDir); err != nil {
		return fmt.Errorf("复制SDK失败: %w", err)
	}

	binDir := ""
	if _, err := os.Stat(filepath.Join(targetDir, "bin")); err == nil {
		binDir = "bin"
	}

	if err := a.pathMgr.ConfigureSdk(sdkTypeStr, targetDir, binDir, nil); err != nil {
		return fmt.Errorf("配置PATH失败: %w", err)
	}

	a.pathMgr.CleanExternalPaths(sdkTypeStr, versionName, sourceDir)

	return a.cfg.SetActiveVersion(sdkTypeStr, versionName)
}

func (a *App) ImportSdk(externalPath string, sdkType string) error {
	if err := validatePathSegment(sdkType); err != nil {
		return err
	}
	sdkRoot := pathmgr.DetectSdkRoot(externalPath, sdkType)

	dirName := filepath.Base(sdkRoot)
	versionName := pathmgr.ExtractVersion(dirName)
	if versionName == "" || versionName == "." {
		versionName = "imported"
	}

	targetDir := a.cfg.SdkVersionDir(sdkType, versionName)

	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("清理目标目录失败: %w", err)
	}

	if err := pathmgr.CopyDir(sdkRoot, targetDir); err != nil {
		return fmt.Errorf("复制SDK失败: %w", err)
	}

	binDir := ""
	if _, err := os.Stat(filepath.Join(targetDir, "bin")); err == nil {
		binDir = "bin"
	}

	if err := a.pathMgr.ConfigureSdk(sdkType, targetDir, binDir, nil); err != nil {
		return fmt.Errorf("配置PATH失败: %w", err)
	}

	a.pathMgr.CleanExternalPaths(sdkType, versionName, sdkRoot)

	return a.cfg.SetActiveVersion(sdkType, versionName)
}

func (a *App) ImportPathSdk(sdkTypeStr string) error {
	if err := validatePathSegment(sdkTypeStr); err != nil {
		return err
	}
	sdkType := sdk.SdkType(sdkTypeStr)
	f := a.registry.Get(sdkType)
	if f == nil {
		return fmt.Errorf("未知的SDK类型: %s", sdkTypeStr)
	}

	cmdName, _ := f.VerifyCommand()
	binPath := resolveCommand(cmdName)
	if binPath == "" {
		return fmt.Errorf("在系统 PATH 中未找到 %s", cmdName)
	}

	binDir := filepath.Dir(binPath)
	sdkRoot := pathmgr.DetectSdkRoot(binDir, sdkTypeStr)

	var versionName string
	if ver, err := a.detectVersionFromDir(sdkRoot, f); err == nil && ver != "" {
		versionName = ver
	} else {
		dirName := filepath.Base(sdkRoot)
		versionName = pathmgr.ExtractVersion(dirName)
		if versionName == "" || versionName == "." || versionName == dirName {
			versionName = "imported"
		}
	}

	targetDir := a.cfg.SdkVersionDir(sdkTypeStr, versionName)
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("清理目标目录失败: %w", err)
	}

	if err := pathmgr.CopyDir(sdkRoot, targetDir); err != nil {
		return fmt.Errorf("复制SDK失败: %w", err)
	}

	relBinDir := ""
	if isDir(filepath.Join(targetDir, "bin")) {
		relBinDir = "bin"
	}

	if err := a.pathMgr.ConfigureSdk(sdkTypeStr, targetDir, relBinDir, f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("配置PATH失败: %w", err)
	}

	a.pathMgr.CleanExternalPaths(sdkTypeStr, versionName, sdkRoot)

	return a.cfg.SetActiveVersion(sdkTypeStr, versionName)
}
