package sdk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"sdk_version_control/internal/config"
)

// GradleFetcher Gradle 版本获取器
type GradleFetcher struct {
	cfg        *config.Config
	sm         *config.SettingsManager
	httpClient *http.Client
}

func NewGradleFetcher(cfg *config.Config, sm *config.SettingsManager) *GradleFetcher {
	return &GradleFetcher{cfg: cfg, sm: sm, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (f *GradleFetcher) SetHTTPClient(client *http.Client) { f.httpClient = client }
func (f *GradleFetcher) StripArchiveTopDir() bool           { return true }

func (f *GradleFetcher) useEndpoint(defaultURL string) string {
	if f.sm == nil {
		return defaultURL
	}
	custom := f.sm.Get().Endpoints[string(Gradle)]
	if custom == "" {
		return defaultURL
	}
	return strings.Replace(defaultURL, "https://services.gradle.org", custom, -1)
}

func (f *GradleFetcher) Type() SdkType {
	return Gradle
}

func (f *GradleFetcher) GetBinDir() string {
	return "bin"
}

func (f *GradleFetcher) GetExtraEnvVars() map[string]string {
	return map[string]string{
		"GRADLE_HOME": "", // 根目录
	}
}

func (f *GradleFetcher) VerifyCommand() (string, []string) {
	return "gradle", []string{"--version"}
}

type gradleVersionJSON struct {
	Version     string `json:"version"`
	DownloadURL string `json:"downloadUrl"`
	Released    bool   `json:"released"`
	Snapshot    bool   `json:"snapshot"`
	Current     bool   `json:"current"`
}

func (f *GradleFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	resp, err := f.httpClient.Get(f.useEndpoint("https://services.gradle.org/versions/all"))
	if err != nil {
		return nil, fmt.Errorf("获取Gradle版本列表失败: %w", err)
	}
	defer resp.Body.Close()

	var raw []gradleVersionJSON
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("解析Gradle版本数据失败: %w", err)
	}

	var versions []VersionInfo
	for _, v := range raw {
		if !v.Released || v.Snapshot {
			continue
		}
		parts := strings.Split(v.Version, ".")
		major, _ := strconv.Atoi(parts[0])

		versions = append(versions, VersionInfo{
			Version:     v.Version,
			Major:       major,
			DownloadURL: v.DownloadURL,
			FileName:    fmt.Sprintf("gradle-%s-bin.zip", v.Version),
		})
	}

	sort.Slice(versions, func(i, j int) bool {
		return CompareVersions(versions[i].Version, versions[j].Version) > 0
	})

	return versions, nil
}

func (f *GradleFetcher) GetDownloadURL(version string) (string, string, error) {
	resp, err := f.httpClient.Get(f.useEndpoint("https://services.gradle.org/versions/all"))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var raw []gradleVersionJSON
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return "", "", err
	}

	for _, v := range raw {
		if v.Version == version {
			dlURL := v.DownloadURL
			// 应用自定义端点
			if f.sm != nil {
				if custom := f.sm.Get().Endpoints[string(Gradle)]; custom != "" {
					dlURL = strings.Replace(dlURL, "https://services.gradle.org", custom, -1)
				}
			}
			return dlURL, fmt.Sprintf("gradle-%s-bin.zip", version), nil
		}
	}

	return "", "", fmt.Errorf("未找到Gradle版本: %s", version)
}

func (f *GradleFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(Gradle))
	active := f.cfg.GetActiveVersion(string(Gradle))
	configured := active != ""

	return &SdkStatus{
		SdkType:           Gradle,
		DisplayName:       SdkDisplayName(Gradle),
		Configured:        configured,
		PathConfigured:    !configured && IsCommandAvailable("gradle"),
		CurrentVersion:    active,
		InstalledVersions: installed,
		InstallPath:       f.cfg.SdkDir(string(Gradle)),
	}, nil
}
