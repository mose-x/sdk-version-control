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
