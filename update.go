package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"sdk_version_control/internal/sdk"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type AppInfo struct {
	Version   string `json:"version"`
	GoVersion string `json:"goVersion"`
	License   string `json:"license"`
	RepoURL   string `json:"repoUrl"`
	UpdateURL string `json:"updateUrl"`
}

func (a *App) loadAboutInfo() {
	if err := json.Unmarshal(aboutJSON, &a.appInfo); err != nil {
		a.appInfo = AppInfo{
			Version:   "0.1.0",
			GoVersion: "1.25",
			License:   "MIT License",
			RepoURL:   "https://github.com/example/sdk-version-control",
			UpdateURL: "",
		}
	}
}

func (a *App) GetAppInfo() AppInfo {
	return a.appInfo
}

// GitHubRelease models the relevant fields of the GitHub Releases API
// response (GET /repos/{owner}/{repo}/releases/latest). The updater reads
// tag_name for the version, body for the changelog, and assets[] for
// per-platform download URLs — no version.json manifest needed.
type GitHubRelease struct {
	TagName string        `json:"tag_name"`
	Body    string        `json:"body"`
	Assets  []GitHubAsset `json:"assets"`
}

type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type UpdateInfo struct {
	HasUpdate     bool   `json:"hasUpdate"`
	LatestVersion string `json:"latestVersion"`
	Changelog     string `json:"changelog"`
	DownloadURL   string `json:"downloadUrl"`
	Filename      string `json:"filename"`
	Sha256        string `json:"sha256"`
}

func (a *App) CheckUpdate() (UpdateInfo, error) {
	if a.appInfo.UpdateURL == "" {
		return UpdateInfo{}, fmt.Errorf("update URL is not configured")
	}

	client := &http.Client{Transport: a.buildProxyTransport(), Timeout: 15 * time.Second}

	// updateUrl points at the GitHub Releases API
	// (https://api.github.com/repos/<owner>/<repo>/releases/latest). GitHub
	// requires a User-Agent and the JSON media type.
	req, err := http.NewRequest(http.MethodGet, a.appInfo.UpdateURL, nil)
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "SDKVersionControl")

	resp, err := client.Do(req)
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return UpdateInfo{}, fmt.Errorf("server returned error status: %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return UpdateInfo{}, fmt.Errorf("failed to parse release info: %w", err)
	}

	remoteVersion := strings.TrimPrefix(release.TagName, "v")
	hasUpdate := sdk.CompareVersions(remoteVersion, a.appInfo.Version) > 0

	if !hasUpdate {
		return UpdateInfo{HasUpdate: false, LatestVersion: remoteVersion}, nil
	}

	// Match the asset for the current platform from the release's asset list.
	asset, ok := matchPlatformAsset(release.Assets)
	// If the release has no asset for the current platform, report no update
	// rather than HasUpdate=true with an empty download URL, which would leave
	// the user stuck on "new version available" with no way to fetch it.
	if !ok {
		return UpdateInfo{HasUpdate: false, LatestVersion: remoteVersion}, nil
	}

	// Resolve the expected sha256 from sha256sums.txt (also a release asset).
	// Verification is skipped if sha256sums.txt is absent (lenient fallback).
	sha := a.fetchAssetSha256(client, release.Assets, asset.Name)

	return UpdateInfo{
		HasUpdate:     true,
		LatestVersion: remoteVersion,
		Changelog:     release.Body,
		DownloadURL:   asset.BrowserDownloadURL,
		Filename:      asset.Name,
		Sha256:        sha,
	}, nil
}

