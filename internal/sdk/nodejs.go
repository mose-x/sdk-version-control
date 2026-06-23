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

// NodejsFetcher Node.js 版本获取器
type NodejsFetcher struct {
	cfg        *config.Config
	sm         *config.SettingsManager
	httpClient *http.Client
}

// nodeVersionJSON 对应 nodejs.org/dist/index.json 的结构
type nodeVersionJSON struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	LTS     any    `json:"lts"` // false 或 字符串如 "Iron"
	Major   int    `json:"-"`   // 从 Version 解析
}

func NewNodejsFetcher(cfg *config.Config, sm *config.SettingsManager) *NodejsFetcher {
	return &NodejsFetcher{cfg: cfg, sm: sm, httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (f *NodejsFetcher) SetHTTPClient(client *http.Client) { f.httpClient = client }
func (f *NodejsFetcher) StripArchiveTopDir() bool           { return true }

func (f *NodejsFetcher) useEndpoint(defaultURL string) string {
	if f.sm == nil {
		return defaultURL
	}
	custom := f.sm.Get().Endpoints[string(NodeJS)]
	if custom == "" {
		return defaultURL
	}
	return strings.Replace(defaultURL, "https://nodejs.org", custom, -1)
}

func (f *NodejsFetcher) Type() SdkType {
	return NodeJS
}

func (f *NodejsFetcher) GetBinDir() string {
	if config.IsWindows() {
		return "" // Windows 下 node.exe 直接在根目录
	}
	return "bin"
}

func (f *NodejsFetcher) GetExtraEnvVars() map[string]string {
	return nil
}

func (f *NodejsFetcher) VerifyCommand() (string, []string) {
	return "node", []string{"--version"}
}

func (f *NodejsFetcher) FetchRemoteVersions() ([]VersionInfo, error) {
	resp, err := f.httpClient.Get(f.useEndpoint("https://nodejs.org/dist/index.json"))
	if err != nil {
		return nil, fmt.Errorf("获取Node.js版本列表失败: %w", err)
	}
	defer resp.Body.Close()

	var raw []nodeVersionJSON
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("解析Node.js版本数据失败: %w", err)
	}

	var versions []VersionInfo
	for _, v := range raw {
		ver := strings.TrimPrefix(v.Version, "v")
		parts := strings.Split(ver, ".")
		if len(parts) < 1 {
			continue
		}
		major, _ := strconv.Atoi(parts[0])
		if major < 16 { // 过滤掉太旧的版本
			continue
		}

		isLTS := false
		if v.LTS != nil {
			if s, ok := v.LTS.(string); ok && s != "" {
				isLTS = true
			}
		}

		url, fileName := f.buildDownloadURL(ver)
		if url == "" {
			continue
		}

		versions = append(versions, VersionInfo{
			Version:     ver,
			Major:       major,
			DownloadURL: url,
			FileName:    fileName,
			IsLTS:       isLTS,
			ReleaseDate: v.Date,
		})
	}

	// 降序排列
	sort.Slice(versions, func(i, j int) bool {
		return CompareVersions(versions[i].Version, versions[j].Version) > 0
	})

	return versions, nil
}

func (f *NodejsFetcher) buildDownloadURL(version string) (string, string) {
	os := runtime.GOOS
	arch := runtime.GOARCH

	var suffix string
	switch {
	case os == "windows" && arch == "amd64":
		suffix = "win-x64.zip"
	case os == "linux" && arch == "amd64":
		suffix = "linux-x64.tar.xz"
	case os == "darwin" && arch == "arm64":
		suffix = "darwin-arm64.tar.gz"
	case os == "darwin" && arch == "amd64":
		suffix = "darwin-x64.tar.gz"
	default:
		return "", ""
	}

	fileName := fmt.Sprintf("node-v%s-%s", version, suffix)
	url := f.useEndpoint(fmt.Sprintf("https://nodejs.org/dist/v%s/%s", version, fileName))
	return url, fileName
}

func (f *NodejsFetcher) GetLocalStatus() (*SdkStatus, error) {
	installed := f.cfg.GetInstalledVersions(string(NodeJS))
	active := f.cfg.GetActiveVersion(string(NodeJS))
	configured := active != ""

	return &SdkStatus{
		SdkType:           NodeJS,
		DisplayName:       SdkDisplayName(NodeJS),
		Configured:        configured,
		PathConfigured:    !configured && IsCommandAvailable("node"),
		CurrentVersion:    active,
		InstalledVersions: installed,
		InstallPath:       f.cfg.SdkDir(string(NodeJS)),
	}, nil
}

func (f *NodejsFetcher) GetDownloadURL(version string) (string, string, error) {
	url, fileName := f.buildDownloadURL(version)
	if url == "" {
		return "", "", fmt.Errorf("不支持当前平台")
	}
	return url, fileName, nil
}

// CompareVersions 比较两个语义化版本号，返回 -1/0/1
func CompareVersions(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")
	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}
	for i := 0; i < maxLen; i++ {
		var numA, numB int
		if i < len(partsA) {
			numA, _ = strconv.Atoi(partsA[i])
		}
		if i < len(partsB) {
			numB, _ = strconv.Atoi(partsB[i])
		}
		if numA < numB {
			return -1
		}
		if numA > numB {
			return 1
		}
	}
	return 0
}
