package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"sdk_version_control/internal/downloader"
	"sdk_version_control/internal/extractor"
	"sdk_version_control/internal/logger"
	"sdk_version_control/internal/sdk"
)

func (a *App) GetAllSdkStatus() []sdk.SdkStatus {
	if a.registry == nil {
		return nil
	}
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
	if a.registry == nil {
		return nil, fmt.Errorf("application not fully initialized")
	}
	if err := validatePathSegment(sdkType); err != nil {
		return nil, err
	}
	f := a.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return nil, fmt.Errorf("unknown SDK type: %s", sdkType)
	}
	return f.GetLocalStatus()
}

func (a *App) CheckSystemConflicts(sdkType string) ([]string, error) {
	if a.registry == nil {
		return nil, fmt.Errorf("application not fully initialized")
	}
	if err := validatePathSegment(sdkType); err != nil {
		return nil, err
	}
	f := a.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return nil, fmt.Errorf("unknown SDK type: %s", sdkType)
	}

	var keys []string
	for k := range f.GetExtraEnvVars() {
		keys = append(keys, k)
	}

	return a.pathMgr.DetectSystemConflicts(sdkType, keys), nil
}

func (a *App) GetRemoteVersions(sdkType string) ([]sdk.VersionInfo, error) {
	if a.registry == nil {
		return nil, fmt.Errorf("application not fully initialized")
	}
	if err := validatePathSegment(sdkType); err != nil {
		return nil, err
	}
	f := a.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return nil, fmt.Errorf("unknown SDK type: %s", sdkType)
	}
	proxyCfg := a.getProxyConfig()
	client := downloader.BuildClient(proxyCfg)
	client.Timeout = 30 * time.Second
	f.SetHTTPClient(client)
	return f.FetchRemoteVersions()
}

