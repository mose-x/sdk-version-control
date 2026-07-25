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

type RubyFetcher struct {
	cfg        *config.Config
	sm         *config.SettingsManager
	httpClient *http.Client
}

func NewRubyFetcher(cfg *config.Config, sm *config.SettingsManager) *RubyFetcher {
	return &RubyFetcher{cfg: cfg, sm: sm, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (f *RubyFetcher) SetHTTPClient(client *http.Client) { f.httpClient = client }
func (f *RubyFetcher) StripArchiveTopDir() bool          { return true }

func (f *RubyFetcher) useEndpoint(defaultURL string) string {
	if f.sm == nil {
		return defaultURL
	}
	custom := f.sm.Get().Endpoints[string(Ruby)]
	if custom == "" {
		return defaultURL
	}
	defaultURL = strings.Replace(defaultURL, "https://github.com", custom, -1)
	defaultURL = strings.Replace(defaultURL, "https://cache.ruby-lang.org", custom, -1)
	return defaultURL
}
func (f *RubyFetcher) Type() SdkType                      { return Ruby }
func (f *RubyFetcher) GetBinDir() string                  { return "bin" }
func (f *RubyFetcher) GetExtraEnvVars() map[string]string { return nil }
func (f *RubyFetcher) VerifyCommand() (string, []string)  { return "ruby", []string{"--version"} }

func (f *RubyFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	var versions []VersionInfo
	page := 1
	for page <= 3 {
		url := fmt.Sprintf("https://api.github.com/repos/oneclick/rubyinstaller2/releases?per_page=30&page=%d", page)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build Ruby version request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := f.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Ruby version list: %w", err)
		}
		var releases []ghRelease
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to parse Ruby version data: %w", err)
		}
		resp.Body.Close()
		if len(releases) == 0 {
			break
		}

		for _, r := range releases {
			if r.Draft || r.Prerelease {
				continue
			}
			tag := strings.TrimPrefix(r.TagName, "RubyInstaller-")
			tag = strings.TrimPrefix(tag, "v")
			// Only keep pure version numbers
			ver := tag
			if idx := strings.Index(tag, "-"); idx > 0 {
				ver = tag[:idx]
			}
			parts := strings.Split(ver, ".")
			if len(parts) < 2 {
				continue
			}
			major, _ := strconv.Atoi(parts[0])
			date := ""
			if t, err := time.Parse(time.RFC3339, r.PublishedAt); err == nil {
				date = t.Format("2006-01-02")
			}
			versions = append(versions, VersionInfo{
				Version: ver, Major: major, ReleaseDate: date,
				DownloadURL: f.rubyDownloadURL(ver),
				FileName:    f.rubyFileName(ver),
			})
		}
		page++
	}
	sort.Slice(versions, func(i, j int) bool { return CompareVersions(versions[i].Version, versions[j].Version) > 0 })
	return versions, nil
}

func (f *RubyFetcher) GetDownloadURL(version string) (string, string, error) {
	return f.rubyDownloadURL(version), f.rubyFileName(version), nil
}

func (f *RubyFetcher) rubyDownloadURL(ver string) string {
	if runtime.GOOS == "windows" {
		return f.useEndpoint(fmt.Sprintf("https://github.com/oneclick/rubyinstaller2/releases/download/RubyInstaller-%s/rubyinstaller-%s-1-x64.7z", ver, ver))
	}
	return f.useEndpoint(fmt.Sprintf("https://cache.ruby-lang.org/pub/ruby/%s/ruby-%s.tar.gz", majorMinor(ver), ver))
}

func (f *RubyFetcher) rubyFileName(ver string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("rubyinstaller-%s-1-x64.7z", ver)
	}
	return fmt.Sprintf("ruby-%s.tar.gz", ver)
}

func majorMinor(ver string) string {
	parts := strings.Split(ver, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return ver
}

func (f *RubyFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(Ruby))
	active := f.cfg.GetActiveVersion(string(Ruby))
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
		SdkType: Ruby, DisplayName: SdkDisplayName(Ruby),
		Configured: configured, PathConfigured: !configured && IsCommandAvailable("ruby"),
		CurrentVersion:    active,
		InstalledVersions: installed, InstallPath: f.cfg.SdkDir(string(Ruby)),
		NeedsSwitch: needsSwitch,
	}, nil
}
