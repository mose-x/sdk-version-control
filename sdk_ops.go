package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"sdk_version_control/internal/downloader"
	"sdk_version_control/internal/extractor"
	"sdk_version_control/internal/sdk"
)

func (a *App) GetAllSdkStatus() []sdk.SdkStatus {
	var statuses []sdk.SdkStatus
	for _, f := range a.registry.All() {
		status, err := f.GetLocalStatus()
		if err != nil {
			statuses = append(statuses, sdk.SdkStatus{
				SdkType:     f.Type(),
				DisplayName: sdk.SdkDisplayName(f.Type()),
				Configured:  false,
			})
			continue
		}
		if status.PathConfigured && status.PathVersion == "" {
			cmd, args := f.VerifyCommand()
			status.PathVersion = extractVersionFromOutput(cmd, args)
		}
		statuses = append(statuses, *status)
	}
	return statuses
}

func (a *App) GetSdkStatus(sdkType string) (*sdk.SdkStatus, error) {
	if err := validatePathSegment(sdkType); err != nil {
		return nil, err
	}
	f := a.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return nil, fmt.Errorf("未知的SDK类型: %s", sdkType)
	}
	return f.GetLocalStatus()
}

func (a *App) CheckSystemConflicts(sdkType string) ([]string, error) {
	if err := validatePathSegment(sdkType); err != nil {
		return nil, err
	}
	f := a.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return nil, fmt.Errorf("未知的SDK类型: %s", sdkType)
	}

	var keys []string
	for k := range f.GetExtraEnvVars() {
		keys = append(keys, k)
	}

	return a.pathMgr.DetectSystemConflicts(sdkType, keys), nil
}

func (a *App) GetRemoteVersions(sdkType string) ([]sdk.VersionInfo, error) {
	if err := validatePathSegment(sdkType); err != nil {
		return nil, err
	}
	f := a.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return nil, fmt.Errorf("未知的SDK类型: %s", sdkType)
	}
	proxyCfg := a.getProxyConfig()
	client := downloader.BuildClient(proxyCfg)
	client.Timeout = 30 * time.Second
	f.SetHTTPClient(client)
	return f.FetchRemoteVersions()
}

func (a *App) InstallSdk(sdkTypeStr string, version string) error {
	if err := validatePathSegment(sdkTypeStr); err != nil {
		return err
	}
	if err := validatePathSegment(version); err != nil {
		return err
	}
	sdkType := sdk.SdkType(sdkTypeStr)
	f := a.registry.Get(sdkType)
	if f == nil {
		return fmt.Errorf("未知的SDK类型: %s", sdkTypeStr)
	}

	downloadURL, fileName, err := f.GetDownloadURL(version)
	if err != nil {
		return fmt.Errorf("获取下载链接失败: %w", err)
	}
	downloadURL = a.applyGithubMirror(downloadURL)

	proxyCfg := a.getProxyConfig()

	tmpFile := filepath.Join(a.cfg.TmpDir(), fileName)
	a.emitProgress(sdkType, version, "downloading", 0, "Downloading...", 0, 0, 0, downloadURL)

	installCtx, cancel := context.WithCancel(a.ctx)
	a.cancelMu.Lock()
	if old, ok := a.cancelFuncs[sdkTypeStr]; ok {
		old()
	}
	a.cancelFuncs[sdkTypeStr] = cancel
	a.cancelMu.Unlock()
	defer func() {
		cancel()
		a.cancelMu.Lock()
		delete(a.cancelFuncs, sdkTypeStr)
		a.cancelMu.Unlock()
	}()

	threads := a.settings.Get().DownloadThreads
	if threads <= 0 {
		threads = 4
	}
	err = a.downloader.Download(installCtx, downloadURL, tmpFile, func(downloaded, total, speed int64) {
		if total > 0 {
			percent := int(downloaded * 100 / total)
			msg := fmt.Sprintf("Downloading... %d%%", percent)
			a.emitProgress(sdkType, version, "downloading", percent, msg, downloaded, total, speed, downloadURL)
		} else {
			a.emitProgress(sdkType, version, "downloading", 0, "Downloading...", downloaded, 0, speed, downloadURL)
		}
	}, proxyCfg, threads)
	if err != nil {
		a.emitProgress(sdkType, version, "error", 0, fmt.Sprintf("Download failed: %v", err), 0, 0, 0, downloadURL)
		return fmt.Errorf("下载失败: %w", err)
	}
	defer os.Remove(tmpFile)

	a.emitProgress(sdkType, version, "extracting", 0, "Extracting...", 0, 0, 0, downloadURL)
	versionDir := a.cfg.SdkVersionDir(string(sdkType), version)
	if err := os.RemoveAll(versionDir); err != nil {
		return fmt.Errorf("清理旧版本目录失败: %w", err)
	}
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	ext, err := extractor.NewExtractor(fileName)
	if err != nil {
		return fmt.Errorf("不支持的压缩格式: %w", err)
	}
	if err := ext.Extract(tmpFile, versionDir); err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}
	if err := extractor.StripTopDir(versionDir); err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}

	a.emitProgress(sdkType, version, "configuring_path", 0, "Configuring environment...", 0, 0, 0, downloadURL)
	if err := a.pathMgr.ConfigureSdk(string(sdkType), versionDir, f.GetBinDir(), f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("配置PATH失败: %w", err)
	}

	if err := a.cfg.SetActiveVersion(string(sdkType), version); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	a.emitProgress(sdkType, version, "done", 100, "Installation complete!", 0, 0, 0, downloadURL)
	return nil
}

func (a *App) CancelInstall(sdkType string) {
	a.cancelMu.Lock()
	if cancel, ok := a.cancelFuncs[sdkType]; ok {
		cancel()
		delete(a.cancelFuncs, sdkType)
	}
	a.cancelMu.Unlock()
}

func (a *App) GetInstallDir(sdkType string) string {
	if err := validatePathSegment(sdkType); err != nil {
		return ""
	}
	return a.cfg.SdkDir(sdkType)
}

func (a *App) SwitchVersion(sdkTypeStr string, version string) error {
	if err := validatePathSegment(sdkTypeStr); err != nil {
		return err
	}
	if err := validatePathSegment(version); err != nil {
		return err
	}
	sdkType := sdk.SdkType(sdkTypeStr)
	f := a.registry.Get(sdkType)
	if f == nil {
		return fmt.Errorf("未知的SDK类型: %s", sdkTypeStr)
	}

	versionDir := a.cfg.SdkVersionDir(sdkTypeStr, version)
	if _, err := os.Stat(versionDir); err != nil {
		return fmt.Errorf("版本目录不存在: %s", version)
	}

	if err := a.pathMgr.ConfigureSdk(sdkTypeStr, versionDir, f.GetBinDir(), f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("配置PATH失败: %w", err)
	}

	if err := a.cfg.SetActiveVersion(sdkTypeStr, version); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	return nil
}

func (a *App) GetSdkDownloadURL(sdkType string, version string) (string, error) {
	if err := validatePathSegment(sdkType); err != nil {
		return "", err
	}
	if err := validatePathSegment(version); err != nil {
		return "", err
	}
	f := a.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return "", fmt.Errorf("未知的SDK类型: %s", sdkType)
	}
	url, _, err := f.GetDownloadURL(version)
	if err != nil {
		return "", err
	}
	return a.applyGithubMirror(url), nil
}

func (a *App) DetectPathVersion(sdkType string) string {
	f := a.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return ""
	}
	cmd, args := f.VerifyCommand()
	return extractVersionFromOutput(cmd, args)
}
