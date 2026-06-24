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

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed about.json
var aboutJSON []byte

// App struct - Wails bound core structure
type App struct {
	ctx         context.Context
	cfg         *config.Config
	registry    *sdk.Registry
	downloader  *downloader.Downloader
	pathMgr     pathmgr.PathManager
	settings    *config.SettingsManager
	appInfo     AppInfo
	cancelMu    sync.Mutex
	cancelFuncs map[string]context.CancelFunc
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

	app := &App{
		cfg:         cfg,
		settings:    config.NewSettingsManager(cfg.SvcDir()),
		pathMgr:     pathmgr.NewPathManager(cfg),
		downloader:  downloader.NewDownloader(),
		cancelFuncs: make(map[string]context.CancelFunc),
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
	for sdkType, cancel := range a.cancelFuncs {
		logger.Info("Cancelling ongoing install: %s", sdkType)
		cancel()
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
