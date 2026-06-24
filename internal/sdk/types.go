package sdk

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// getSystemPath returns the system+user PATH (platform-dependent, implemented in path_*.go)
func getSystemPath() string {
	return getPlatformPath()
}

// GetSystemPath is the exported version for use by external packages
func GetSystemPath() string {
	return getPlatformPath()
}

// IsCommandAvailable checks whether a command is available in the system PATH
// Scans both the process PATH and the system PATH from the Windows registry
func IsCommandAvailable(cmd string) bool {
	if _, err := exec.LookPath(cmd); err == nil {
		return true
	}
	// Manually scan PATH directories (handles Wails process env inconsistency)
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

// SdkType defines the supported SDK types
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

// AllSdkTypes returns all supported SDK types (in display order)
func AllSdkTypes() []SdkType {
	return []SdkType{
		NodeJS, JDK, Golang, Python, Rust, Ruby, DotNet, PHP, Perl,
		Maven, Gradle,
		Flutter, Android, Dart,
	}
}

// SdkDisplayName returns the display name of an SDK
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

// SdkDirName returns the directory name of the SDK under ~/.svc/
func SdkDirName(t SdkType) string {
	return string(t)
}

// VersionInfo describes an available remote version
type VersionInfo struct {
	Version     string `json:"version"`
	Major       int    `json:"major"`
	DownloadURL string `json:"downloadUrl"`
	FileName    string `json:"fileName"`
	IsLTS       bool   `json:"isLts"`
	ReleaseDate string `json:"releaseDate"`
}

// SdkStatus describes the local installation status of an SDK
type SdkStatus struct {
	SdkType           SdkType  `json:"sdkType"`
	DisplayName       string   `json:"displayName"`
	Configured        bool     `json:"configured"`        // configured in .svc
	PathConfigured    bool     `json:"pathConfigured"`    // present in PATH but not in .svc
	PathVersion       string   `json:"pathVersion"`       // version detected in PATH
	CurrentVersion    string   `json:"currentVersion"`
	InstalledVersions []string `json:"installedVersions"`
	InstallPath       string   `json:"installPath"`
}

// InstallProgress is the install progress (pushed to the frontend via Wails Events)
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

// PackageManagerInfo describes a package manager
type PackageManagerInfo struct {
	Name      string  `json:"name"`      // npm, yarn, pnpm, composer
	Version   string  `json:"version"`   // current version
	Installed bool    `json:"installed"` // whether installed
	ParentSdk SdkType `json:"parentSdk"` // parent SDK type
}
