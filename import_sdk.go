package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"sdk_version_control/internal/extractor"
	"sdk_version_control/internal/helpers"
	"sdk_version_control/internal/logger"
	"sdk_version_control/internal/pathmgr"
	"sdk_version_control/internal/sdk"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// copyToTargetAtomically copies sourceDir to a temp sibling of targetDir,
// aligns the import layout, runs the layer-2 critical-files check and the
// verify callback on the ALIGNED temp dir, then atomically replaces
// targetDir. Uses rename-old-first pattern: the old targetDir is renamed to
// .old BEFORE the new tmpDir is renamed into place, so if the second rename
// fails, the old version is restored from .old.
//
// The critical-files check runs AFTER AlignImportLayout (see
// alignAndCheckCriticalFiles): flat imports (directory / PATH / flat archive)
// only gain their expected wrapper dir (go/, dart-sdk/, ...) during alignment,
// so checking the pre-alignment layout would reject complete SDKs.
func copyToTargetAtomically(sourceDir, targetDir string, binDirs []string, sdkType sdk.SdkType, verify func(string) error) error {
	tmpDir := targetDir + ".new"
	oldDir := targetDir + ".old"
	os.RemoveAll(tmpDir)
	os.RemoveAll(oldDir)
	if err := pathmgr.CopyDir(sourceDir, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to copy SDK: %w", err)
	}
	if err := alignAndCheckCriticalFiles(tmpDir, binDirs, sdkType); err != nil {
		os.RemoveAll(tmpDir)
		return err
	}
	if verify != nil {
		if err := verify(tmpDir); err != nil {
			os.RemoveAll(tmpDir)
			return err
		}
	}
	// Rename old targetDir to .old (if it exists). This preserves the old
	// version so it can be restored if the next Rename fails.
	if _, err := os.Stat(targetDir); err == nil {
		if renameErr := os.Rename(targetDir, oldDir); renameErr != nil {
			// H1: Never delete the live version as fallback. Abort so the
			// existing directory is preserved instead of losing data.
			os.RemoveAll(tmpDir)
			return fmt.Errorf("failed to backup existing directory for atomic replace: %w", renameErr)
		}
	}
	// Rename tmpDir into place. If this fails, restore from .old.
	if err := os.Rename(tmpDir, targetDir); err != nil {
		if _, statErr := os.Stat(oldDir); statErr == nil {
			os.Rename(oldDir, targetDir)
		}
		os.RemoveAll(tmpDir)
		return fmt.Errorf("failed to move files into place: %w", err)
	}
	// Success — clean up .old.
	os.RemoveAll(oldDir)
	return nil
}

// alignAndCheckCriticalFiles aligns the import layout of dir (wrapping a flat
// layout into the fetcher's expected top-level dir when needed) and only THEN
// runs the layer-2 critical-files check on the aligned result.
//
// The check MUST run after alignment: directory / PATH imports of Go, Dart,
// Android, Perl (Windows) and Python (all platforms) — and the JDK macOS PATH
// import — arrive flat (bin/... with no go/ dart-sdk/ python/ ... wrapper),
// so checking the pre-alignment layout wrongly rejects complete SDKs as
// "SDK incomplete". Archive imports whose layout already carries the wrapper
// are unaffected: AlignImportLayout is a no-op for them.
func alignAndCheckCriticalFiles(dir string, binDirs []string, t sdk.SdkType) error {
	if err := pathmgr.AlignImportLayout(dir, binDirs); err != nil {
		logger.Warn("Failed to align import layout: %v", err)
	}
	return checkCriticalFiles(dir, t)
}