// matchPlatformAsset picks the release asset for the current OS/arch.
// Asset names follow the build convention SDKVersionControl-<ver>-<os>-<arch><ext>:
//
//	windows-x64.exe / windows-arm64.exe
//	macos-x64.bin   / macos-arm64.bin   (bare binary for in-place self-update, NOT .dmg)
//	linux-x64       / linux-arm64       (bare binary for in-place self-update, NOT .deb/.rpm)
//
// runtime.GOOS is windows/darwin/linux, but asset names use "macos" for darwin;
// runtime.GOARCH is amd64/arm64, but asset names use "x64" for amd64.
func matchPlatformAsset(assets []GitHubAsset) (GitHubAsset, bool) {
	osToken := map[string]string{
		"windows": "windows",
		"darwin":  "macos",
		"linux":   "linux",
	}[runtime.GOOS]
	archToken := map[string]string{
		"amd64": "x64",
		"arm64": "arm64",
	}[runtime.GOARCH]
	if osToken == "" || archToken == "" {
		return GitHubAsset{}, false
	}
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if !strings.Contains(name, osToken) || !strings.Contains(name, archToken) {
			continue
		}
		// macOS self-update uses the bare .bin, not the .dmg installer.
		if runtime.GOOS == "darwin" && strings.HasSuffix(name, ".dmg") {
			continue
		}
		// Windows ships both a bare .exe (for self-update) and an NSIS
		// -setup.exe installer (for first-time install). Self-update must
		// pick the bare exe — the installer can't be swapped in place and
		// running it would need admin/UAC.
		if runtime.GOOS == "windows" && strings.Contains(name, "-setup.") {
			continue
		}
		// Linux ships both a bare binary (for self-update) and .deb/.rpm
		// packages (for first-time install via dpkg/rpm). Self-update must
		// pick the bare binary — package managers can't be swapped in place
		// and would need root + package-manager state.
		if runtime.GOOS == "linux" && (strings.HasSuffix(name, ".deb") || strings.HasSuffix(name, ".rpm")) {
			continue
		}
		return a, true
	}
	return GitHubAsset{}, false
}

// fetchAssetSha256 downloads the sha256sums.txt release asset (if present) and
// returns the hash recorded for filename. Empty string if the manifest is
// missing or the file isn't listed — DownloadUpdate then skips verification
// (lenient fallback for older releases without a checksum manifest).
func (a *App) fetchAssetSha256(client *http.Client, assets []GitHubAsset, filename string) string {
	var sumsURL string
	for _, a := range assets {
		if a.Name == "sha256sums.txt" {
			sumsURL = a.BrowserDownloadURL
			break
		}
	}
	if sumsURL == "" {
		return ""
	}
	resp, err := client.Get(sumsURL)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	// Lines: "<64-hex-hash>  <filename>"
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == filename {
			return fields[0]
		}
	}
	return ""
}

type UpdateProgress struct {
	Stage            string `json:"stage"`
	Percent          int    `json:"percent"`
	DownloadedBytes  int64  `json:"downloadedBytes"`
	TotalBytes       int64  `json:"totalBytes"`
	SpeedBytesPerSec int64  `json:"speedBytesPerSec"`
	Message          string `json:"message"`
}

// DownloadUpdate fetches the new binary to a temp path, then (if expectedSha256
// is non-empty) verifies the SHA256 before reporting success. On mismatch the
// downloaded file is deleted so ApplyUpdate cannot pick up a corrupt payload.
func (a *App) DownloadUpdate(downloadURL, expectedSha256 string) error {
	if downloadURL == "" {
		return fmt.Errorf("download URL is empty")
	}
	downloadURL = a.applyGithubMirror(downloadURL)

	tmpPath := getUpdateFilePath()
	os.Remove(tmpPath)

	proxyCfg := a.getProxyConfig()
	threads := a.settings.Get().DownloadThreads
	if threads <= 0 {
		threads = 4
	}

	err := a.downloader.Download(a.ctx, downloadURL, tmpPath, func(downloaded, total, speed int64) {
		percent := 0
		if total > 0 {
			percent = int(downloaded * 100 / total)
		}
		msg := "Downloading..."
		if total > 0 {
			msg = fmt.Sprintf("Downloading %.1fMB / %.1fMB", float64(downloaded)/(1024*1024), float64(total)/(1024*1024))
		}
		a.emitUpdateProgress(UpdateProgress{
			Stage:            "downloading",
			Percent:          percent,
			DownloadedBytes:  downloaded,
			TotalBytes:       total,
			SpeedBytesPerSec: speed,
			Message:          msg,
		})
	}, proxyCfg, threads)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Verify integrity if the server published a SHA256 for this asset.
	// Older releases without the field skip verification (lenient fallback).
	if expectedSha256 != "" {
		a.emitUpdateProgress(UpdateProgress{
			Stage:   "verifying",
			Percent: 100,
			Message: "Verifying integrity...",
		})
		actual, err := sha256OfFile(tmpPath)
		if err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to hash downloaded file: %w", err)
		}
		if actual != expectedSha256 {
			os.Remove(tmpPath)
			return fmt.Errorf("integrity check failed: expected %s, got %s", expectedSha256, actual)
		}
	}

	a.emitUpdateProgress(UpdateProgress{
		Stage:   "done",
		Percent: 100,
		Message: "Download complete",
	})

	return nil
}

// sha256OfFile streams the file through a SHA256 hasher and returns the hex digest.
func sha256OfFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (a *App) emitUpdateProgress(p UpdateProgress) {
	wailsRuntime.EventsEmit(a.ctx, "update:progress", p)
}
