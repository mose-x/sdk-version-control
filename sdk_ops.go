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

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
)

// verifyFileSHA256 computes the SHA256 hash of a file and compares it to the
// expected hex-encoded checksum. Returns nil if they match, an error otherwise.
func verifyFileSHA256(filePath, expectedSHA256 string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file for checksum: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("failed to compute checksum: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expectedSHA256) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSHA256, actual)
	}
	return nil
}

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
	t := sdk.SdkType(sdkType)
	f := a.registry.Get(t)
	if f == nil {
		return nil, fmt.Errorf("unknown SDK type: %s", sdkType)
	}

	// Cache-first: return the cached list immediately (memory → disk) so the UI
	// gets a usable list without waiting for a network round-trip. A background
	// goroutine then refreshes the cache from the remote and emits
	// "install:versions-refreshed" so the UI can silently swap in the fresh list.
	//
	// On a cache MISS (first run, never fetched before) we fetch synchronously
	// so the user sees a real list on the first open instead of an empty panel
	// that only fills in seconds later via the event.
	if cached, ok := sdk.GetCachedVersions(t); ok {
		a.refreshVersionsInBackground(t, f)
		return cached, nil
	}

	versions, err := a.fetchAndCacheVersions(t, f)
	if err != nil {
		return nil, err
	}
	return versions, nil
}

// fetchAndCacheVersions fetches the remote version list through f, stores it in
// the version cache (memory + disk), and returns it. Used both by the
// synchronous cache-miss path and by the background refresh goroutine.
func (a *App) fetchAndCacheVersions(t sdk.SdkType, f sdk.VersionFetcher) ([]sdk.VersionInfo, error) {
	proxyCfg := a.getProxyConfig()
	client := downloader.BuildClient(proxyCfg)
	client.Timeout = 30 * time.Second
	f.SetHTTPClient(client)
	versions, err := f.FetchRemoteVersions()
	if err != nil {
		return nil, err
	}
	sdk.SetCachedVersions(t, versions)
	return versions, nil
}