// criticalFilesFor returns the relative paths (from SDK root) of files that
// must exist for the SDK to be considered complete. Used by the import flow's
// layer-2 integrity check, which runs AFTER AlignImportLayout (see
// alignAndCheckCriticalFiles) so flat imports are judged on their aligned
// layout. The paths therefore match the post-alignment / download-install
// layout (e.g. "go/bin/go" with the wrapper dir present).
func criticalFilesFor(t sdk.SdkType) []string {
	switch t {
	case sdk.Golang:
		if runtime.GOOS == "windows" {
			return []string{"go/bin/go.exe", "go/bin/gofmt.exe"}
		}
		return []string{"go/bin/go", "go/bin/gofmt"}
	case sdk.NodeJS:
		if runtime.GOOS == "windows" {
			return []string{"node.exe", "npm.cmd"}
		}
		return []string{"bin/node", "bin/npm"}
	case sdk.JDK:
		if runtime.GOOS == "darwin" {
			return []string{"Contents/Home/bin/java", "Contents/Home/bin/javac"}
		}
		if runtime.GOOS == "windows" {
			return []string{"bin/java.exe", "bin/javac.exe"}
		}
		return []string{"bin/java", "bin/javac"}
	case sdk.Python:
		if runtime.GOOS == "windows" {
			return []string{"python/python.exe"}
		}
		return []string{"python/bin/python3"}
	case sdk.Rust:
		if runtime.GOOS == "windows" {
			return []string{"cargo/bin/cargo.exe", "rustc/bin/rustc.exe"}
		}
		return []string{"cargo/bin/cargo", "rustc/bin/rustc"}
	case sdk.Ruby:
		if runtime.GOOS == "windows" {
			return []string{"bin/ruby.exe"}
		}
		return []string{"bin/ruby"}
	case sdk.DotNet:
		if runtime.GOOS == "windows" {
			return []string{"dotnet.exe"}
		}
		return []string{"dotnet"}
	case sdk.PHP:
		if runtime.GOOS == "windows" {
			return []string{"php.exe"}
		}
		return []string{"php"}
	case sdk.Perl:
		if runtime.GOOS == "windows" {
			return []string{"perl/bin/perl.exe"}
		}
		return []string{"bin/perl"}
	case sdk.Maven:
		if runtime.GOOS == "windows" {
			return []string{"bin/mvn.cmd"}
		}
		return []string{"bin/mvn"}
	case sdk.Gradle:
		if runtime.GOOS == "windows" {
			return []string{"bin/gradle.bat"}
		}
		return []string{"bin/gradle"}
	case sdk.Flutter:
		if runtime.GOOS == "windows" {
			return []string{"bin/flutter.bat"}
		}
		return []string{"bin/flutter"}
	case sdk.Android:
		if runtime.GOOS == "windows" {
			return []string{"cmdline-tools/bin/sdkmanager.bat"}
		}
		return []string{"cmdline-tools/bin/sdkmanager"}
	case sdk.Dart:
		if runtime.GOOS == "windows" {
			return []string{"dart-sdk/bin/dart.exe"}
		}
		return []string{"dart-sdk/bin/dart"}
	default:
		return nil
	}
}

