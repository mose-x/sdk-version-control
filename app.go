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
	"sdk_version_control/internal/logmgr"
	"sdk_version_control/internal/pathmgr"
	"sdk_version_control/internal/proxy"
	"sdk_version_control/internal/sdk"
	"sdk_version_control/internal/settings"
	"sdk_version_control/internal/shimmanager"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed about.json
var aboutJSON []byte

// cancelEntry pairs a cancel func with a monotonic install ID so the deferred
// cleanup in InstallSdk only deletes the map entry when it still belongs to
// THIS install (not a newer concurrent install of the same SDK type).
//
// done is closed by the owning install's deferred cleanup when it has FULLY
// exited. A newer InstallSdk for the same SDK cancels the old entry and then
// waits (bounded) on done: without that wait the old download goroutine could
// still be writing the shared tmp download file (or its deferred cleanup
// could delete it) after the new install has re-created the same file.
type cancelEntry struct {
	cancel context.CancelFunc
	id     uint64
	done   chan struct{}
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
	proxySvc     *proxy.Service
	settingsSvc  *settings.Service
	appInfo      AppInfo
	cancelMu     sync.Mutex
	cancelFuncs  map[string]cancelEntry
	nextCancelID uint64

	// fetcherMu guards fetcherLocks (NOT the fetchers themselves). The
	// registry hands out shared fetcher singletons whose HTTP client is a
	// bare field set via SetHTTPClient; the per-SDK mutexes returned by
	// fetcherLock serialize "SetHTTPClient + the calls that must observe it"
	// (FetchRemoteVersions / GetDownloadURL / FetchChecksum) so a background
	// version refresh and an install of the same SDK cannot race on it.
	fetcherMu    sync.Mutex
	fetcherLocks map[string]*sync.Mutex
}

// fetcherLock returns the per-SDK mutex used to serialize SetHTTPClient and
// the HTTP calls that depend on it. Lazily initialized so tests can use a
// bare &App{}.
func (a *App) fetcherLock(key string) *sync.Mutex {
	a.fetcherMu.Lock()
	defer a.fetcherMu.Unlock()
	if a.fetcherLocks == nil {
		a.fetcherLocks = make(map[string]*sync.Mutex)
	}
	m, ok := a.fetcherLocks[key]
	if !ok {
		m = &sync.Mutex{}
		a.fetcherLocks[key] = m
	}
	return m
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

	sm := config.NewSettingsManager(cfg.HomeDir())
	app := &App{
		cfg:         cfg,
		settings:    sm,
		proxySvc:    proxy.New(sm),
		settingsSvc: settings.New(sm),
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

// CheckProxy verifies that the configured proxy can reach the given URL.
func (a *App) CheckProxy(targetURL string) error {
	return a.proxySvc.CheckProxy(targetURL)
}

// GetSettings returns the settings with the GitHub token masked.
func (a *App) GetSettings() config.AppSettings {
	return a.settingsSvc.Get()
}

// SaveSettings persists a settings snapshot (owned fields are preserved).
func (a *App) SaveSettings(s config.AppSettings) error {
	return a.settingsSvc.Save(s)
}

// SaveGithubToken stores (or clears, when empty) the GitHub PAT.
func (a *App) SaveGithubToken(token string) error {
	return a.settingsSvc.SaveGithubToken(token)
}

// GetDefaultEndpoints lists the built-in endpoint presets.
func (a *App) GetDefaultEndpoints() []sdk.EndpointInfo {
	return a.settingsSvc.GetDefaultEndpoints()
}

// GetEndpoints returns the custom endpoint overrides.
func (a *App) GetEndpoints() map[string]string {
	return a.settingsSvc.GetEndpoints()
}

// SaveEndpoints replaces the custom endpoint overrides.
func (a *App) SaveEndpoints(endpoints map[string]string) error {
	return a.settingsSvc.SaveEndpoints(endpoints)
}

// GetLogFiles lists the application log files.
func (a *App) GetLogFiles() ([]logger.LogFileInfo, error) {
	return logmgr.GetLogFiles()
}

// GetLogContent returns the content of one log file.
func (a *App) GetLogContent(filename string) (string, error) {
	return logmgr.GetLogContent(filename)
}

// CleanLogs removes all log files.
func (a *App) CleanLogs() error {
	return logmgr.CleanLogs()
}

// DeleteLogFile removes a single log file.
func (a *App) DeleteLogFile(filename string) error {
	return logmgr.DeleteLogFile(filename)
}

// GetLogDir returns the active log directory.
func (a *App) GetLogDir() string {
	return logmgr.GetLogDir()
}
