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

type PHPFetcher struct {
	cfg        *config.Config
	sm         *config.SettingsManager
	httpClient *http.Client
}

func NewPHPFetcher(cfg *config.Config, sm *config.SettingsManager) *PHPFetcher {
	return &PHPFetcher{cfg: cfg, sm: sm, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (f *PHPFetcher) SetHTTPClient(client *http.Client) { f.httpClient = client }
func (f *PHPFetcher) StripArchiveTopDir() bool           { return true }

func (f *PHPFetcher) useEndpoint(defaultURL string) string {
	if f.sm == nil {
		return defaultURL
	}
	custom := f.sm.Get().Endpoints[string(PHP)]
	if custom == "" {
		return defaultURL
	}
	defaultURL = strings.Replace(defaultURL, "https://windows.php.net", custom, -1)
	defaultURL = strings.Replace(defaultURL, "https://www.php.net", custom, -1)
	return defaultURL
}
func (f *PHPFetcher) Type() SdkType                { return PHP }
func (f *PHPFetcher) GetBinDir() string {
	if runtime.GOOS != "windows" {
		return "bin"
	}
	return ""
}
func (f *PHPFetcher) GetExtraEnvVars() map[string]string { return nil }
func (f *PHPFetcher) VerifyCommand() (string, []string)  { return "php", []string{"--version"} }

func (f *PHPFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	resp, err := f.httpClient.Get(f.useEndpoint("https://windows.php.net/downloads/releases/"))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PHP version list: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read PHP version data: %w", err)
	}

	// Match php-X.Y.Z-nts-Win32-vs16-x64.zip (or non-nts variants)
	re := regexp.MustCompile(`php-(\d+\.\d+\.\d+)-nts-Win32-vs\d+-x64\.zip`)
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
			DownloadURL: f.phpDownloadURL(ver),
			FileName:    f.phpFileName(ver),
		})
	}

	sort.Slice(versions, func(i, j int) bool { return CompareVersions(versions[i].Version, versions[j].Version) > 0 })
	return versions, nil
}

func (f *PHPFetcher) GetDownloadURL(version string) (string, string, error) {
	return f.phpDownloadURL(version), f.phpFileName(version), nil
}

func (f *PHPFetcher) phpDownloadURL(ver string) string {
	if runtime.GOOS == "windows" {
		return f.useEndpoint(fmt.Sprintf("https://windows.php.net/downloads/releases/php-%s-nts-Win32-vs16-x64.zip", ver))
	}
	return f.useEndpoint(fmt.Sprintf("https://www.php.net/distributions/php-%s.tar.gz", ver))
}

func (f *PHPFetcher) phpFileName(ver string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("php-%s-nts-Win32-vs16-x64.zip", ver)
	}
	return fmt.Sprintf("php-%s.tar.gz", ver)
}

func (f *PHPFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(PHP))
	active := f.cfg.GetActiveVersion(string(PHP))
	configured := active != ""
	return &SdkStatus{
		SdkType: PHP, DisplayName: SdkDisplayName(PHP),
		Configured: configured, PathConfigured: !configured && IsCommandAvailable("php"),
		CurrentVersion: active,
		InstalledVersions: installed, InstallPath: f.cfg.SdkDir(string(PHP)),
	}, nil
}
