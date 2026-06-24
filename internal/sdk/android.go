package sdk

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"time"

	"sdk_version_control/internal/config"
)

type AndroidFetcher struct {
	cfg        *config.Config
	sm         *config.SettingsManager
	httpClient *http.Client
}

func NewAndroidFetcher(cfg *config.Config, sm *config.SettingsManager) *AndroidFetcher {
	return &AndroidFetcher{cfg: cfg, sm: sm, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (f *AndroidFetcher) SetHTTPClient(client *http.Client) { f.httpClient = client }
func (f *AndroidFetcher) StripArchiveTopDir() bool           { return false }

func (f *AndroidFetcher) useEndpoint(defaultURL string) string {
	if f.sm == nil {
		return defaultURL
	}
	custom := f.sm.Get().Endpoints[string(Android)]
	if custom == "" {
		return defaultURL
	}
	return strings.Replace(defaultURL, "https://dl.google.com", custom, -1)
}
func (f *AndroidFetcher) Type() SdkType                    { return Android }
func (f *AndroidFetcher) GetBinDir() string                 { return "cmdline-tools/latest/bin" }
func (f *AndroidFetcher) GetExtraEnvVars() map[string]string {
	return map[string]string{"ANDROID_HOME": "", "ANDROID_SDK_ROOT": ""}
}
func (f *AndroidFetcher) VerifyCommand() (string, []string) { return "sdkmanager", []string{"--version"} }

// Android repository XML structure
type androidRepository struct {
	XMLName xml.Name         `xml:"sdk-repository"`
	Packages []androidPackage `xml:"remotePackage"`
}

type androidPackage struct {
	Path     string `xml:"path,attr"`
	Revision struct {
		Major int `xml:"major"`
		Minor int `xml:"minor"`
		Micro int `xml:"micro"`
	} `xml:"revision"`
	Archives struct {
		Archive []struct {
			OS   string `xml:"host-os,attr"`
			URL  string `xml:"complete>url"`
			Size int64  `xml:"complete>size"`
		} `xml:"archive"`
	} `xml:"archives"`
}

func (f *AndroidFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	resp, err := f.httpClient.Get(f.useEndpoint("https://dl.google.com/android/repository/repository2-3.xml"))
	if err != nil {
		// Fall back to known versions
		return f.fallbackVersions(), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return f.fallbackVersions(), nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return f.fallbackVersions(), nil
	}

	var repo androidRepository
	if err := xml.Unmarshal(body, &repo); err != nil {
		return f.fallbackVersions(), nil
	}

	osKey := "windows"
	if runtime.GOOS == "linux" { osKey = "linux" }
	if runtime.GOOS == "darwin" { osKey = "macosx" }

	seen := make(map[string]bool)
	var versions []VersionInfo
	for _, pkg := range repo.Packages {
		if !strings.HasPrefix(pkg.Path, "cmdline-tools;") {
			continue
		}
		ver := fmt.Sprintf("%d.%d.%d", pkg.Revision.Major, pkg.Revision.Minor, pkg.Revision.Micro)
		if seen[ver] { continue }
		seen[ver] = true

		// Find the download URL matching the current platform
		downloadURL := ""
		fileName := ""
		for _, a := range pkg.Archives.Archive {
			if a.OS == osKey || a.OS == "" {
				downloadURL = f.useEndpoint("https://dl.google.com/android/repository/" + a.URL)
				parts := strings.Split(a.URL, "/")
				fileName = parts[len(parts)-1]
				break
			}
		}
		if downloadURL == "" {
			continue
		}

		versions = append(versions, VersionInfo{
			Version:     ver,
			Major:       pkg.Revision.Major,
			DownloadURL: downloadURL,
			FileName:    fileName,
		})
	}

	if len(versions) == 0 {
		return f.fallbackVersions(), nil
	}

	sort.Slice(versions, func(i, j int) bool { return CompareVersions(versions[i].Version, versions[j].Version) > 0 })
	return versions, nil
}

func (f *AndroidFetcher) fallbackVersions() []VersionInfo {
	osKey := "win"
	if runtime.GOOS == "linux" { osKey = "linux" }
	if runtime.GOOS == "darwin" { osKey = "mac" }
	build := "14742923"
	return []VersionInfo{{
		Version:     "14.0",
		Major:       14,
		DownloadURL: f.useEndpoint(fmt.Sprintf("https://dl.google.com/android/repository/commandlinetools-%s-%s_latest.zip", osKey, build)),
		FileName:    fmt.Sprintf("commandlinetools-%s-%s_latest.zip", osKey, build),
	}}
}

func (f *AndroidFetcher) GetDownloadURL(version string) (string, string, error) {
	// First try to fetch from remote
	versions, _ := f.FetchRemoteVersions()
	for _, v := range versions {
		if v.Version == version {
			return v.DownloadURL, v.FileName, nil
		}
	}
	osKey := "win"
	if runtime.GOOS == "linux" { osKey = "linux" }
	if runtime.GOOS == "darwin" { osKey = "mac" }
	build := "14742923"
	return f.useEndpoint(fmt.Sprintf("https://dl.google.com/android/repository/commandlinetools-%s-%s_latest.zip", osKey, build)),
		fmt.Sprintf("commandlinetools-%s-%s_latest.zip", osKey, build), nil
}

func (f *AndroidFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(Android))
	active := f.cfg.GetActiveVersion(string(Android))
	configured := active != ""
	return &SdkStatus{
		SdkType: Android, DisplayName: SdkDisplayName(Android),
		Configured: configured, PathConfigured: !configured && IsCommandAvailable("sdkmanager"),
		CurrentVersion: active,
		InstalledVersions: installed, InstallPath: f.cfg.SdkDir(string(Android)),
	}, nil
}
