package sdk

import "net/http"

// VersionFetcher 版本获取器接口，每种 SDK 实现此接口
type VersionFetcher interface {
	// FetchRemoteVersions 从远程获取所有可用版本（降序排列）
	FetchRemoteVersions() ([]VersionInfo, error)

	// GetLocalStatus 获取本地安装状态
	GetLocalStatus() (*SdkStatus, error)

	// GetDownloadURL 获取指定版本在当前 OS 的下载链接
	GetDownloadURL(version string) (string, string, error) // url, filename, error

	// GetBinDir 返回安装后的 bin 目录相对路径（相对于版本目录）
	GetBinDir() string

	// GetExtraEnvVars 返回需要额外设置的环境变量（如 JAVA_HOME, GOROOT）
	// key -> value 的映射，value 是相对于版本安装目录的路径
	GetExtraEnvVars() map[string]string

	// VerifyCommand 返回用于验证安装的命令和参数
	VerifyCommand() (string, []string)

	// SdkType 返回 SDK 类型
	Type() SdkType

	// SetHTTPClient 设置用于网络请求的 HTTP 客户端（支持代理）
	SetHTTPClient(client *http.Client)

	// StripArchiveTopDir 是否在解压后剥离单一的顶层目录
	// 大多数 SDK 的压缩包有顶层目录（如 jdk-17.0.19+10/），需要剥离
	// 但 Go、Perl、Android、Dart 等的 GetBinDir 已经包含了顶层目录名，不应剥离
	StripArchiveTopDir() bool
}
