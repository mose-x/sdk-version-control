package sdk

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"sdk_version_control/internal/config"
)

type PerlFetcher struct {
	cfg        *config.Config
	sm         *config.SettingsManager
	httpClient *http.Client
}

func NewPerlFetcher(cfg *config.Config, sm *config.SettingsManager) *PerlFetcher {
	return &PerlFetcher{cfg: cfg, sm: sm, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (f *PerlFetcher) SetHTTPClient(client *http.Client) { f.httpClient = client }
func (f *PerlFetcher) StripArchiveTopDir() bool           { return false }

func (f *PerlFetcher) useEndpoint(defaultURL string) string {
	if f.sm == nil {
		return defaultURL
	}
	custom := f.sm.Get().Endpoints[string(Perl)]
	if custom == "" {
		return defaultURL
	}
	return strings.Replace(defaultURL, "https://strawberryperl.com", custom, -1)
}
func (f *PerlFetcher) Type() SdkType                 { return Perl }
func (f *PerlFetcher) GetBinDir() string              { return "perl/bin" }
func (f *PerlFetcher) GetExtraEnvVars() map[string]string { return nil }
func (f *PerlFetcher) VerifyCommand() (string, []string)  { return "perl", []string{"--version"} }

func (f *PerlFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	resp, err := f.httpClient.Get(f.useEndpoint("https://strawberryperl.com/releases.html"))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Perl version list: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Perl version data: %w", err)
	}

	// Match strawberry-perl-X.Y.Z.X-64bit-portable.zip
	re := regexp.MustCompile(`strawberry-perl-(\d+\.\d+\.\d+\.\d+)-64bit-portable\.zip`)
	seen := make(map[string]bool)
	var versions []VersionInfo

	matches := re.FindAllStringSubmatch(string(body), -1)
	for _, m := range matches {
		ver := m[1]
		if seen[ver] { continue }
		seen[ver] = true
		parts := strings.Split(ver, ".")
		major, _ := strconv.Atoi(parts[0])
		versions = append(versions, VersionInfo{
			Version:     ver,
			Major:       major,
			DownloadURL: f.perlDownloadURL(ver),
			FileName:    f.perlFileName(ver),
		})
	}

	sort.Slice(versions, func(i, j int) bool { return CompareVersions(versions[i].Version, versions[j].Version) > 0 })
	return versions, nil
}

func (f *PerlFetcher) GetDownloadURL(version string) (string, string, error) {
	return f.perlDownloadURL(version), f.perlFileName(version), nil
}

func (f *PerlFetcher) perlDownloadURL(ver string) string {
	if runtime.GOOS == "windows" {
		return f.useEndpoint(fmt.Sprintf("https://strawberryperl.com/download/%s/strawberry-perl-%s-64bit-portable.zip", ver, ver))
	}
	return f.useEndpoint(fmt.Sprintf("https://www.cpan.org/src/5.0/perl-%s.tar.gz", ver))
}

func (f *PerlFetcher) perlFileName(ver string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("strawberry-perl-%s-64bit-portable.zip", ver)
	}
	return fmt.Sprintf("perl-%s.tar.gz", ver)
}

func (f *PerlFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(Perl))
	active := f.cfg.GetActiveVersion(string(Perl))
	configured := active != ""
	return &SdkStatus{
		SdkType: Perl, DisplayName: SdkDisplayName(Perl),
		Configured: configured, PathConfigured: !configured && IsCommandAvailable("perl"),
		CurrentVersion: active,
		InstalledVersions: installed, InstallPath: f.cfg.SdkDir(string(Perl)),
	}, nil
}
