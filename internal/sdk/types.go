package sdk

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// getSystemPath 获取系统+用户 PATH（平台相关，实现在 path_*.go）
func getSystemPath() string {
	return getPlatformPath()
}

// GetSystemPath 导出版本，供外部包使用
func GetSystemPath() string {
	return getPlatformPath()
}

// IsCommandAvailable 检查命令是否在系统 PATH 中可用
// 同时扫描进程 PATH 和 Windows 注册表中的系统 PATH
func IsCommandAvailable(cmd string) bool {
	if _, err := exec.LookPath(cmd); err == nil {
		return true
	}
	// 手动扫描 PATH 目录（解决 Wails 进程环境不一致问题）
	pathEnv := getSystemPath()
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	exts := []string{""}
	if runtime.GOOS == "windows" {
		exts = []string{"", ".exe", ".cmd", ".bat"}
	}
	for _, dir := range strings.Split(pathEnv, sep) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		for _, ext := range exts {
			p := filepath.Join(dir, cmd+ext)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return true
			}
		}
	}
	return false
}

// SdkType 定义支持的 SDK 类型
type SdkType string

const (
	NodeJS SdkType = "nodejs"
	JDK    SdkType = "jdk"
	Golang SdkType = "go"
	Python SdkType = "python"
	Rust   SdkType = "rust"
	Ruby   SdkType = "ruby"
	DotNet SdkType = "dotnet"
	PHP    SdkType = "php"
	Perl   SdkType = "perl"
	Maven  SdkType = "maven"
	Gradle SdkType = "gradle"
	Flutter  SdkType = "flutter"
	Android  SdkType = "android"
	Dart     SdkType = "dart"
)

// AllSdkTypes 返回所有支持的 SDK 类型（按展示顺序）
func AllSdkTypes() []SdkType {
	return []SdkType{
		NodeJS, JDK, Golang, Python, Rust, Ruby, DotNet, PHP, Perl,
		Maven, Gradle,
		Flutter, Android, Dart,
	}
}

// SdkDisplayName 返回 SDK 的显示名称
func SdkDisplayName(t SdkType) string {
	switch t {
	case NodeJS:
		return "Node.js"
	case JDK:
		return "JDK"
	case Golang:
		return "Go"
	case Python:
		return "Python"
	case Rust:
		return "Rust"
	case Ruby:
		return "Ruby"
	case DotNet:
		return ".NET"
	case PHP:
		return "PHP"
	case Perl:
		return "Perl"
	case Maven:
		return "Maven"
	case Gradle:
		return "Gradle"
	case Flutter:
		return "Flutter"
	case Android:
		return "Android"
	case Dart:
		return "Dart"
	default:
		return string(t)
	}
}

// SdkDirName 返回 SDK 在 ~/.svc/ 下的目录名
func SdkDirName(t SdkType) string {
	return string(t)
}

// VersionInfo 描述一个可用的远程版本
type VersionInfo struct {
	Version     string `json:"version"`
	Major       int    `json:"major"`
	DownloadURL string `json:"downloadUrl"`
	FileName    string `json:"fileName"`
	IsLTS       bool   `json:"isLts"`
	ReleaseDate string `json:"releaseDate"`
}

// SdkStatus 描述某个 SDK 在本机的安装状态
type SdkStatus struct {
	SdkType           SdkType  `json:"sdkType"`
	DisplayName       string   `json:"displayName"`
	Configured        bool     `json:"configured"`        // 已在 .svc 中配置
	PathConfigured    bool     `json:"pathConfigured"`    // 在 PATH 中存在但不在 .svc
	PathVersion       string   `json:"pathVersion"`       // PATH 中检测到的版本号
	CurrentVersion    string   `json:"currentVersion"`
	InstalledVersions []string `json:"installedVersions"`
	InstallPath       string   `json:"installPath"`
}

// InstallProgress 安装进度（通过 Wails Events 推送到前端）
type InstallProgress struct {
	SdkType          SdkType `json:"sdkType"`
	Version          string  `json:"version"`
	Stage            string  `json:"stage"`   // "downloading" | "extracting" | "configuring_path" | "verifying" | "done" | "error"
	Percent          int     `json:"percent"` // 0-100
	Message          string  `json:"message"`
	DownloadedBytes  int64   `json:"downloadedBytes"`
	TotalBytes       int64   `json:"totalBytes"`
	SpeedBytesPerSec int64   `json:"speedBytesPerSec"`
	DownloadURL      string  `json:"downloadUrl"`
}

// PackageManagerInfo 包管理器信息
type PackageManagerInfo struct {
	Name      string  `json:"name"`      // npm, yarn, pnpm, composer
	Version   string  `json:"version"`   // 当前版本
	Installed bool    `json:"installed"` // 是否已安装
	ParentSdk SdkType `json:"parentSdk"` // 父SDK类型
}
