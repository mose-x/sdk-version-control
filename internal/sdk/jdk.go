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

// JdkFetcher JDK 版本获取器 (基于 Adoptium/Eclipse Temurin)
type JdkFetcher struct {
	cfg        *config.Config
	sm         *config.SettingsManager
	httpClient *http.Client
}

func NewJdkFetcher(cfg *config.Config, sm *config.SettingsManager) *JdkFetcher {
	return &JdkFetcher{cfg: cfg, sm: sm, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (f *JdkFetcher) SetHTTPClient(client *http.Client) { f.httpClient = client }
func (f *JdkFetcher) StripArchiveTopDir() bool           { return true }

func (f *JdkFetcher) useEndpoint(defaultURL string) string {
	if f.sm == nil {
		return defaultURL
	}
	custom := f.sm.Get().Endpoints[string(JDK)]
	if custom == "" {
		return defaultURL
	}
	return strings.Replace(defaultURL, "https://api.adoptium.net", custom, -1)
}

func (f *JdkFetcher) Type() SdkType {
	return JDK
}

func (f *JdkFetcher) GetBinDir() string {
	return "bin"
}

func (f *JdkFetcher) GetExtraEnvVars() map[string]string {
	return map[string]string{
		"JAVA_HOME": "", // 根目录
	}
}

func (f *JdkFetcher) VerifyCommand() (string, []string) {
	return "java", []string{"-version"}
}

type adoptiumRelease struct {
	Version struct {
		Semver string `json:"semver"`
		Major  int    `json:"major"`
	} `json:"version"`
	Binary struct {
		Package struct {
			Link string `json:"link"`
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"package"`
	} `json:"binary"`
	ReleaseName string `json:"release_name"`
}

func (f *JdkFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	// 获取可用主版本列表
	releasesResp, err := f.httpClient.Get(f.useEndpoint("https://api.adoptium.net/v3/info/available_releases"))
	if err != nil {
		return nil, fmt.Errorf("获取JDK可用版本列表失败: %w", err)
	}
	defer releasesResp.Body.Close()

	var releases struct {
		AvailableReleases    []int `json:"available_releases"`
		AvailableLTSReleases []int `json:"available_lts_releases"`
	}
	if err := json.NewDecoder(releasesResp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("解析JDK版本列表失败: %w", err)
	}

	ltsSet := make(map[int]bool)
	for _, v := range releases.AvailableLTSReleases {
		ltsSet[v] = true
	}

	os := f.osParam()
	if os == "" {
		return nil, fmt.Errorf("不支持当前操作系统")
	}

	var versions []VersionInfo
	for _, major := range releases.AvailableReleases {
		url := f.useEndpoint(fmt.Sprintf("https://api.adoptium.net/v3/assets/latest/%d/hotspot?architecture=x64&os=%s&image_type=jdk", major, os))
		resp, err := f.httpClient.Get(url)
		if err != nil {
			continue
		}

		var assets []adoptiumRelease
		if err := json.NewDecoder(resp.Body).Decode(&assets); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		for _, asset := range assets {
			if asset.Binary.Package.Link == "" {
				continue
			}
			versions = append(versions, VersionInfo{
				Version:     asset.Version.Semver,
				Major:       asset.Version.Major,
				DownloadURL: asset.Binary.Package.Link,
				FileName:    asset.Binary.Package.Name,
				IsLTS:       ltsSet[asset.Version.Major],
			})
		}
	}

	// 降序排列
	sort.Slice(versions, func(i, j int) bool {
		return CompareVersions(versions[i].Version, versions[j].Version) > 0
	})

	return versions, nil
}

func (f *JdkFetcher) GetDownloadURL(version string) (string, string, error) {
	os := f.osParam()
	if os == "" {
		return "", "", fmt.Errorf("不支持当前操作系统")
	}

	parts := strings.Split(version, ".")
	major, _ := strconv.Atoi(parts[0])

	url := f.useEndpoint(fmt.Sprintf("https://api.adoptium.net/v3/assets/latest/%d/hotspot?architecture=x64&os=%s&image_type=jdk", major, os))
	resp, err := f.httpClient.Get(url)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var assets []adoptiumRelease
	if err := json.NewDecoder(resp.Body).Decode(&assets); err != nil {
		return "", "", err
	}

	for _, asset := range assets {
		if strings.Contains(asset.Version.Semver, version) || asset.Version.Semver == version {
			return asset.Binary.Package.Link, asset.Binary.Package.Name, nil
		}
	}

	return "", "", fmt.Errorf("未找到JDK版本: %s", version)
}

func (f *JdkFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(JDK))
	active := f.cfg.GetActiveVersion(string(JDK))
	configured := active != ""

	return &SdkStatus{
		SdkType:           JDK,
		DisplayName:       SdkDisplayName(JDK),
		Configured:        configured,
		PathConfigured:    !configured && IsCommandAvailable("java"),
		CurrentVersion:    active,
		InstalledVersions: installed,
		InstallPath:       f.cfg.SdkDir(string(JDK)),
	}, nil
}

func (f *JdkFetcher) osParam() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	case "darwin":
		return "mac"
	default:
		return ""
	}
}
