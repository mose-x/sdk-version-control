package sdk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"sdk_version_control/internal/config"
)

type DartFetcher struct {
	cfg        *config.Config
	sm         *config.SettingsManager
	httpClient *http.Client
}

func NewDartFetcher(cfg *config.Config, sm *config.SettingsManager) *DartFetcher {
	return &DartFetcher{cfg: cfg, sm: sm, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (f *DartFetcher) SetHTTPClient(client *http.Client) { f.httpClient = client }
func (f *DartFetcher) StripArchiveTopDir() bool          { return false }

func (f *DartFetcher) useEndpoint(defaultURL string) string {
	if f.sm == nil {
		return defaultURL
	}
	custom := f.sm.Get().Endpoints[string(Dart)]
	if custom == "" {
		return defaultURL
	}
	return strings.Replace(defaultURL, "https://storage.googleapis.com", custom, -1)
}
func (f *DartFetcher) Type() SdkType                      { return Dart }
func (f *DartFetcher) GetBinDir() string                  { return "dart-sdk/bin" }
func (f *DartFetcher) GetExtraEnvVars() map[string]string { return nil }
func (f *DartFetcher) VerifyCommand() (string, []string)  { return "dart", []string{"--version"} }

type gcsListResponse struct {
	Prefixes      []string `json:"prefixes"`
	NextPageToken string   `json:"nextPageToken"`
}

func (f *DartFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	var versions []VersionInfo
	pageToken := ""

	for {
		apiURL := f.useEndpoint("https://storage.googleapis.com/storage/v1/b/dart-archive/o?prefix=channels/stable/release/&delimiter=/&maxResults=200")
		if pageToken != "" {
			apiURL += "&pageToken=" + url.QueryEscape(pageToken)
		}
		resp, err := f.httpClient.Get(apiURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Dart version list: %w", err)
		}
		var result gcsListResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to parse Dart version data: %w", err)
		}
		resp.Body.Close()

		for _, prefix := range result.Prefixes {
			// prefix format: "channels/stable/release/3.6.0/"
			parts := strings.Split(strings.TrimSuffix(prefix, "/"), "/")
			if len(parts) < 4 {
				continue
			}
			ver := parts[3]
			if ver == "latest" {
				continue
			}
			// Filter dev/beta
			if strings.Contains(ver, "-") {
				continue
			}
			vParts := strings.Split(ver, ".")
			if len(vParts) < 2 {
				continue
			}
			major, _ := strconv.Atoi(vParts[0])

			osName := "windows"
			arch := "x64"
			if runtime.GOOS == "linux" {
				osName = "linux"
			}
			if runtime.GOOS == "darwin" {
				osName = "macos"
				if runtime.GOARCH == "arm64" {
					arch = "arm64"
				}
			}

			versions = append(versions, VersionInfo{
				Version:     ver,
				Major:       major,
				DownloadURL: f.useEndpoint(fmt.Sprintf("https://storage.googleapis.com/dart-archive/channels/stable/release/%s/sdk/dartsdk-%s-%s-release.zip", ver, osName, arch)),
				FileName:    fmt.Sprintf("dartsdk-%s-%s-release.zip", osName, arch),
			})
		}

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	sort.Slice(versions, func(i, j int) bool { return CompareVersions(versions[i].Version, versions[j].Version) > 0 })
	return versions, nil
}

func (f *DartFetcher) GetDownloadURL(version string) (string, string, error) {
	osName := "windows"
	arch := "x64"
	if runtime.GOOS == "linux" {
		osName = "linux"
	}
	if runtime.GOOS == "darwin" {
		osName = "macos"
		if runtime.GOARCH == "arm64" {
			arch = "arm64"
		}
	}
	url := f.useEndpoint(fmt.Sprintf("https://storage.googleapis.com/dart-archive/channels/stable/release/%s/sdk/dartsdk-%s-%s-release.zip", version, osName, arch))
	return url, fmt.Sprintf("dartsdk-%s-%s-release.zip", osName, arch), nil
}

func (f *DartFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(Dart))
	active := f.cfg.GetActiveVersion(string(Dart))
	configured := active != ""

	needsSwitch := false
	if active != "" {
		found := false
		for _, v := range installed {
			if v == active {
				found = true
				break
			}
		}
		needsSwitch = !found
	}

	return &SdkStatus{
		SdkType: Dart, DisplayName: SdkDisplayName(Dart),
		Configured: configured, PathConfigured: !configured && IsCommandAvailable("dart"),
		CurrentVersion:    active,
		InstalledVersions: installed, InstallPath: f.cfg.SdkDir(string(Dart)),
		NeedsSwitch: needsSwitch,
	}, nil
}
