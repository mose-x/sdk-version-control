package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const settingsFile = "settings.json"

// ProxySettings 代理配置
type ProxySettings struct {
	Enabled  bool   `json:"enabled"`  // 是否启用代理
	Mode     string `json:"mode"`     // "system" | "custom"
	URL      string `json:"url"`      // 自定义代理地址
	Protocol string `json:"protocol"` // "http" | "socks5"（自定义代理无 scheme 时使用）
}

// AppSettings 应用设置
type AppSettings struct {
	Theme           string            `json:"theme"`           // "system", "dark", "light"
	Language        string            `json:"language"`        // "zh", "en"
	Proxy           ProxySettings     `json:"proxy"`
	Endpoints       map[string]string `json:"endpoints"`       // sdkType -> custom endpoint URL
	InstallPath     string            `json:"installPath"`     // 自定义安装目录，空=默认 ~/.svc
	GithubMirror    string            `json:"githubMirror"`    // GitHub 镜像地址，空=不替换
	DownloadThreads int               `json:"downloadThreads"` // 下载线程数，0=默认4
}

// SettingsManager 管理应用设置
type SettingsManager struct {
	mu       sync.RWMutex
	svcDir   string
	settings AppSettings
}

// NewSettingsManager 创建设置管理器
func NewSettingsManager(svcDir string) *SettingsManager {
	sm := &SettingsManager{
		svcDir: svcDir,
		settings: AppSettings{
			Theme:           "system",
			Language:        "zh",
			Proxy:           ProxySettings{Enabled: false, Mode: "system"},
			DownloadThreads: 4,
		},
	}
	sm.load()
	return sm
}

func (s *SettingsManager) load() {
	path := filepath.Join(s.svcDir, settingsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &s.settings); err != nil {
		log.Printf("警告: 设置文件解析失败 (%s): %v，将使用默认设置", path, err)
	}
}

func (s *SettingsManager) save() error {
	data, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.svcDir, settingsFile), data, 0644)
}

// Get 获取当前设置
func (s *SettingsManager) Get() AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// Update 更新设置
func (s *SettingsManager) Update(settings AppSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = settings
	return s.save()
}