// refreshVersionsInBackground fetches a fresh version list off the UI thread and
// emits "install:versions-refreshed" with {sdkType, versions} when the fresh
// list differs from the cached one. Errors are logged but not surfaced: this is
// a silent refresh, and the UI already has the cached list to show.
//
// Throttled per-SDK via ShouldRefreshVersions (30s cooldown): rapidly switching
// SDK panels triggers GetRemoteVersions on each switch, and without throttling
// every cache hit would spawn a goroutine + emit an event, flooding the UI.
//
// The goroutine is fire-and-forget; it is not cancelled on app shutdown
// (Wails tears down the runtime, so a late EventsEmit is a no-op).
func (a *App) refreshVersionsInBackground(t sdk.SdkType, f sdk.VersionFetcher) {
	if !sdk.ShouldRefreshVersions(t) {
		return
	}
	go func() {
		fresh, err := a.fetchAndCacheVersions(t, f)
		if err != nil {
			logger.Warn("Background version refresh failed for %s: %v", t, err)
			return
		}
		// Emit even when the list is identical to the cached one: the UI uses
		// the event as a "refresh done" signal too (e.g. to clear a stale
		// loading state from a manual refresh click). Cheap and idempotent.
		wailsRuntime.EventsEmit(a.ctx, "install:versions-refreshed", map[string]any{
			"sdkType":  string(t),
			"versions": fresh,
		})
	}()
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
	if err := validatePathSegment(fileName); err != nil {
		return fmt.Errorf("invalid download filename: %w", err)
	}
	downloadURL = a.applyGithubMirror(downloadURL)

	tmpFile := filepath.Join(a.cfg.TmpDir(), fileName)
	logger.Info("Download URL: %s", downloadURL)
	a.emitProgress(sdkType, version, "downloading", 0, "Downloading...", 0, 0, 0, downloadURL)

	defer os.Remove(tmpFile)

	installCtx, cancel := context.WithCancel(a.ctx)
	a.cancelMu.Lock()
	if old, ok := a.cancelFuncs[sdkTypeStr]; ok {
		old.cancel()
	}
	myID := a.nextCancelID
	a.nextCancelID++
	a.cancelFuncs[sdkTypeStr] = cancelEntry{cancel: cancel, id: myID}
	a.cancelMu.Unlock()
	defer func() {
		cancel()
		a.cancelMu.Lock()
		if entry, ok := a.cancelFuncs[sdkTypeStr]; ok && entry.id == myID {
			delete(a.cancelFuncs, sdkTypeStr)
		}
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

	// Verify download integrity via SHA256 if the fetcher supports it (M1).
	if cf, ok := f.(sdk.ChecksumFetcher); ok {
		expected, err := cf.FetchChecksum(version)
		if err != nil {
			logger.Warn("Failed to fetch checksum for %s %s: %v (skipping verification)", sdkTypeStr, version, err)
		} else if expected != "" {
			a.emitProgress(sdkType, version, "verifying", 0, "Verifying checksum...", 0, 0, 0, downloadURL)
			if err := verifyFileSHA256(tmpFile, expected); err != nil {
				a.emitProgress(sdkType, version, "error", 0, fmt.Sprintf("Checksum verification failed: %v", err), 0, 0, 0, downloadURL)
				return fmt.Errorf("checksum verification failed: %w", err)
			}
			logger.Info("Checksum verified for %s %s", sdkTypeStr, version)
		} else {
			logger.Warn("No checksum available for %s %s, skipping verification", sdkTypeStr, version)
		}
	}

	logger.Info("Download completed, extracting...")
	a.emitProgress(sdkType, version, "extracting", 0, "Extracting...", 0, 0, 0, downloadURL)
	versionDir := a.cfg.SdkVersionDir(string(sdkType), version)

	// Extract to a temporary sibling directory first, then atomically replace
	// the old version on success. This preserves the previously-working version
	// if extraction fails (M4: data loss on extraction failure).
	tmpVersionDir := versionDir + ".new"
	os.RemoveAll(tmpVersionDir)
	if err := os.MkdirAll(tmpVersionDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	ext, err := extractor.NewExtractor(fileName)
	if err != nil {
		os.RemoveAll(tmpVersionDir)
		return fmt.Errorf("unsupported archive format: %w", err)
	}
	if err := ext.Extract(tmpFile, tmpVersionDir); err != nil {
		os.RemoveAll(tmpVersionDir)
		return fmt.Errorf("extraction failed: %w", err)
	}
	// Strip the archive's top-level directory only when the fetcher opts in.
	// SDKs whose GetBinDirs() already includes the top-level dir name (e.g.
	// Go "go/bin", Dart "dart-sdk/bin", Android "cmdline-tools/bin", Perl
	// "perl/bin"+"c/bin") set StripArchiveTopDir()=false so the top dir is
	// preserved and their bin paths resolve correctly.
	if f.StripArchiveTopDir() {
		if err := extractor.StripTopDir(tmpVersionDir); err != nil {
			os.RemoveAll(tmpVersionDir)
			return fmt.Errorf("extraction failed: %w", err)
		}
	}

	// Rust-specific: merge rust-std component's lib/rustlib into cargo/ and
	// rustc/ so that sysroot resolution finds the std library.
	if rustFetcher, ok := f.(*sdk.RustFetcher); ok {
		if err := rustFetcher.MergeComponents(tmpVersionDir); err != nil {
			os.RemoveAll(tmpVersionDir)
			return fmt.Errorf("failed to merge Rust components: %w", err)
		}
	}

	// Atomically replace the old version directory using rename-old-first
	// pattern: rename old to .old, rename new into place, cleanup .old.
	// If the second rename fails, restore from .old (prevents data loss).
	oldVersionDir := versionDir + ".old"
	os.RemoveAll(oldVersionDir)
	if _, err := os.Stat(versionDir); err == nil {
		if renameErr := os.Rename(versionDir, oldVersionDir); renameErr != nil {
			os.RemoveAll(versionDir)
		}
	}
	if err := os.Rename(tmpVersionDir, versionDir); err != nil {
		if _, statErr := os.Stat(oldVersionDir); statErr == nil {
			os.Rename(oldVersionDir, versionDir)
		}
		os.RemoveAll(tmpVersionDir)
		return fmt.Errorf("failed to move extracted files into place: %w", err)
	}
	os.RemoveAll(oldVersionDir)

	logger.Info("Extraction completed, configuring environment...")
	a.emitProgress(sdkType, version, "configuring_path", 0, "Configuring environment...", 0, 0, 0, downloadURL)

	// M13: Set active version BEFORE ConfigureSdk. If SetActiveVersion fails,
	// the config doesn't record this version — don't configure PATH for an
	// unrecorded version (would leave inconsistent state on failure).
	if err := a.cfg.SetActiveVersion(string(sdkType), version); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := a.pathMgr.ConfigureSdk(string(sdkType), versionDir, f.GetBinDirs(), f.GetExtraEnvVars()); err != nil {
		return fmt.Errorf("failed to configure PATH: %w", err)
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
	if entry, ok := a.cancelFuncs[sdkType]; ok {
		entry.cancel()
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
