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

// copyToTargetAtomically copies sourceDir to a temp sibling of targetDir,
// aligns the import layout, then atomically replaces targetDir via Rename.
// On failure (copy or align error), the old targetDir is preserved.
func copyToTargetAtomically(sourceDir, targetDir string, binDirs []string) error {
	tmpDir := targetDir + ".new"
	os.RemoveAll(tmpDir)
	if err := pathmgr.CopyDir(sourceDir, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to copy SDK: %w", err)
	}
	if err := pathmgr.AlignImportLayout(tmpDir, binDirs); err != nil {
		logger.Warn("Failed to align import layout: %v", err)
	}
	if err := os.RemoveAll(targetDir); err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to clean target directory: %w", err)
	}
	if err := os.Rename(tmpDir, targetDir); err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to move files into place: %w", err)
	}
	return nil
}

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
	if a.registry == nil {
		return fmt.Errorf("application not fully initialized")
	}
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
		// Honor the fetcher's StripArchiveTopDir() flag — same logic as
		// InstallSdk.  SDKs whose GetBinDirs() includes the top-level dir
		// name (Go, Dart, Android, Perl) must NOT strip, otherwise their
		// bin paths break.
		if f.StripArchiveTopDir() {
			if err := extractor.StripTopDir(tmpDir); err != nil {
				return fmt.Errorf("extraction failed: %w", err)
			}
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
	binDirs := f.GetBinDirs()
	if err := copyToTargetAtomically(sourceDir, targetDir, binDirs); err != nil {
		return err
	}

	if err := a.pathMgr.ConfigureSdk(sdkTypeStr, targetDir, binDirs, f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	a.pathMgr.CleanExternalPaths(sdkTypeStr, versionName, sourceDir)

	if err := a.cfg.SetActiveVersion(sdkTypeStr, versionName); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	if err := a.shimMgr.RefreshRcFile(); err != nil {
		logger.Warn("Failed to refresh .svc.rc after import: %v", err)
	}

	logger.Info("Successfully imported local SDK: %s %s", sdkTypeStr, versionName)
	return nil
}

func (a *App) ImportSdk(externalPath string, sdkType string) error {
	if a.registry == nil {
		return fmt.Errorf("application not fully initialized")
	}
	if err := validatePathSegment(sdkType); err != nil {
		return err
	}
	f := a.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return fmt.Errorf("unknown SDK type: %s", sdkType)
	}
	logger.Info("Importing SDK: %s from %s", sdkType, externalPath)
	sdkRoot := pathmgr.DetectSdkRoot(externalPath, sdkType)

	dirName := filepath.Base(sdkRoot)
	versionName := pathmgr.ExtractVersion(dirName)
	if versionName == "" || versionName == "." {
		versionName = "imported"
	}

	targetDir := a.cfg.SdkVersionDir(sdkType, versionName)
	binDirs := f.GetBinDirs()
	if err := copyToTargetAtomically(sdkRoot, targetDir, binDirs); err != nil {
		return err
	}

	if err := a.pathMgr.ConfigureSdk(sdkType, targetDir, binDirs, f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	a.pathMgr.CleanExternalPaths(sdkType, versionName, sdkRoot)

	if err := a.cfg.SetActiveVersion(sdkType, versionName); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	if err := a.shimMgr.RefreshRcFile(); err != nil {
		logger.Warn("Failed to refresh .svc.rc after import: %v", err)
	}

	logger.Info("Successfully imported SDK: %s %s", sdkType, versionName)
	return nil
}

func (a *App) ImportPathSdk(sdkTypeStr string) error {
	if a.registry == nil {
		return fmt.Errorf("application not fully initialized")
	}
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

	// Python on macOS/Linux lives at /usr/bin/python3 (system-managed) and
	// Windows Store ships a python.exe stub in WindowsApps. Importing either
	// would CopyDir an OS directory (/usr, C:\Windows, ...) into the app's
	// store. Refuse here as a backstop even though the UI already hides the
	// import button for system-protected copies -- guards against direct API
	// calls and races where the status was computed before this guard landed.
	if sdk.IsSystemPythonPath(binPath) {
		return fmt.Errorf("system %s is at %s (a protected OS path) and cannot be imported; please install %s via the app instead, the app-managed copy will take precedence via PATH priority",
			cmdName, binPath, f.Type())
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
	binDirs := f.GetBinDirs()
	if err := copyToTargetAtomically(sdkRoot, targetDir, binDirs); err != nil {
		return err
	}

	if err := a.pathMgr.ConfigureSdk(sdkTypeStr, targetDir, binDirs, f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	a.pathMgr.CleanExternalPaths(sdkTypeStr, versionName, sdkRoot)

	if err := a.cfg.SetActiveVersion(sdkTypeStr, versionName); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	if err := a.shimMgr.RefreshRcFile(); err != nil {
		logger.Warn("Failed to refresh .svc.rc after import: %v", err)
	}

	logger.Info("Successfully imported SDK from PATH: %s %s", sdkTypeStr, versionName)
	return nil
}
