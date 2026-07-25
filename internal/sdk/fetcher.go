package sdk

import "net/http"

// VersionFetcher is the version fetcher interface; each SDK implements it
type VersionFetcher interface {
	// FetchRemoteVersions fetches all available versions from the remote (descending order)
	FetchRemoteVersions() ([]VersionInfo, error)
	// GetLocalStatus returns the local installation status
	GetLocalStatus() (*SdkStatus, error)
	// GetDownloadURL returns the download link for the specified version on the current OS
	GetDownloadURL(version string) (string, string, error)
	// GetBinDirs returns the relative paths of the bin directories after install
	// (relative to the version directory). Most SDKs have a single bin dir, but
	// some (e.g. Rust tarball) ship commands across multiple dirs (cargo/bin,
	// rustc/bin, rustfmt-preview/bin). Order matters: earlier dirs win on name
	// conflicts. An empty string means the version root itself is the bin dir.
	GetBinDirs() []string
	// GetExtraEnvVars returns the additional env vars that need to be set (e.g. JAVA_HOME, GOROOT)
	// key -> value mapping; value is the path relative to the version install directory
	GetExtraEnvVars() map[string]string
	// VerifyCommand returns the command and arguments used to verify the installation
	VerifyCommand() (string, []string)
	// Type returns the SDK type
	Type() SdkType
	// SetHTTPClient sets the HTTP client used for network requests (supports proxies)
	SetHTTPClient(client *http.Client)
	// StripArchiveTopDir indicates whether to strip a single top-level directory after extraction
	// Most SDK archives have a top-level directory (e.g. jdk-17.0.19+10/) that needs stripping
	// But Go, Perl, Android, Dart etc. have GetBinDir that already includes the top-level dir name, so they should not be stripped
	StripArchiveTopDir() bool
}
