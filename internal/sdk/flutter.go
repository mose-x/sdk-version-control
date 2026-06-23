package sdk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"sdk_version_control/internal/config"
)

type FlutterFetcher struct {
	cfg        *config.Config
	sm         *config.SettingsManager
	httpClient *http.Client
}

func NewFlutterFetcher(cfg *config.Config, sm *config.SettingsManager) *FlutterFetcher {
	return &FlutterFetcher{cfg: cfg, sm: sm, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (f *FlutterFetcher) SetHTTPClient(client *http.Client) { f.httpClient = client }
func (f *FlutterFetcher) StripArchiveTopDir() bool           { return true }

func (f *FlutterFetcher) useEndpoint(defaultURL string) string {
	if f.sm == nil {
		return defaultURL
	}
	custom := f.sm.Get().Endpoints[string(Flutter)]
	if custom == "" {
		return defaultURL
	}
	return strings.Replace(defaultURL, "https://storage.googleapis.com", custom, -1)
}
func (f *FlutterFetcher) Type() SdkType                    { return Flutter }
func (f *FlutterFetcher) GetBinDir() string                 { return "bin" }
func (f *FlutterFetcher) GetExtraEnvVars() map[string]string {
	return map[string]string{"FLUTTER_ROOT": ""}
}
func (f *FlutterFetcher) VerifyCommand() (string, []string) { return "flutter", []string{"--version"} }

func (f *FlutterFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	var versions []VersionInfo
	page := 1
	for page <= 3 {
		url := fmt.Sprintf("https://api.github.com/repos/flutter/flutter/releases?per_page=30&page=%d", page)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("构建Flutter版本请求失败: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := f.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("获取Flutter版本列表失败: %w", err)
		}
		var releases []ghRelease
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("解析Flutter版本数据失败: %w", err)
		}
		resp.Body.Close()
		if len(releases) == 0 { break }

		for _, r := range releases {
			if r.Draft || r.Prerelease { continue }
			tag := r.TagName
			if strings.Contains(tag, "beta") || strings.Contains(tag, "dev") { continue }
			ver := strings.TrimPrefix(tag, "v")
			parts := strings.Split(ver, ".")
			if len(parts) < 2 { continue }
			major, _ := strconv.Atoi(parts[0])
			date := ""
			if t, err := time.Parse(time.RFC3339, r.PublishedAt); err == nil {
				date = t.Format("2006-01-02")
			}
			osName := "windows"
			if runtime.GOOS == "linux" { osName = "linux" }
			if runtime.GOOS == "darwin" { osName = "macos" }
			versions = append(versions, VersionInfo{
				Version: ver, Major: major, ReleaseDate: date,
				DownloadURL: f.useEndpoint(fmt.Sprintf("https://storage.googleapis.com/flutter_infra_release/releases/stable/%s/flutter_%s_%s-stable.zip", osName, osName, ver)),
				FileName:    fmt.Sprintf("flutter_%s_%s-stable.zip", osName, ver),
			})
		}
		page++
	}
	sort.Slice(versions, func(i, j int) bool { return CompareVersions(versions[i].Version, versions[j].Version) > 0 })
	return versions, nil
}

func (f *FlutterFetcher) GetDownloadURL(version string) (string, string, error) {
	osName := "windows"
	if runtime.GOOS == "linux" { osName = "linux" }
	if runtime.GOOS == "darwin" { osName = "macos" }
	url := f.useEndpoint(fmt.Sprintf("https://storage.googleapis.com/flutter_infra_release/releases/stable/%s/flutter_%s_%s-stable.zip", osName, osName, version))
	return url, fmt.Sprintf("flutter_%s_%s-stable.zip", osName, version), nil
}

func (f *FlutterFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(Flutter))
	active := f.cfg.GetActiveVersion(string(Flutter))
	configured := active != ""
	return &SdkStatus{
		SdkType: Flutter, DisplayName: SdkDisplayName(Flutter),
		Configured: configured, PathConfigured: !configured && IsCommandAvailable("flutter"),
		CurrentVersion: active,
		InstalledVersions: installed, InstallPath: f.cfg.SdkDir(string(Flutter)),
	}, nil
}
