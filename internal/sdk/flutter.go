package sdk

import (
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
func (f *FlutterFetcher) StripArchiveTopDir() bool          { return true }

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
func (f *FlutterFetcher) Type() SdkType        { return Flutter }
func (f *FlutterFetcher) GetBinDirs() []string { return []string{"bin"} }
func (f *FlutterFetcher) GetExtraEnvVars() map[string]string {
	return map[string]string{"FLUTTER_ROOT": ""}
}
func (f *FlutterFetcher) VerifyCommand() (string, []string) { return "flutter", []string{"--version"} }

func (f *FlutterFetcher) buildOSName() string {
	switch runtime.GOOS {
	case "linux":
		return "linux"
	case "darwin":
		return "macos"
	default:
		return "windows"
	}
}

// flutterArchSuffix returns the architecture segment inserted into the macOS
// Flutter archive name. macOS arm64 uses an "_arm64" suffix (e.g.
// flutter_macos_arm64_<ver>-stable.zip); all other platforms/arches use "".
// Flutter does not publish arm64-specific builds for Windows or Linux.
// Pure so tests can exercise every (goos, goarch) combo on any host.
func flutterArchSuffix(goos, goarch string) string {
	if goos == "darwin" && goarch == "arm64" {
		return "_arm64"
	}
	return ""
}

func (f *FlutterFetcher) buildExt() string {
	if runtime.GOOS == "linux" {
		return "tar.xz"
	}
	return "zip"
}

func isStableFlutterTag(tag string) bool {
	return !strings.Contains(tag, "beta") && !strings.Contains(tag, "dev") && !strings.Contains(tag, "pre")
}

func (f *FlutterFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	var versions []VersionInfo
	page := 1
	for page <= 3 {
		url := fmt.Sprintf("https://api.github.com/repos/flutter/flutter/releases?per_page=30&page=%d", page)
		var releases []ghRelease
		if err := fetchGithubReleasesPage(f.sm, f.httpClient, url, &releases); err != nil {
			return nil, fmt.Errorf("failed to fetch Flutter version list (page %d): %w", page, err)
		}
		if len(releases) == 0 {
			break
		}

		for _, r := range releases {
			if r.Draft || r.Prerelease {
				continue
			}
			tag := r.TagName
			if !isStableFlutterTag(tag) {
				continue
			}
			ver := strings.TrimPrefix(tag, "v")
			parts := strings.Split(ver, ".")
			if len(parts) < 2 {
				continue
			}
			major, _ := strconv.Atoi(parts[0])
			date := ""
			if t, err := time.Parse(time.RFC3339, r.PublishedAt); err == nil {
				date = t.Format("2006-01-02")
			}
			osName := f.buildOSName()
			ext := f.buildExt()
			archSuffix := flutterArchSuffix(runtime.GOOS, runtime.GOARCH)
			versions = append(versions, VersionInfo{
				Version: ver, Major: major, ReleaseDate: date,
				DownloadURL: f.useEndpoint(fmt.Sprintf("https://storage.googleapis.com/flutter_infra_release/releases/stable/%s/flutter_%s%s_%s-stable.%s", osName, osName, archSuffix, ver, ext)),
				FileName:    fmt.Sprintf("flutter_%s%s_%s-stable.%s", osName, archSuffix, ver, ext),
			})
		}
		page++
	}
	sort.Slice(versions, func(i, j int) bool { return CompareVersions(versions[i].Version, versions[j].Version) > 0 })
	return versions, nil
}

func (f *FlutterFetcher) GetDownloadURL(version string) (string, string, error) {
	osName := f.buildOSName()
	ext := f.buildExt()
	archSuffix := flutterArchSuffix(runtime.GOOS, runtime.GOARCH)
	url := f.useEndpoint(fmt.Sprintf("https://storage.googleapis.com/flutter_infra_release/releases/stable/%s/flutter_%s%s_%s-stable.%s", osName, osName, archSuffix, version, ext))
	return url, fmt.Sprintf("flutter_%s%s_%s-stable.%s", osName, archSuffix, version, ext), nil
}

func (f *FlutterFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(Flutter))
	active := f.cfg.GetActiveVersion(string(Flutter))
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
		SdkType: Flutter, DisplayName: SdkDisplayName(Flutter),
		Configured: configured, PathConfigured: !configured && IsCommandAvailable("flutter"),
		CurrentVersion:    active,
		InstalledVersions: installed, InstallPath: f.cfg.SdkDir(string(Flutter)),
		NeedsSwitch: needsSwitch,
	}, nil
}
