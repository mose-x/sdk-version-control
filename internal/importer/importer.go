package importer

import (
	"fmt"
	"os"
	"path/filepath"

	"sdk_version_control/internal/config"
	"sdk_version_control/internal/extractor"
	"sdk_version_control/internal/helpers"
	"sdk_version_control/internal/logger"
	"sdk_version_control/internal/pathmgr"
	"sdk_version_control/internal/sdk"
	"sdk_version_control/internal/shimmanager"
	"sdk_version_control/internal/wailsrt"
)

// Service implements the SDK import flows: local archive/directory import,
// external-directory import, and system-PATH import. All three share the
// 3-layer integrity verification (pre-check, critical files after layout
// alignment, post-check) and the atomic copy/replace helper.
type Service struct {
	cfg      *config.Config
	registry *sdk.Registry
	pathMgr  pathmgr.PathManager
	shimMgr  *shimmanager.Manager
	rt       wailsrt.Runtime
}

// New wires an import Service. rt may be nil in tests that never open
// dialogs (SelectLocalFile/SelectLocalDir guard on it).
func New(cfg *config.Config, registry *sdk.Registry, pathMgr pathmgr.PathManager, shimMgr *shimmanager.Manager, rt wailsrt.Runtime) *Service {
	return &Service{cfg: cfg, registry: registry, pathMgr: pathMgr, shimMgr: shimMgr, rt: rt}
}

func (s *Service) SelectLocalFile() (string, error) {
	if s.rt == nil {
		return "", fmt.Errorf("app not initialized")
	}
	return s.rt.OpenFileDialog("Select Archive File", []wailsrt.FileFilter{
		{DisplayName: "Archive", Pattern: "*.zip;*.tar.gz;*.tgz;*.tar.xz;*.7z"},
		{DisplayName: "All Files", Pattern: "*.*"},
	})
}

func (s *Service) SelectLocalDir() (string, error) {
	if s.rt == nil {
		return "", fmt.Errorf("app not initialized")
	}
	return s.rt.OpenDirectoryDialog("Select SDK Directory")
}

func (s *Service) ImportLocalSdk(sdkTypeStr string, localPath string) error {
	if s.registry == nil {
		return fmt.Errorf("application not fully initialized")
	}
	if err := helpers.ValidatePathSegment(sdkTypeStr); err != nil {
		return err
	}
	sdkType := sdk.SdkType(sdkTypeStr)
	f := s.registry.Get(sdkType)
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
		tmpDir := filepath.Join(s.cfg.TmpDir(), "import_"+filepath.Base(localPath))
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
	versionName, err := s.detectVersionFromDir(sourceDir, f)
	if err != nil {
		return fmt.Errorf("SDK binary verification failed, cannot import: %w", err)
	}

	// Layer 2 (critical files) runs INSIDE copyToTargetAtomically, AFTER the
	// layout is aligned: checking the pre-alignment (possibly flat) sourceDir
	// wrongly rejected complete SDKs as "SDK incomplete".
	targetDir := s.cfg.SdkVersionDir(sdkTypeStr, versionName)
	binDirs := f.GetBinDirs()
	verifyImport := func(dir string) error {
		postVer, err := s.detectVersionFromDir(dir, f)
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
	if err := s.cfg.SetActiveVersion(sdkTypeStr, versionName); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := s.pathMgr.ConfigureSdk(sdkTypeStr, targetDir, binDirs, f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	s.pathMgr.CleanExternalPaths(sdkTypeStr, versionName, sourceDir)

	if err := s.shimMgr.RefreshRcFile(); err != nil {
		logger.Warn("Failed to refresh .svc.rc after import: %v", err)
	}

	logger.Info("Successfully imported local SDK: %s %s", sdkTypeStr, versionName)
	return nil
}

func (s *Service) ImportSdk(externalPath string, sdkType string) error {
	if s.registry == nil {
		return fmt.Errorf("application not fully initialized")
	}
	if err := helpers.ValidatePathSegment(sdkType); err != nil {
		return err
	}
	f := s.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return fmt.Errorf("unknown SDK type: %s", sdkType)
	}
	logger.Info("Importing SDK: %s from %s", sdkType, externalPath)
	sdkRoot := pathmgr.DetectSdkRoot(externalPath, sdkType)

	// Layer 1: Pre-check — run the verify binary to confirm it's usable.
	versionName, err := s.detectVersionFromDir(sdkRoot, f)
	if err != nil {
		return fmt.Errorf("SDK binary verification failed, cannot import: %w", err)
	}

	// Layer 2 (critical files) runs INSIDE copyToTargetAtomically, AFTER the
	// layout is aligned: checking the pre-alignment (possibly flat) sdkRoot
	// wrongly rejected complete SDKs as "SDK incomplete".
	targetDir := s.cfg.SdkVersionDir(sdkType, versionName)
	binDirs := f.GetBinDirs()
	verifyImport := func(dir string) error {
		postVer, err := s.detectVersionFromDir(dir, f)
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
	if err := s.cfg.SetActiveVersion(sdkType, versionName); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := s.pathMgr.ConfigureSdk(sdkType, targetDir, binDirs, f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	s.pathMgr.CleanExternalPaths(sdkType, versionName, sdkRoot)

	if err := s.shimMgr.RefreshRcFile(); err != nil {
		logger.Warn("Failed to refresh .svc.rc after import: %v", err)
	}

	logger.Info("Successfully imported SDK: %s %s", sdkType, versionName)
	return nil
}

func (s *Service) ImportPathSdk(sdkTypeStr string) error {
	if s.registry == nil {
		return fmt.Errorf("application not fully initialized")
	}
	if err := helpers.ValidatePathSegment(sdkTypeStr); err != nil {
		return err
	}
	logger.Info("Importing SDK from system PATH: %s", sdkTypeStr)
	sdkType := sdk.SdkType(sdkTypeStr)
	f := s.registry.Get(sdkType)
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
	versionName, err := s.detectVersionFromDir(sdkRoot, f)
	if err != nil {
		return fmt.Errorf("SDK binary verification failed, cannot import: %w", err)
	}

	// Layer 2 (critical files) runs INSIDE copyToTargetAtomically, AFTER the
	// layout is aligned: checking the pre-alignment (possibly flat) sdkRoot
	// wrongly rejected complete SDKs as "SDK incomplete" (e.g. PATH imports
	// of Python on all platforms, Perl on Windows, JDK on macOS).
	targetDir := s.cfg.SdkVersionDir(sdkTypeStr, versionName)
	binDirs := f.GetBinDirs()
	verifyImport := func(dir string) error {
		postVer, err := s.detectVersionFromDir(dir, f)
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
	if err := s.cfg.SetActiveVersion(sdkTypeStr, versionName); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := s.pathMgr.ConfigureSdk(sdkTypeStr, targetDir, binDirs, f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	s.pathMgr.CleanExternalPaths(sdkTypeStr, versionName, sdkRoot)

	if err := s.shimMgr.RefreshRcFile(); err != nil {
		logger.Warn("Failed to refresh .svc.rc after import: %v", err)
	}

	logger.Info("Successfully imported SDK from PATH: %s %s", sdkTypeStr, versionName)
	return nil
}
