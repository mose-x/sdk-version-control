package sdk

import (
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestDotNetBuildExt(t *testing.T) {
	f := &DotNetFetcher{}
	ext := f.buildExt()
	switch runtime.GOOS {
	case "windows":
		if ext != "zip" {
			t.Errorf("windows: expected 'zip', got '%s'", ext)
		}
	case "linux", "darwin":
		if ext != "tar.gz" {
			t.Errorf("%s: expected 'tar.gz', got '%s'", runtime.GOOS, ext)
		}
	}
}

func TestDotNetBuildURL(t *testing.T) {
	f := &DotNetFetcher{}
	url := f.buildURL("8.0.100")
	rid := f.buildRID()
	ext := f.buildExt()

	if !strings.Contains(url, rid) {
		t.Errorf("URL should contain RID '%s': %s", rid, url)
	}
	if !strings.HasSuffix(url, "."+ext) {
		t.Errorf("URL should end with '.%s' on %s, got: %s", ext, runtime.GOOS, url)
	}
	if !strings.Contains(url, "dotnet-sdk-8.0.100-") {
		t.Errorf("URL should contain version: %s", url)
	}
}

func TestDotNetBuildFileName(t *testing.T) {
	f := &DotNetFetcher{}
	name := f.buildFileName("8.0.100")
	rid := f.buildRID()
	ext := f.buildExt()

	expected := "dotnet-sdk-8.0.100-" + rid + "." + ext
	if name != expected {
		t.Errorf("expected '%s', got '%s'", expected, name)
	}
}

func TestDotnetRID(t *testing.T) {
	tests := []struct {
		goos, goarch, want string
	}{
		{"windows", "amd64", "win-x64"},
		{"windows", "arm64", "win-arm64"},
		{"linux", "amd64", "linux-x64"},
		{"linux", "arm64", "linux-arm64"},
		{"darwin", "amd64", "osx-x64"},
		{"darwin", "arm64", "osx-arm64"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"-"+tt.goarch, func(t *testing.T) {
			got := dotnetRID(tt.goos, tt.goarch)
			if got != tt.want {
				t.Errorf("dotnetRID(%q, %q): expected %q, got %q", tt.goos, tt.goarch, tt.want, got)
			}
		})
	}
}

func TestDotnetChannelMajor(t *testing.T) {
	tests := []struct {
		channel string
		want    int
	}{
		{"10.0", 10},
		{"9.0", 9},
		{"8.0", 8},
		{"3.1", 3},
		{"2.1", 2},
		{"11", 11}, // no dot
		{"", 0},
		{"abc", 0},
		{".5", 0},
	}
	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			if got := dotnetChannelMajor(tt.channel); got != tt.want {
				t.Errorf("dotnetChannelMajor(%q) = %d; want %d", tt.channel, got, tt.want)
			}
		})
	}
}

// TestDotnetIsLTS pins the official .NET LTS rule: even major = LTS. The old
// implementation compared support-phase to "lts", a value that never exists
// in the real release metadata (only preview/active/maintenance/eol), so
// IsLTS was always false. The recognition is now a pure function of the
// channel version.
func TestDotnetIsLTS(t *testing.T) {
	tests := []struct {
		channel string
		want    bool
	}{
		{"10.0", true},  // LTS
		{"9.0", false},  // STS
		{"8.0", true},   // LTS
		{"7.0", false},  // STS
		{"6.0", true},   // LTS
		{"5.0", false},  // STS
		{"3.1", false},  // odd major
		{"11.0", false}, // preview odd major
		{"", false},
		{"abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			if got := dotnetIsLTS(tt.channel); got != tt.want {
				t.Errorf("dotnetIsLTS(%q) = %v; want %v", tt.channel, got, tt.want)
			}
		})
	}
}

func TestDotnetIsPrereleaseSDK(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"11.0.100-preview.7.26381.103", true},
		{"10.0.100-rc.1", true},
		{"10.0.100-RC.2", true}, // case-insensitive
		{"10.0.400", false},
		{"9.0.317", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := dotnetIsPrereleaseSDK(tt.version); got != tt.want {
				t.Errorf("dotnetIsPrereleaseSDK(%q) = %v; want %v", tt.version, got, tt.want)
			}
		})
	}
}

// dotnetRoutingTransport serves canned bodies keyed by request URL and
// records every requested URL, so the index + per-channel fetch flow can be
// tested fully offline.
type dotnetRoutingTransport struct {
	mu        sync.Mutex
	bodies    map[string]string
	requested []string
}

