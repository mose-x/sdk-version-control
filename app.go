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
	"sdk_version_control/internal/pathmgr"
	"sdk_version_control/internal/sdk"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed about.json
var aboutJSON []byte

// App struct - Wails 绑定的核心结构
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

// NewApp 创建 App 实例
func NewApp() *App {
	cfg, err := config.NewConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化配置失败: %v\n", err)
		os.Exit(1)
	}

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

// startup 应用启动时调用
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	s := a.settings.Get()
	if s.InstallPath != "" {
		a.cfg.SetSvcDir(s.InstallPath)
	}
	a.registry = sdk.NewRegistry(a.cfg, a.settings)

	if entries, err := os.ReadDir(a.cfg.TmpDir()); err == nil {
		for _, e := range entries {
			os.RemoveAll(filepath.Join(a.cfg.TmpDir(), e.Name()))
		}
	}
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

// GetPathEntries 获取所有 PATH 条目
func (a *App) GetPathEntries() ([]pathmgr.PathEntry, error) {
	return a.pathMgr.GetAllPathEntries()
}
