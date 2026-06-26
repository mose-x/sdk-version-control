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

type RustFetcher struct {
	cfg        *config.Config
	sm         *config.SettingsManager
	httpClient *http.Client
}

func NewRustFetcher(cfg *config.Config, sm *config.SettingsManager) *RustFetcher {
	return &RustFetcher{cfg: cfg, sm: sm, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (f *RustFetcher) SetHTTPClient(client *http.Client) { f.httpClient = client }
func (f *RustFetcher) StripArchiveTopDir() bool          { return true }

func (f *RustFetcher) useEndpoint(defaultURL string) string {
	if f.sm == nil {
		return defaultURL
	}
	custom := f.sm.Get().Endpoints[string(Rust)]
	if custom == "" {
		return defaultURL
	}
	return strings.Replace(defaultURL, "https://static.rust-lang.org", custom, -1)
}

func (f *RustFetcher) Type() SdkType     { return Rust }
func (f *RustFetcher) GetBinDir() string { return "cargo/bin" }
func (f *RustFetcher) GetExtraEnvVars() map[string]string {
	return nil
}
func (f *RustFetcher) VerifyCommand() (string, []string) { return "rustc", []string{"--version"} }

type ghRelease struct {
	TagName     string `json:"tag_name"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
}

func (f *RustFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	var versions []VersionInfo
	page := 1
	for page <= 3 {
		url := fmt.Sprintf("https://api.github.com/repos/rust-lang/rust/releases?per_page=30&page=%d", page)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build Rust version request: %w", err)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := f.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Rust version list: %w", err)
		}
		var releases []ghRelease
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to parse Rust version data: %w", err)
		}
		resp.Body.Close()
		if len(releases) == 0 {
			break
		}

		for _, r := range releases {
			if r.Draft || r.Prerelease {
				continue
			}
			tag := r.TagName
			if strings.Contains(tag, "beta") || strings.Contains(tag, "nightly") || strings.Contains(tag, "alpha") {
				continue
			}
			parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
			major, _ := strconv.Atoi(parts[0])
			date := ""
			if t, err := time.Parse(time.RFC3339, r.PublishedAt); err == nil {
				date = t.Format("2006-01-02")
			}
			versions = append(versions, VersionInfo{
				Version:     strings.TrimPrefix(tag, "v"),
				Major:       major,
				ReleaseDate: date,
				DownloadURL: f.buildDownloadURL(strings.TrimPrefix(tag, "v")),
				FileName:    f.buildFileName(strings.TrimPrefix(tag, "v")),
			})
		}
		page++
	}

	sort.Slice(versions, func(i, j int) bool {
		return CompareVersions(versions[i].Version, versions[j].Version) > 0
	})
	return versions, nil
}

func (f *RustFetcher) GetDownloadURL(version string) (string, string, error) {
	return f.buildDownloadURL(version), f.buildFileName(version), nil
}

func (f *RustFetcher) buildDownloadURL(version string) string {
	target := "x86_64-pc-windows-msvc"
	if runtime.GOOS == "linux" {
		target = "x86_64-unknown-linux-gnu"
	}
	if runtime.GOOS == "darwin" {
		target = "x86_64-apple-darwin"
		if runtime.GOARCH == "arm64" {
			target = "aarch64-apple-darwin"
		}
	}
	return f.useEndpoint(fmt.Sprintf("https://static.rust-lang.org/dist/rust-%s-%s.tar.gz", version, target))
}

func (f *RustFetcher) buildFileName(version string) string {
	target := "x86_64-pc-windows-msvc"
	if runtime.GOOS == "linux" {
		target = "x86_64-unknown-linux-gnu"
	}
	if runtime.GOOS == "darwin" {
		target = "x86_64-apple-darwin"
		if runtime.GOARCH == "arm64" {
			target = "aarch64-apple-darwin"
		}
	}
	return fmt.Sprintf("rust-%s-%s.tar.gz", version, target)
}

func (f *RustFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(Rust))
	active := f.cfg.GetActiveVersion(string(Rust))
	configured := active != ""
	return &SdkStatus{
		SdkType: Rust, DisplayName: SdkDisplayName(Rust),
		Configured: configured, PathConfigured: !configured && IsCommandAvailable("rustc"),
		CurrentVersion:    active,
		InstalledVersions: installed, InstallPath: f.cfg.SdkDir(string(Rust)),
	}, nil
}