func (a *App) InstallSdk(sdkTypeStr string, version string) error {
	if a.registry == nil {
		return fmt.Errorf("application not fully initialized")
	}
	if err := validatePathSegment(sdkTypeStr); err != nil {
		return err
	}
	if err := validatePathSegment(version); err != nil {
		return err
	}
	sdkType := sdk.SdkType(sdkTypeStr)
	f := a.registry.Get(sdkType)
	if f == nil {
		return fmt.Errorf("unknown SDK type: %s", sdkTypeStr)
	}

	logger.Info("Starting installation: %s %s", sdkTypeStr, version)

	// Inject the proxy-aware client BEFORE resolving the download URL: 8 SDKs
	// (jdk/go/gradle/python/ruby/php/perl/android) issue HTTP API calls inside
	// GetDownloadURL, and python/ruby/php/perl/android even re-run
	// FetchRemoteVersions (multi-page GitHub API). Without this injection they
	// used the bare constructor client and bypassed the user's proxy, so
	// resolving the URL failed in proxy-only networks even though the actual
	// file download (below) correctly used the proxy.
	proxyCfg := a.getProxyConfig()
	client := downloader.BuildClient(proxyCfg)
	client.Timeout = 30 * time.Second
	f.SetHTTPClient(client)

	downloadURL, fileName, err := f.GetDownloadURL(version)
	if err != nil {
		return fmt.Errorf("failed to get download URL: %w", err)
	}
	downloadURL = a.applyGithubMirror(downloadURL)

	tmpFile := filepath.Join(a.cfg.TmpDir(), fileName)
	logger.Info("Download URL: %s", downloadURL)
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
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmpFile)

	logger.Info("Download completed, extracting...")
	a.emitProgress(sdkType, version, "extracting", 0, "Extracting...", 0, 0, 0, downloadURL)
	versionDir := a.cfg.SdkVersionDir(string(sdkType), version)
	if err := os.RemoveAll(versionDir); err != nil {
		return fmt.Errorf("failed to clean old version directory: %w", err)
	}
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	ext, err := extractor.NewExtractor(fileName)
	if err != nil {
		return fmt.Errorf("unsupported archive format: %w", err)
	}
	if err := ext.Extract(tmpFile, versionDir); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}
	// Strip the archive's top-level directory only when the fetcher opts in.
	// SDKs whose GetBinDirs() already includes the top-level dir name (e.g.
	// Go "go/bin", Dart "dart-sdk/bin", Android "cmdline-tools/bin", Perl
	// "perl/bin"+"c/bin") set StripArchiveTopDir()=false so the top dir is
	// preserved and their bin paths resolve correctly.
	if f.StripArchiveTopDir() {
		if err := extractor.StripTopDir(versionDir); err != nil {
			return fmt.Errorf("extraction failed: %w", err)
		}
	}

	logger.Info("Extraction completed, configuring environment...")
	a.emitProgress(sdkType, version, "configuring_path", 0, "Configuring environment...", 0, 0, 0, downloadURL)
	if err := a.pathMgr.ConfigureSdk(string(sdkType), versionDir, f.GetBinDirs(), f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	if err := a.cfg.SetActiveVersion(string(sdkType), version); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Refresh .svc.rc so env var lines (JAVA_HOME, GOROOT, etc.) reflect the
	// newly-active version. ConfigureSdk ran updateRcFile before SetActiveVersion,
	// so its env vars pointed at the previous active version; this refresh fixes
	// that. Non-fatal: the shim runtime reads config.json directly, so a stale
	// .svc.rc only affects tools that source it directly.
	if err := a.shimMgr.RefreshRcFile(); err != nil {
		logger.Warn("Failed to refresh .svc.rc after install: %v", err)
	}

	logger.Info("Installation complete: %s %s", sdkTypeStr, version)
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
	if a.registry == nil {
		return fmt.Errorf("application not fully initialized")
	}
	if err := validatePathSegment(sdkTypeStr); err != nil {
		return err
	}
	if err := validatePathSegment(version); err != nil {
		return err
	}
	sdkType := sdk.SdkType(sdkTypeStr)
	f := a.registry.Get(sdkType)
	if f == nil {
		return fmt.Errorf("unknown SDK type: %s", sdkTypeStr)
	}

	logger.Info("Switching %s version to: %s", sdkTypeStr, version)

	versionDir := a.cfg.SdkVersionDir(sdkTypeStr, version)
	if _, err := os.Stat(versionDir); err != nil {
		logger.Error("Version directory does not exist: %s", versionDir)
		return fmt.Errorf("version directory does not exist: %s", version)
	}

	if err := a.pathMgr.ConfigureSdk(sdkTypeStr, versionDir, f.GetBinDirs(), f.GetExtraEnvVars()); err != nil {
		logger.Error("Failed to configure PATH for %s %s: %v", sdkTypeStr, version, err)
		return fmt.Errorf("failed to configure PATH: %w", err)
	}

	if err := a.cfg.SetActiveVersion(sdkTypeStr, version); err != nil {
		logger.Error("Failed to save config for %s %s: %v", sdkTypeStr, version, err)
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Refresh .svc.rc so env var lines reflect the newly-switched version.
	if err := a.shimMgr.RefreshRcFile(); err != nil {
		logger.Warn("Failed to refresh .svc.rc after switch: %v", err)
	}

	logger.Info("Successfully switched %s to version %s", sdkTypeStr, version)
	return nil
}

func (a *App) GetSdkDownloadURL(sdkType string, version string) (string, error) {
	if a.registry == nil {
		return "", fmt.Errorf("application not fully initialized")
	}
	if err := validatePathSegment(sdkType); err != nil {
		return "", err
	}
	if err := validatePathSegment(version); err != nil {
		return "", err
	}
	f := a.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return "", fmt.Errorf("unknown SDK type: %s", sdkType)
	}
	// Same proxy injection as InstallSdk: GetDownloadURL may issue HTTP API
	// calls that must honor the user's proxy configuration.
	proxyCfg := a.getProxyConfig()
	client := downloader.BuildClient(proxyCfg)
	client.Timeout = 30 * time.Second
	f.SetHTTPClient(client)
	url, _, err := f.GetDownloadURL(version)
	if err != nil {
		return "", err
	}
	return a.applyGithubMirror(url), nil
}

func (a *App) DetectPathVersion(sdkType string) string {
	if a.registry == nil {
		return ""
	}
	f := a.registry.Get(sdk.SdkType(sdkType))
	if f == nil {
		return ""
	}
	cmd, args := f.VerifyCommand()
	return extractVersionFromOutput(cmd, args)
}
