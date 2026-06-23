package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const (
	svcDirName   = ".svc"
	configFile   = "config.json"
	tmpDirName   = "tmp"
	envShFile    = "env.sh"
)

// Config 管理 ~/.svc 目录和应用配置
type Config struct {
	mu      sync.RWMutex
	homeDir string
	svcDir  string
	data    *ConfigData
}

// ConfigData 持久化到 config.json 的数据
type ConfigData struct {
	ActiveVersions map[string]string `json:"activeVersions"` // sdkType -> version
}

// NewConfig 创建配置管理器
func NewConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	svcDir := filepath.Join(home, svcDirName)
	c := &Config{
		homeDir: home,
		svcDir:  svcDir,
		data: &ConfigData{
			ActiveVersions: make(map[string]string),
		},
	}
	if err := c.init(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) init() error {
	// 创建 ~/.svc 目录
	dirs := []string{
		c.svcDir,
		filepath.Join(c.svcDir, tmpDirName),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	// 创建各 SDK 子目录
	for _, name := range []string{
		"nodejs", "jdk", "go", "python", "rust", "ruby", "dotnet", "php", "perl",
		"maven", "gradle",
		"flutter", "android", "dart",
	} {
		if err := os.MkdirAll(filepath.Join(c.svcDir, name), 0755); err != nil {
			return err
		}
	}
	// 加载配置文件
	return c.load()
}

func (c *Config) load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	path := filepath.Join(c.svcDir, configFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c.save()
		}
		return err
	}
	return json.Unmarshal(data, c.data)
}

func (c *Config) save() error {
	data, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.svcDir, configFile), data, 0644)
}

// HomeDir 返回用户主目录
func (c *Config) HomeDir() string {
	return c.homeDir
}

// SvcDir 返回 ~/.svc 路径
func (c *Config) SvcDir() string {
	return c.svcDir
}

// SetSvcDir 设置自定义安装目录
func (c *Config) SetSvcDir(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.svcDir = dir
}

// DefaultSvcDir 返回默认安装目录
func DefaultSvcDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home, err = os.Getwd()
		if err != nil {
			return svcDirName
		}
	}
	return filepath.Join(home, svcDirName)
}

// TmpDir 返回临时下载目录
func (c *Config) TmpDir() string {
	return filepath.Join(c.svcDir, tmpDirName)
}

// SdkDir 返回指定 SDK 的存储目录
func (c *Config) SdkDir(sdkType string) string {
	return filepath.Join(c.svcDir, sdkType)
}

// SdkVersionDir 返回指定 SDK 版本的安装目录
func (c *Config) SdkVersionDir(sdkType string, version string) string {
	return filepath.Join(c.SdkDir(sdkType), version)
}

// GetActiveVersion 获取当前激活的版本
func (c *Config) GetActiveVersion(sdkType string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data.ActiveVersions[sdkType]
}

// SetActiveVersion 设置当前激活版本
func (c *Config) SetActiveVersion(sdkType string, version string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data.ActiveVersions[sdkType] = version
	return c.save()
}

// GetInstalledVersions 获取本地已安装的所有版本
func (c *Config) GetInstalledVersions(sdkType string) []string {
	dir := c.SdkDir(sdkType)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("读取SDK目录失败 (%s): %v", dir, err)
		}
		return nil
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	return versions
}

// EnvShPath 返回 env.sh 文件路径（Linux/macOS 用）
func (c *Config) EnvShPath() string {
	return filepath.Join(c.svcDir, envShFile)
}

// IsWindows 判断当前是否为 Windows
func IsWindows() bool {
	return runtime.GOOS == "windows"
}

// IsDarwin 判断当前是否为 macOS
func IsDarwin() bool {
	return runtime.GOOS == "darwin"
}

// IsLinux 判断当前是否为 Linux
func IsLinux() bool {
	return runtime.GOOS == "linux"
}
