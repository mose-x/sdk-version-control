package sdk

// EndpointInfo describes an SDK download endpoint
type EndpointInfo struct {
	SdkType         string `json:"sdkType"`
	DisplayName     string `json:"displayName"`
	DefaultEndpoint string `json:"defaultEndpoint"`
}

// DefaultEndpoints returns the default endpoints of all SDKs (in order)
func DefaultEndpoints() []EndpointInfo {
	return []EndpointInfo{
		{string(NodeJS), SdkDisplayName(NodeJS), "https://nodejs.org"},
		{string(JDK), SdkDisplayName(JDK), "https://api.adoptium.net"},
		{string(Golang), SdkDisplayName(Golang), "https://go.dev"},
		{string(Python), SdkDisplayName(Python), "https://www.python.org"},
		{string(Rust), SdkDisplayName(Rust), "https://static.rust-lang.org"},
		{string(Ruby), SdkDisplayName(Ruby), "https://github.com"},
		{string(DotNet), SdkDisplayName(DotNet), "https://dotnetcli.blob.core.windows.net"},
		{string(PHP), SdkDisplayName(PHP), "https://windows.php.net"},
		{string(Perl), SdkDisplayName(Perl), "https://strawberryperl.com"},
		{string(Maven), SdkDisplayName(Maven), "https://archive.apache.org"},
		{string(Gradle), SdkDisplayName(Gradle), "https://services.gradle.org"},
		{string(Flutter), SdkDisplayName(Flutter), "https://storage.googleapis.com"},
		{string(Android), SdkDisplayName(Android), "https://dl.google.com"},
		{string(Dart), SdkDisplayName(Dart), "https://storage.googleapis.com"},
	}
}

// ChinaMirrors returns the built-in China mirror mappings for Google-backed
// download domains. This is data-only: nothing is rewritten automatically.
// Users can configure a mirror via the settings Endpoints map / useEndpoint,
// and a one-click switch UI is a follow-up.
//
// Keys are the original hostnames; values are the mirror hostnames. The
// Android (dl.google.com) mirror also carries a path prefix, since TUNA
// serves Android repository files under /android/repository rather than
// mirroring the host root.
func ChinaMirrors() map[string]string {
	return map[string]string{
		"storage.googleapis.com": "https://storage.flutter-io.cn",
		"dl.google.com":          "https://mirrors.tuna.tsinghua.edu.cn",
		"go.dev":                 "https://golang.google.cn",
	}
}
