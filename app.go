package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"sdk_version_control/internal/config"
	"sdk_version_control/internal/downloader"
	"sdk_version_control/internal/logger"
	"sdk_version_control/internal/pathmgr"
	"sdk_version_control/internal/sdk"
	"sdk_version_control/internal/shimmanager"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed about.json
var aboutJSON []byte

// cancelEntry pairs a cancel func with a monotonic install ID so the deferred
// cleanup in InstallSdk only deletes the map entry when it still belongs to
// THIS install (not a newer concurrent install of the same SDK type).
type cancelEntry struct {
	cancel context.CancelFunc
	id     uint64
}

// App struct - Wails bound core structure
type App struct {
	ctx          context.Context
	cfg          *config.Config
	registry     *sdk.Registry
	downloader   *downloader.Downloader
	pathMgr      pathmgr.PathManager
	shimMgr      *shimmanager.Manager
	settings     *config.SettingsManager
	appInfo      AppInfo
	cancelMu     sync.Mutex
	cancelFuncs  map[string]cancelEntry
	nextCancelID uint64
}

// NewApp creates an App instance
func NewApp() *App {
	cfg, err := config.NewConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize config: %v\n", err)
		os.Exit(1)
	}

	logger.Init(cfg.SvcDir())
	logger.Info("Application starting...")

	shimMgr := shimmanager.New(cfg)
	pathMgr := pathmgr.NewPathManager(cfg)
	pathMgr.SetShimManager(shimMgr)

	app := &App{
		cfg:         cfg,
		settings:    config.NewSettingsManager(cfg.HomeDir()),
		pathMgr:     pathMgr,
		shimMgr:     shimMgr,
		downloader:  downloader.NewDownloader(),
		cancelFuncs: make(map[string]cancelEntry),
	}
	app.loadAboutInfo()
	return app
}

// startup called on application launch
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	s := a.settings.Get()
	if s.InstallPath != "" {
		logger.Info("Using custom install path: %s", s.InstallPath)
		a.cfg.SetSvcDir(s.InstallPath)
	}
	a.registry = sdk.NewRegistry(a.cfg, a.settings)
	logger.Info("SDK registry initialized with %d SDK types", len(a.registry.All()))

	// Seed ~/.svc/mirrors.json (the editable "easter egg" GitHub mirror list)
	// and point the version cache at ~/.svc/cache. Both must be (re)initialised
	// after any SetSvcDir override above so the files land in the active SVC dir.
	sdk.InitMirrorsFile(a.cfg.SvcDir())
	sdk.InitVersionCacheDir(filepath.Join(a.cfg.SvcDir(), "cache"))

	// One-time shims setup: creates ~/.svc/shims, installs the shim binary,
	// and adds the single .svc.rc source line (Unix) / shims PATH entry (Windows).
	// This is the only place SVC ever touches the shell rc or registry PATH.
	if err := a.shimMgr.EnsureSetup(); err != nil {
		logger.Warn("Shim setup failed (run 'svc init' to retry): %v", err)
	}

	// Clean up temp directory
	if entries, err := os.ReadDir(a.cfg.TmpDir()); err == nil && len(entries) > 0 {
		logger.Info("Cleaning up %d temporary files from previous run", len(entries))
		for _, e := range entries {
			os.RemoveAll(filepath.Join(a.cfg.TmpDir(), e.Name()))
		}
	}

	logger.Info("Application startup complete")
}

// shutdown called on application exit
func (a *App) shutdown(ctx context.Context) {
	logger.Info("Application shutting down...")
	a.cancelMu.Lock()
	for sdkType, entry := range a.cancelFuncs {
		logger.Info("Cancelling ongoing install: %s", sdkType)
		entry.cancel()
	}
	a.cancelMu.Unlock()
	logger.Info("Application shutdown complete")
}

func (a *App) emitProgress(sdkType sdk.SdkType, version, stage string, percent int, message string, downloadedBytes, totalBytes, speedBytesPerSec int64, downloadURL string) {
	wailsRuntime.EventsEmit(a.ctx, "install:progress", sdk.InstallProgress{
		SdkType:          sdkType,
		Version:          version,
		Stage:            stage,
		Percent:          percent,
		Message:          message,
		DownloadedBytes:  downloadedBytes,
		TotalBytes:       totalBytes,
		SpeedBytesPerSec: speedBytesPerSec,
		DownloadURL:      downloadURL,
	})
}

// GetPathEntries retrieves all PATH entries
func (a *App) GetPathEntries() ([]pathmgr.PathEntry, error) {
	return a.pathMgr.GetAllPathEntries()
}
