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

// PythonFetcher Python 版本获取器
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
		return "" // python.exe 在根目录
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
		return nil, fmt.Errorf("获取Python版本列表失败: %w", err)
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("解析Python版本页面失败: %w", err)
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
		// 过滤掉 alpha/beta/rc 版本
		if strings.Contains(href, "a") || strings.Contains(href, "b") || strings.Contains(href, "rc") {
			return
		}
		parts := strings.Split(href, ".")
		if len(parts) < 2 {
			return
		}
		major, _ := strconv.Atoi(parts[0])
		minor, _ := strconv.Atoi(parts[1])
		// 只要 Python 3.8+
		if major < 3 || (major == 3 && minor < 8) {
			return
		}

		url, fileName := f.buildDownloadURL(href)
		if url == "" {
			return
		}

		// 并发验证下载包是否存在（HEAD 请求）
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
				return // 该版本没有 Windows 二进制包，跳过
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
		// Windows 使用 embed 包（免安装版）
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
		return "", "", fmt.Errorf("不支持当前平台")
	}
	return url, fileName, nil
}

func (f *PythonFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(Python))
	active := f.cfg.GetActiveVersion(string(Python))
	configured := active != ""

	return &SdkStatus{
		SdkType:           Python,
		DisplayName:       SdkDisplayName(Python),
		Configured:        configured,
		PathConfigured:    !configured && IsCommandAvailable("python"),
		CurrentVersion:    active,
		InstalledVersions: installed,
		InstallPath:       f.cfg.SdkDir(string(Python)),
	}, nil
}