// checkCriticalFiles verifies that the critical files for the SDK type exist
// in sdkRoot. Returns an error listing the first missing file.
func checkCriticalFiles(sdkRoot string, t sdk.SdkType) error {
	for _, file := range criticalFilesFor(t) {
		if _, err := os.Stat(filepath.Join(sdkRoot, file)); err != nil {
			return fmt.Errorf("SDK incomplete, missing %s", file)
		}
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
	if err := helpers.ValidatePathSegment(sdkTypeStr); err != nil {
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

	// Layer 1: Pre-check — run the verify binary to confirm it's usable.
	versionName, err := a.detectVersionFromDir(sourceDir, f)
	if err != nil {
		return fmt.Errorf("SDK binary verification failed, cannot import: %w", err)
	}

	// Layer 2 (critical files) runs INSIDE copyToTargetAtomically, AFTER the
	// layout is aligned: checking the pre-alignment (possibly flat) sourceDir
	// wrongly rejected complete SDKs as "SDK incomplete".
	targetDir := a.cfg.SdkVersionDir(sdkTypeStr, versionName)
	binDirs := f.GetBinDirs()
	verifyImport := func(dir string) error {
		postVer, err := a.detectVersionFromDir(dir, f)
		if err != nil {
			return fmt.Errorf("post-import verification failed: %w", err)
		}
		if postVer != versionName {
			return fmt.Errorf("post-import version mismatch: expected %s, got %s", versionName, postVer)
		}
		return nil
	}
	if err := copyToTargetAtomically(sourceDir, targetDir, binDirs, sdkType, verifyImport); err != nil {
		return err
	}

	// H1: Set active version BEFORE ConfigureSdk (matching InstallSdk M13 fix).
	if err := a.cfg.SetActiveVersion(sdkTypeStr, versionName); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := a.pathMgr.ConfigureSdk(sdkTypeStr, targetDir, binDirs, f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	a.pathMgr.CleanExternalPaths(sdkTypeStr, versionName, sourceDir)

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
	if err := helpers.ValidatePathSegment(sdkType); err != nil {
		return err
	}
	f := a.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return fmt.Errorf("unknown SDK type: %s", sdkType)
	}
	logger.Info("Importing SDK: %s from %s", sdkType, externalPath)
	sdkRoot := pathmgr.DetectSdkRoot(externalPath, sdkType)

	// Layer 1: Pre-check — run the verify binary to confirm it's usable.
	versionName, err := a.detectVersionFromDir(sdkRoot, f)
	if err != nil {
		return fmt.Errorf("SDK binary verification failed, cannot import: %w", err)
	}

	// Layer 2 (critical files) runs INSIDE copyToTargetAtomically, AFTER the
	// layout is aligned: checking the pre-alignment (possibly flat) sdkRoot
	// wrongly rejected complete SDKs as "SDK incomplete".
	targetDir := a.cfg.SdkVersionDir(sdkType, versionName)
	binDirs := f.GetBinDirs()
	verifyImport := func(dir string) error {
		postVer, err := a.detectVersionFromDir(dir, f)
		if err != nil {
			return fmt.Errorf("post-import verification failed: %w", err)
		}
		if postVer != versionName {
			return fmt.Errorf("post-import version mismatch: expected %s, got %s", versionName, postVer)
		}
		return nil
	}
	if err := copyToTargetAtomically(sdkRoot, targetDir, binDirs, sdk.SdkType(sdkType), verifyImport); err != nil {
		return err
	}

	// H1: Set active version BEFORE ConfigureSdk (matching InstallSdk M13 fix).
	if err := a.cfg.SetActiveVersion(sdkType, versionName); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := a.pathMgr.ConfigureSdk(sdkType, targetDir, binDirs, f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	a.pathMgr.CleanExternalPaths(sdkType, versionName, sdkRoot)

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
	if err := helpers.ValidatePathSegment(sdkTypeStr); err != nil {
		return err
	}
	logger.Info("Importing SDK from system PATH: %s", sdkTypeStr)
	sdkType := sdk.SdkType(sdkTypeStr)
	f := a.registry.Get(sdkType)
	if f == nil {
		return fmt.Errorf("unknown SDK type: %s", sdkTypeStr)
	}

	cmdName, _ := f.VerifyCommand()
	binPath := helpers.ResolveCommand(cmdName)
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

	// Layer 1: Pre-check — run the verify binary to confirm it's usable.
	versionName, err := a.detectVersionFromDir(sdkRoot, f)
	if err != nil {
		return fmt.Errorf("SDK binary verification failed, cannot import: %w", err)
	}

	// Layer 2 (critical files) runs INSIDE copyToTargetAtomically, AFTER the
	// layout is aligned: checking the pre-alignment (possibly flat) sdkRoot
	// wrongly rejected complete SDKs as "SDK incomplete" (e.g. PATH imports
	// of Python on all platforms, Perl on Windows, JDK on macOS).
	targetDir := a.cfg.SdkVersionDir(sdkTypeStr, versionName)
	binDirs := f.GetBinDirs()
	verifyImport := func(dir string) error {
		postVer, err := a.detectVersionFromDir(dir, f)
		if err != nil {
			return fmt.Errorf("post-import verification failed: %w", err)
		}
		if postVer != versionName {
			return fmt.Errorf("post-import version mismatch: expected %s, got %s", versionName, postVer)
		}
		return nil
	}
	if err := copyToTargetAtomically(sdkRoot, targetDir, binDirs, sdkType, verifyImport); err != nil {
		return err
	}

	// H1: Set active version BEFORE ConfigureSdk (matching InstallSdk M13 fix).
	if err := a.cfg.SetActiveVersion(sdkTypeStr, versionName); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := a.pathMgr.ConfigureSdk(sdkTypeStr, targetDir, binDirs, f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	a.pathMgr.CleanExternalPaths(sdkTypeStr, versionName, sdkRoot)

	if err := a.shimMgr.RefreshRcFile(); err != nil {
		logger.Warn("Failed to refresh .svc.rc after import: %v", err)
	}

	logger.Info("Successfully imported SDK from PATH: %s %s", sdkTypeStr, versionName)
	return nil
}