func (t *dotnetRoutingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requested = append(t.requested, req.URL.String())
	t.mu.Unlock()
	body, ok := t.bodies[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// TestDotNetFetchRemoteVersionsLatestSDK drives the full fetch flow with
// fixture JSON: versions must come from each channel's latest-sdk field (NOT
// the index's latest-release runtime version), preview channels and empty
// latest-sdk channels are skipped, and IsLTS follows the even-major rule.
func TestDotNetFetchRemoteVersionsLatestSDK(t *testing.T) {
	const indexURL = "https://dotnetcli.blob.core.windows.net/dotnet/release-metadata/releases-index.json"
	indexBody := `{"releases-index":[
		{"channel-version":"11.0","latest-release":"11.0.0-preview.7","latest-sdk":"11.0.100-preview.7.26381.103","support-phase":"preview","releases.json":"https://example.test/11.0/releases.json"},
		{"channel-version":"10.0","latest-release":"10.0.11","latest-sdk":"10.0.400","support-phase":"active","releases.json":"https://example.test/10.0/releases.json"},
		{"channel-version":"9.0","latest-release":"9.0.19","latest-sdk":"9.0.317","support-phase":"maintenance","releases.json":"https://example.test/9.0/releases.json"},
		{"channel-version":"8.0","latest-release":"8.0.21","latest-sdk":"","support-phase":"maintenance","releases.json":"https://example.test/8.0/releases.json"},
		{"channel-version":"7.5","latest-release":"7.5.0","support-phase":"active","releases.json":""}
	]}`

	tr := &dotnetRoutingTransport{bodies: map[string]string{
		indexURL: indexBody,
		"https://example.test/11.0/releases.json": `{"channel-version":"11.0","latest-sdk":"11.0.100-preview.7.26381.103"}`,
		"https://example.test/10.0/releases.json": `{"channel-version":"10.0","latest-sdk":"10.0.400"}`,
		"https://example.test/9.0/releases.json":  `{"channel-version":"9.0","latest-sdk":"9.0.317"}`,
		"https://example.test/8.0/releases.json":  `{"channel-version":"8.0","latest-sdk":""}`,
	}}
	f := NewDotNetFetcher(nil, nil)
	f.SetHTTPClient(&http.Client{Transport: tr})

	versions, err := f.FetchRemoteVersions()
	if err != nil {
		t.Fatalf("FetchRemoteVersions: %v", err)
	}

	// Expect exactly the two stable channels with a usable latest-sdk:
	// 11.0 skipped (preview SDK), 8.0 skipped (empty latest-sdk),
	// 7.5 skipped (no releases.json link).
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d: %+v", len(versions), versions)
	}

	// Sorted descending: 10.0.400 first, then 9.0.317.
	if versions[0].Version != "10.0.400" {
		t.Errorf("versions[0] = %q; want 10.0.400 (the channel's latest-sdk, not the latest-release runtime version)", versions[0].Version)
	}
	if versions[1].Version != "9.0.317" {
		t.Errorf("versions[1] = %q; want 9.0.317", versions[1].Version)
	}

	// Download URL must embed the SDK version (the 404 bug used the runtime
	// version here, e.g. /Sdk/10.0.11/... which does not exist).
	if !strings.Contains(versions[0].DownloadURL, "/Sdk/10.0.400/dotnet-sdk-10.0.400-") {
		t.Errorf("DownloadURL not built from latest-sdk: %q", versions[0].DownloadURL)
	}
	if !strings.Contains(versions[0].FileName, "dotnet-sdk-10.0.400-") {
		t.Errorf("FileName not built from latest-sdk: %q", versions[0].FileName)
	}

	// LTS via even-major rule: 10 is even, 9 is odd.
	if !versions[0].IsLTS {
		t.Error("10.0 channel should be LTS (even major)")
	}
	if versions[1].IsLTS {
		t.Error("9.0 channel should not be LTS (odd major)")
	}
	if versions[0].Major != 10 || versions[1].Major != 9 {
		t.Errorf("Major fields wrong: %d, %d", versions[0].Major, versions[1].Major)
	}

	// The preview channel's releases.json was fetched but its result dropped;
	// the runtime versions (10.0.11/9.0.19) must never appear in the list.
	for _, v := range versions {
		if v.Version == "10.0.11" || v.Version == "9.0.19" || strings.Contains(v.Version, "preview") {
			t.Errorf("runtime/preview version leaked into the list: %q", v.Version)
		}
	}
}

// TestDotNetFetchRemoteVersionsAllChannelsSkipped verifies an error when no
// channel yields an installable SDK (guards against a silently empty list).
func TestDotNetFetchRemoteVersionsAllChannelsSkipped(t *testing.T) {
	const indexURL = "https://dotnetcli.blob.core.windows.net/dotnet/release-metadata/releases-index.json"
	indexBody := `{"releases-index":[
		{"channel-version":"11.0","support-phase":"preview","releases.json":"https://example.test/11.0/releases.json"}
	]}`
	tr := &dotnetRoutingTransport{bodies: map[string]string{
		indexURL: indexBody,
		"https://example.test/11.0/releases.json": `{"latest-sdk":"11.0.100-preview.7.26381.103"}`,
	}}
	f := NewDotNetFetcher(nil, nil)
	f.SetHTTPClient(&http.Client{Transport: tr})

	if _, err := f.FetchRemoteVersions(); err == nil {
		t.Fatal("expected an error when every channel is skipped")
	}
}
