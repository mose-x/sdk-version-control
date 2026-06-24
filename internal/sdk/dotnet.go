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

type DotNetFetcher struct {
	cfg        *config.Config
	sm         *config.SettingsManager
	httpClient *http.Client
}

func NewDotNetFetcher(cfg *config.Config, sm *config.SettingsManager) *DotNetFetcher {
	return &DotNetFetcher{cfg: cfg, sm: sm, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (f *DotNetFetcher) SetHTTPClient(client *http.Client) { f.httpClient = client }
func (f *DotNetFetcher) StripArchiveTopDir() bool           { return true }

func (f *DotNetFetcher) useEndpoint(defaultURL string) string {
	if f.sm == nil {
		return defaultURL
	}
	custom := f.sm.Get().Endpoints[string(DotNet)]
	if custom == "" {
		return defaultURL
	}
	defaultURL = strings.Replace(defaultURL, "https://dotnetcli.blob.core.windows.net", custom, -1)
	return strings.Replace(defaultURL, "https://dotnetcli.azureedge.net", custom, -1)
}
func (f *DotNetFetcher) Type() SdkType                  { return DotNet }
func (f *DotNetFetcher) GetBinDir() string               { return "" }
func (f *DotNetFetcher) GetExtraEnvVars() map[string]string {
	return map[string]string{"DOTNET_ROOT": ""}
}
func (f *DotNetFetcher) VerifyCommand() (string, []string) { return "dotnet", []string{"--version"} }

type dotnetReleaseIndex struct {
	ReleasesIndex []struct {
		ChannelVersion string `json:"channel-version"`
		LatestRelease  string `json:"latest-release"`
		SupportPhase   string `json:"support-phase"`
		ReleasesJSON   string `json:"releases.json"`
	} `json:"releases-index"`
}

func (f *DotNetFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	resp, err := f.httpClient.Get(f.useEndpoint("https://dotnetcli.blob.core.windows.net/dotnet/release-metadata/releases-index.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch .NET version list: %w", err)
	}
	defer resp.Body.Close()

	var index dotnetReleaseIndex
	if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
		return nil, fmt.Errorf("failed to parse .NET version data: %w", err)
	}

	var versions []VersionInfo
	for _, ch := range index.ReleasesIndex {
		ver := ch.LatestRelease
		parts := strings.Split(ver, ".")
		major, _ := strconv.Atoi(parts[0])
		isLTS := ch.SupportPhase == "lts" || ch.SupportPhase == "maintainance"
		versions = append(versions, VersionInfo{
			Version:     ver,
			Major:       major,
			IsLTS:       isLTS,
			DownloadURL: f.buildURL(ver),
			FileName:    f.buildFileName(ver),
		})
	}
	sort.Slice(versions, func(i, j int) bool { return CompareVersions(versions[i].Version, versions[j].Version) > 0 })
	return versions, nil
}

func (f *DotNetFetcher) buildURL(version string) string {
	rid := "win-x64"
	if runtime.GOOS == "linux" { rid = "linux-x64" }
	if runtime.GOOS == "darwin" {
		rid = "osx-x64"
		if runtime.GOARCH == "arm64" { rid = "osx-arm64" }
	}
	return f.useEndpoint(fmt.Sprintf("https://dotnetcli.azureedge.net/dotnet/Sdk/%s/dotnet-sdk-%s-%s.zip", version, version, rid))
}

func (f *DotNetFetcher) buildFileName(version string) string {
	rid := "win-x64"
	if runtime.GOOS == "linux" { rid = "linux-x64" }
	if runtime.GOOS == "darwin" {
		rid = "osx-x64"
		if runtime.GOARCH == "arm64" { rid = "osx-arm64" }
	}
	return fmt.Sprintf("dotnet-sdk-%s-%s.zip", version, rid)
}

func (f *DotNetFetcher) GetDownloadURL(version string) (string, string, error) {
	return f.buildURL(version), f.buildFileName(version), nil
}

func (f *DotNetFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(DotNet))
	active := f.cfg.GetActiveVersion(string(DotNet))
	configured := active != ""
	return &SdkStatus{
		SdkType: DotNet, DisplayName: SdkDisplayName(DotNet),
		Configured: configured, PathConfigured: !configured && IsCommandAvailable("dotnet"),
		CurrentVersion: active,
		InstalledVersions: installed, InstallPath: f.cfg.SdkDir(string(DotNet)),
	}, nil
}
