package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"sdk_version_control/internal/sdk"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type AppInfo struct {
	Version   string `json:"version"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	License   string `json:"license"`
	RepoURL   string `json:"repoUrl"`
	UpdateURL string `json:"updateUrl"`
}

func (a *App) loadAboutInfo() {
	if err := json.Unmarshal(aboutJSON, &a.appInfo); err != nil {
		a.appInfo = AppInfo{
			Version:   "0.1.0",
			BuildDate: "2026-06-20",
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

type VersionJSON struct {
	Version   string `json:"version"`
	Changelog string `json:"changelog"`
	Downloads map[string]struct {
		URL      string `json:"url"`
		Filename string `json:"filename"`
	} `json:"downloads"`
}

type UpdateInfo struct {
	HasUpdate     bool   `json:"hasUpdate"`
	LatestVersion string `json:"latestVersion"`
	Changelog     string `json:"changelog"`
	DownloadURL   string `json:"downloadUrl"`
	Filename      string `json:"filename"`
}

func (a *App) CheckUpdate() (UpdateInfo, error) {
	if a.appInfo.UpdateURL == "" {
		return UpdateInfo{}, fmt.Errorf("update URL is not configured")
	}

	client := &http.Client{Transport: a.buildProxyTransport(), Timeout: 15 * time.Second}

	resp, err := client.Get(a.appInfo.UpdateURL)
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return UpdateInfo{}, fmt.Errorf("server returned error status: %d", resp.StatusCode)
	}

	var remote VersionJSON
	if err := json.NewDecoder(resp.Body).Decode(&remote); err != nil {
		return UpdateInfo{}, fmt.Errorf("failed to parse version info: %w", err)
	}

	hasUpdate := sdk.CompareVersions(remote.Version, a.appInfo.Version) > 0

	platformKey := runtime.GOOS + "-" + runtime.GOARCH
	dlURL := ""
	filename := ""
	if dl, ok := remote.Downloads[platformKey]; ok {
		dlURL = dl.URL
		filename = dl.Filename
	}

	return UpdateInfo{
		HasUpdate:     hasUpdate,
		LatestVersion: remote.Version,
		Changelog:     remote.Changelog,
		DownloadURL:   dlURL,
		Filename:      filename,
	}, nil
}

type UpdateProgress struct {
	Stage            string `json:"stage"`
	Percent          int    `json:"percent"`
	DownloadedBytes  int64  `json:"downloadedBytes"`
	TotalBytes       int64  `json:"totalBytes"`
	SpeedBytesPerSec int64  `json:"speedBytesPerSec"`
	Message          string `json:"message"`
}

func (a *App) DownloadUpdate(downloadURL string) error {
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

	a.emitUpdateProgress(UpdateProgress{
		Stage:   "done",
		Percent: 100,
		Message: "Download complete",
	})

	return nil
}

func (a *App) emitUpdateProgress(p UpdateProgress) {
	wailsRuntime.EventsEmit(a.ctx, "update:progress", p)
}
