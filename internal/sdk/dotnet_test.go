package sdk

import (
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
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

// mockDotnetTransport returns a canned JSON body for any request, so the
// support-phase recognition logic can be tested without real network calls.
type mockDotnetTransport struct {
	body string
}

func (t *mockDotnetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Header:     make(http.Header),
	}, nil
}

// TestDotNetSupportPhaseRecognition verifies that FetchRemoteVersions
// recognizes the "maintenance" support phase (the correct spelling used by
// the real .NET release-metadata API) as LTS. This guards against the
// "maintainance" typo regression (P4 fix): if the comparison string is
// reverted to the misspelled "maintainance", the maintenance-phase case
// here will fail.
func TestDotNetSupportPhaseRecognition(t *testing.T) {
	tests := []struct {
		name        string
		phase       string
		expectIsLTS bool
	}{
		{"lts phase", "lts", true},
		{"maintenance phase", "maintenance", true},
		{"typo maintainance not recognized", "maintainance", false},
		{"go-live phase", "go-live", false},
		{"eol phase", "eol", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(
				`{"releases-index":[{"channel-version":"8.0","latest-release":"8.0.100","support-phase":%q,"releases.json":""}]}`,
				tt.phase,
			)
			f := NewDotNetFetcher(nil, nil)
			f.SetHTTPClient(&http.Client{Transport: &mockDotnetTransport{body: body}})

			versions, err := f.FetchRemoteVersions()
			if err != nil {
				t.Fatalf("FetchRemoteVersions: %v", err)
			}
			if len(versions) != 1 {
				t.Fatalf("expected 1 version, got %d", len(versions))
			}
			if versions[0].IsLTS != tt.expectIsLTS {
				t.Errorf("support-phase %q: IsLTS=%v, want %v",
					tt.phase, versions[0].IsLTS, tt.expectIsLTS)
			}
		})
	}
}
