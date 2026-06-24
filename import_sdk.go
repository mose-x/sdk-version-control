package main

import (
	"fmt"
	"os"
	"path/filepath"

	"sdk_version_control/internal/extractor"
	"sdk_version_control/internal/logger"
	"sdk_version_control/internal/pathmgr"
	"sdk_version_control/internal/sdk"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) SelectLocalFile() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app not initialized")
	}
	return wailsRuntime.OpenFileDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Select Archive File",
		Filters: []wailsRuntime.FileFilter{
			{DisplayName: "Archive", Pattern: "*.zip;*.tar.gz;*.tgz;*.tar.xz;*.7z"},
			{DisplayName: "All Files", Pattern: "*.*"},
		},
	})
}

func (a *App) SelectLocalDir() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app not initialized")
	}
	return wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Select SDK Directory",
	})
}

func (a *App) ImportLocalSdk(sdkTypeStr string, localPath string) error {
	if err := validatePathSegment(sdkTypeStr); err != nil {
		return err
	}
	sdkType := sdk.SdkType(sdkTypeStr)
	f := a.registry.Get(sdkType)
	if f == nil {
		return fmt.Errorf("unknown SDK type: %s", sdkTypeStr)
	}

	logger.Info("Importing local SDK: %s from %s", sdkTypeStr, localPath)

	info, err := os.Stat(localPath)
	if err != nil {
		logger.Error("Path does not exist: %s", localPath)
		return fmt.Errorf("path does not exist: %s", localPath)
	}

	var sourceDir string

	if info.IsDir() {
		sourceDir = pathmgr.DetectSdkRoot(localPath, sdkTypeStr)
	} else {
		tmpDir := filepath.Join(a.cfg.TmpDir(), "import_"+filepath.Base(localPath))
		if err := os.RemoveAll(tmpDir); err != nil {
			return fmt.Errorf("failed to clean temp directory: %w", err)
		}
		if err := os.MkdirAll(tmpDir, 0755); err != nil {
			return fmt.Errorf("failed to create temp directory: %w", err)
		}
		defer os.RemoveAll(tmpDir)

		ext, err := extractor.NewExtractor(filepath.Base(localPath))
		if err != nil {
			return fmt.Errorf("unsupported archive format: %w", err)
		}
		if err := ext.Extract(localPath, tmpDir); err != nil {
			return fmt.Errorf("extraction failed: %w", err)
		}
		if err := extractor.StripTopDir(tmpDir); err != nil {
			return fmt.Errorf("extraction failed: %w", err)
		}
		sourceDir = pathmgr.DetectSdkRoot(tmpDir, sdkTypeStr)
	}

	var versionName string
	if ver, err := a.detectVersionFromDir(sourceDir, f); err == nil && ver != "" {
		versionName = ver
	} else {
		if err != nil {
			logger.Warn("Failed to run verify command to get version (%s): %v", sdkTypeStr, err)
		}
		dirName := filepath.Base(sourceDir)
		versionName = pathmgr.ExtractVersion(dirName)
		if versionName == "" || versionName == "." || versionName == dirName {
			versionName = "imported"
		}
	}

	targetDir := a.cfg.SdkVersionDir(sdkTypeStr, versionName)
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("failed to clean target directory: %w", err)
	}

	if err := pathmgr.CopyDir(sourceDir, targetDir); err != nil {
		return fmt.Errorf("failed to copy SDK: %w", err)
	}

	binDir := ""
	if _, err := os.Stat(filepath.Join(targetDir, "bin")); err == nil {
		binDir = "bin"
	}

	if err := a.pathMgr.ConfigureSdk(sdkTypeStr, targetDir, binDir, nil); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	a.pathMgr.CleanExternalPaths(sdkTypeStr, versionName, sourceDir)

	logger.Info("Successfully imported local SDK: %s %s", sdkTypeStr, versionName)
	return a.cfg.SetActiveVersion(sdkTypeStr, versionName)
}

func (a *App) ImportSdk(externalPath string, sdkType string) error {
	if err := validatePathSegment(sdkType); err != nil {
		return err
	}
	logger.Info("Importing SDK: %s from %s", sdkType, externalPath)
	sdkRoot := pathmgr.DetectSdkRoot(externalPath, sdkType)

	dirName := filepath.Base(sdkRoot)
	versionName := pathmgr.ExtractVersion(dirName)
	if versionName == "" || versionName == "." {
		versionName = "imported"
	}

	targetDir := a.cfg.SdkVersionDir(sdkType, versionName)

	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("failed to clean target directory: %w", err)
	}

	if err := pathmgr.CopyDir(sdkRoot, targetDir); err != nil {
		return fmt.Errorf("failed to copy SDK: %w", err)
	}

	binDir := ""
	if _, err := os.Stat(filepath.Join(targetDir, "bin")); err == nil {
		binDir = "bin"
	}

	if err := a.pathMgr.ConfigureSdk(sdkType, targetDir, binDir, nil); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	a.pathMgr.CleanExternalPaths(sdkType, versionName, sdkRoot)

	logger.Info("Successfully imported SDK: %s %s", sdkType, versionName)
	return a.cfg.SetActiveVersion(sdkType, versionName)
}

func (a *App) ImportPathSdk(sdkTypeStr string) error {
	if err := validatePathSegment(sdkTypeStr); err != nil {
		return err
	}
	logger.Info("Importing SDK from system PATH: %s", sdkTypeStr)
	sdkType := sdk.SdkType(sdkTypeStr)
	f := a.registry.Get(sdkType)
	if f == nil {
		return fmt.Errorf("unknown SDK type: %s", sdkTypeStr)
	}

	cmdName, _ := f.VerifyCommand()
	binPath := resolveCommand(cmdName)
	if binPath == "" {
		return fmt.Errorf("%s not found in system PATH", cmdName)
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
		return fmt.Errorf("failed to clean target directory: %w", err)
	}

	if err := pathmgr.CopyDir(sdkRoot, targetDir); err != nil {
		return fmt.Errorf("failed to copy SDK: %w", err)
	}

	relBinDir := ""
	if isDir(filepath.Join(targetDir, "bin")) {
		relBinDir = "bin"
	}

	if err := a.pathMgr.ConfigureSdk(sdkTypeStr, targetDir, relBinDir, f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	a.pathMgr.CleanExternalPaths(sdkTypeStr, versionName, sdkRoot)

	logger.Info("Successfully imported SDK from PATH: %s %s", sdkTypeStr, versionName)
	return a.cfg.SetActiveVersion(sdkTypeStr, versionName)
}
