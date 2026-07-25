package sdk

import (
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"sdk_version_control/internal/config"
)

// PythonFetcher Python version fetcher
type PythonFetcher struct {
	cfg        *config.Config
	sm         *config.SettingsManager
	httpClient *http.Client
}

func NewPythonFetcher(cfg *config.Config, sm *config.SettingsManager) *PythonFetcher {
	return &PythonFetcher{cfg: cfg, sm: sm, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (f *PythonFetcher) SetHTTPClient(client *http.Client) { f.httpClient = client }
func (f *PythonFetcher) StripArchiveTopDir() bool           { return true }

func (f *PythonFetcher) useEndpoint(defaultURL string) string {
	if f.sm == nil {
		return defaultURL
	}
	custom := f.sm.Get().Endpoints[string(Python)]
	if custom == "" {
		return defaultURL
	}
	return strings.Replace(defaultURL, "https://www.python.org", custom, -1)
}

func (f *PythonFetcher) Type() SdkType {
	return Python
}

func (f *PythonFetcher) GetBinDir() string {
	if config.IsWindows() {
		return "" // python.exe is in the root directory
	}
	return "bin"
}

func (f *PythonFetcher) GetExtraEnvVars() map[string]string {
	return nil
}

func (f *PythonFetcher) VerifyCommand() (string, []string) {
	return "python", []string{"--version"}
}

func (f *PythonFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	resp, err := f.httpClient.Get(f.useEndpoint("https://www.python.org/ftp/python/"))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Python version list: %w", err)
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Python version page: %w", err)
	}

	var versions []VersionInfo
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	doc.Find("pre a").Each(func(i int, s *goquery.Selection) {
		href := strings.TrimSuffix(s.Text(), "/")
		if href == "" || href == ".." {
			return
		}
		// Filter out alpha/beta/rc versions
		if strings.Contains(href, "a") || strings.Contains(href, "b") || strings.Contains(href, "rc") {
			return
		}
		parts := strings.Split(href, ".")
		if len(parts) < 2 {
			return
		}
		major, _ := strconv.Atoi(parts[0])
		minor, _ := strconv.Atoi(parts[1])
		// Only Python 3.8+
		if major < 3 || (major == 3 && minor < 8) {
			return
		}

		url, fileName := f.buildDownloadURL(href)
		if url == "" {
			return
		}

		// Concurrently verify the download package exists (HEAD request)
		wg.Add(1)
		go func(version, dlURL, fn string, maj, min int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			req, err := http.NewRequest("HEAD", dlURL, nil)
			if err != nil {
				return
			}
			resp, err := f.httpClient.Do(req)
			if err != nil {
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return // No Windows binary package for this version, skip
			}
			mu.Lock()
			versions = append(versions, VersionInfo{
				Version:     version,
				Major:       maj,
				DownloadURL: dlURL,
				FileName:    fn,
				IsLTS:       false,
			})
			mu.Unlock()
		}(href, url, fileName, major, minor)
	})

	wg.Wait()

	sort.Slice(versions, func(i, j int) bool {
		return CompareVersions(versions[i].Version, versions[j].Version) > 0
	})

	return versions, nil
}

func (f *PythonFetcher) buildDownloadURL(version string) (string, string) {
	os := runtime.GOOS
	arch := runtime.GOARCH

	switch {
	case os == "windows" && arch == "amd64":
		// Windows uses the embed package (portable version)
		fileName := fmt.Sprintf("python-%s-embed-amd64.zip", version)
		url := f.useEndpoint(fmt.Sprintf("https://www.python.org/ftp/python/%s/%s", version, fileName))
		return url, fileName
	case os == "linux" && arch == "amd64":
		fileName := fmt.Sprintf("Python-%s.tar.xz", version)
		url := f.useEndpoint(fmt.Sprintf("https://www.python.org/ftp/python/%s/%s", version, fileName))
		return url, fileName
	case os == "darwin" && arch == "arm64":
		fileName := fmt.Sprintf("Python-%s.tar.xz", version)
		url := f.useEndpoint(fmt.Sprintf("https://www.python.org/ftp/python/%s/%s", version, fileName))
		return url, fileName
	case os == "darwin" && arch == "amd64":
		fileName := fmt.Sprintf("Python-%s.tar.xz", version)
		url := f.useEndpoint(fmt.Sprintf("https://www.python.org/ftp/python/%s/%s", version, fileName))
		return url, fileName
	default:
		return "", ""
	}
}

func (f *PythonFetcher) GetDownloadURL(version string) (string, string, error) {
	url, fileName := f.buildDownloadURL(version)
	if url == "" {
		return "", "", fmt.Errorf("current platform is not supported")
	}
	return url, fileName, nil
}

func (f *PythonFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(Python))
	active := f.cfg.GetActiveVersion(string(Python))
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
		SdkType:           Python,
		DisplayName:       SdkDisplayName(Python),
		Configured:        configured,
		PathConfigured:    !configured && IsCommandAvailable("python"),
		CurrentVersion:    active,
		InstalledVersions: installed,
		InstallPath:       f.cfg.SdkDir(string(Python)),
		NeedsSwitch:       needsSwitch,
	}, nil
}
