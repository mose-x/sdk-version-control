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
func (f *AndroidFetcher) StripArchiveTopDir() bool          { return false }

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
func (f *AndroidFetcher) Type() SdkType { return Android }
func (f *AndroidFetcher) GetBinDirs() []string {
	// The cmdline-tools zip extracts to cmdline-tools/bin/ directly — there is
	// no "latest/" layer inside the archive. StripArchiveTopDir()=false keeps
	// the cmdline-tools/ top dir so this path resolves.
	return []string{"cmdline-tools/bin"}
}
func (f *AndroidFetcher) GetExtraEnvVars() map[string]string {
	return map[string]string{"ANDROID_HOME": "", "ANDROID_SDK_ROOT": ""}
}
func (f *AndroidFetcher) VerifyCommand() (string, []string) {
	return "sdkmanager", []string{"--version"}
}

// Android repository XML structure
type androidRepository struct {
	XMLName  xml.Name         `xml:"sdk-repository"`
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
			OS   string `xml:"host-os"`
			URL  string `xml:"complete>url"`
			Size int64  `xml:"complete>size"`
		} `xml:"archive"`
	} `xml:"archives"`
}

func (f *AndroidFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	resp, err := f.httpClient.Get(f.useEndpoint("https://dl.google.com/android/repository/repository2-3.xml"))
	if err != nil {
		return nil, fmt.Errorf("android repository request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("android repository returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read android repository response: %w", err)
	}

	var repo androidRepository
	if err := xml.Unmarshal(body, &repo); err != nil {
		return nil, fmt.Errorf("failed to parse android repository XML: %w", err)
	}

	osKey := "windows"
	if runtime.GOOS == "linux" {
		osKey = "linux"
	}
	if runtime.GOOS == "darwin" {
		osKey = "macosx"
	}

	seen := make(map[string]bool)
	var versions []VersionInfo
	for _, pkg := range repo.Packages {
		if !strings.HasPrefix(pkg.Path, "cmdline-tools;") {
			continue
		}
		ver := fmt.Sprintf("%d.%d.%d", pkg.Revision.Major, pkg.Revision.Minor, pkg.Revision.Micro)
		if seen[ver] {
			continue
		}
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
		return nil, fmt.Errorf("no Android cmdline-tools versions found in repository")
	}

	sort.Slice(versions, func(i, j int) bool { return CompareVersions(versions[i].Version, versions[j].Version) > 0 })
	return versions, nil
}

func (f *AndroidFetcher) GetDownloadURL(version string) (string, string, error) {
	// Cache short-circuit: skip a fresh FetchRemoteVersions round-trip when the
	// version list was already fetched (see PythonFetcher.GetDownloadURL).
	if url, name, ok := LookupCachedDownloadURL(Android, version); ok {
		return url, name, nil
	}
	// Fetch from remote. Unlike the previous behaviour (which silently fell
	// back to a hardcoded 14.0/build when the XML fetch failed), a fetch
	// failure now surfaces as an error so the caller does not install the
	// wrong version.
	versions, err := f.FetchRemoteVersions()
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch Android cmdline-tools versions: %w", err)
	}
	for _, v := range versions {
		if v.Version == version {
			return v.DownloadURL, v.FileName, nil
		}
	}
	return "", "", fmt.Errorf("Android cmdline-tools version not found: %s", version)
}

func (f *AndroidFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(Android))
	active := f.cfg.GetActiveVersion(string(Android))
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
		SdkType: Android, DisplayName: SdkDisplayName(Android),
		Configured: configured, PathConfigured: !configured && IsCommandAvailable("sdkmanager"),
		CurrentVersion:    active,
		InstalledVersions: installed, InstallPath: f.cfg.SdkDir(string(Android)),
		NeedsSwitch: needsSwitch,
	}, nil
}
