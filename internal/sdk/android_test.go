package sdk

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAndroidXMLHostOSParsing(t *testing.T) {
	xmlData := []byte(`<?xml version="1.0"?>
<sdk-repository>
  <remotePackage path="cmdline-tools;22.0">
    <revision>
      <major>22</major>
      <minor>0</minor>
      <micro>0</micro>
    </revision>
    <archives>
      <archive>
        <complete>
          <url>commandlinetools-linux-15859902_latest.zip</url>
          <size>155000000</size>
        </complete>
        <host-os>linux</host-os>
      </archive>
      <archive>
        <complete>
          <url>commandlinetools-win-15859902_latest.zip</url>
          <size>155000000</size>
        </complete>
        <host-os>windows</host-os>
      </archive>
    </archives>
  </remotePackage>
</sdk-repository>`)

	var repo androidRepository
	if err := xml.Unmarshal(xmlData, &repo); err != nil {
		t.Fatalf("failed to parse XML: %v", err)
	}

	if len(repo.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(repo.Packages))
	}

	pkg := repo.Packages[0]
	if pkg.Path != "cmdline-tools;22.0" {
		t.Errorf("package path: expected 'cmdline-tools;22.0', got '%s'", pkg.Path)
	}
	if pkg.Revision.Major != 22 {
		t.Errorf("major: expected 22, got %d", pkg.Revision.Major)
	}

	archives := pkg.Archives.Archive
	if len(archives) != 2 {
		t.Fatalf("expected 2 archives, got %d", len(archives))
	}

	if archives[0].OS != "linux" {
		t.Errorf("archive[0] OS: expected 'linux', got '%s' (host-os is a child element, not an attribute — the struct tag must not use ,attr)", archives[0].OS)
	}
	if archives[1].OS != "windows" {
		t.Errorf("archive[1] OS: expected 'windows', got '%s'", archives[1].OS)
	}

	if archives[0].URL != "commandlinetools-linux-15859902_latest.zip" {
		t.Errorf("archive[0] URL: expected linux zip, got '%s'", archives[0].URL)
	}
	if archives[1].URL != "commandlinetools-win-15859902_latest.zip" {
		t.Errorf("archive[1] URL: expected windows zip, got '%s'", archives[1].URL)
	}
}

func TestAndroidFetcher_GetDownloadURL_noFallback(t *testing.T) {
	// Clear any cached Android versions so GetDownloadURL performs a real
	// fetch instead of short-circuiting on the in-memory cache.
	globalVersionCache.mu.Lock()
	delete(globalVersionCache.entries, Android)
	globalVersionCache.mu.Unlock()

	// An HTTP client whose Transport always fails to dial, so
	// FetchRemoteVersions returns an error without touching the real network.
	// With f.sm == nil, useEndpoint returns the default dl.google.com URL.
	f := &AndroidFetcher{
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return nil, fmt.Errorf("simulated unreachable endpoint")
				},
			},
		},
	}

	_, _, err := f.GetDownloadURL("14.0")
	if err == nil {
		t.Fatal("expected GetDownloadURL to return an error when fetch fails (no silent fallback), got nil")
	}
	if !strings.Contains(err.Error(), "failed to fetch Android cmdline-tools versions") {
		t.Errorf("expected error to mention the fetch failure, got: %v", err)
	}
}

// TestAndroidHostArchKey pins the runtime.GOARCH -> host-arch mapping used
// to filter Android repository archives by CPU architecture (E3). The
// mapping is pure logic so every (goarch, want) pair is exercised regardless
// of the host's real arch.
func TestAndroidHostArchKey(t *testing.T) {
	tests := []struct {
		goarch string
		want   string
	}{
		{"arm64", "aarch64"},
		{"amd64", "x64"},
		{"386", ""},  // unsupported — caller falls back to legacy match
		{"mips", ""}, // unsupported
		{"", ""},     // unknown
	}
	for _, tt := range tests {
		t.Run(tt.goarch, func(t *testing.T) {
			got := androidHostArchKey(tt.goarch)
			if got != tt.want {
				t.Errorf("androidHostArchKey(%q) = %q; want %q", tt.goarch, got, tt.want)
			}
		})
	}
}

// TestAndroidXMLHostArchParsing verifies the host-arch XML field is parsed
// from the repository XML and that the (host-os, host-arch) pair is exposed
// on each archive entry (E3).
func TestAndroidXMLHostArchParsing(t *testing.T) {
	xmlData := []byte(`<?xml version="1.0"?>
<sdk-repository>
  <remotePackage path="cmdline-tools;22.0">
    <revision>
      <major>22</major>
      <minor>0</minor>
      <micro>0</micro>
    </revision>
    <archives>
      <archive>
        <complete>
          <url>commandlinetools-linux-15859902_latest.zip</url>
          <size>155000000</size>
        </complete>
        <host-os>linux</host-os>
        <host-arch>aarch64</host-arch>
      </archive>
      <archive>
        <complete>
          <url>commandlinetools-linux-15859902_x64.zip</url>
          <size>155000000</size>
        </complete>
        <host-os>linux</host-os>
        <host-arch>x64</host-arch>
      </archive>
      <archive>
        <complete>
          <url>commandlinetools-win-15859902_legacy.zip</url>
          <size>155000000</size>
        </complete>
        <host-os>windows</host-os>
      </archive>
    </archives>
  </remotePackage>
</sdk-repository>`)

	var repo androidRepository
	if err := xml.Unmarshal(xmlData, &repo); err != nil {
		t.Fatalf("failed to parse XML: %v", err)
	}
	if len(repo.Packages) != 1 || len(repo.Packages[0].Archives.Archive) != 3 {
		t.Fatalf("expected 1 package with 3 archives, got %+v", repo)
	}
	got := repo.Packages[0].Archives.Archive
	// linux aarch64
	if got[0].HostArch != "aarch64" {
		t.Errorf("archive[0] HostArch: want aarch64, got %q", got[0].HostArch)
	}
	// linux x64
	if got[1].HostArch != "x64" {
		t.Errorf("archive[1] HostArch: want x64, got %q", got[1].HostArch)
	}
	// windows legacy (no host-arch element)
	if got[2].HostArch != "" {
		t.Errorf("archive[2] HostArch: want empty (legacy), got %q", got[2].HostArch)
	}
}

// TestAndroidFetcher_PrefersArchMatchingArchive uses a stub RoundTripper that
// serves a fixed repository XML (with both an aarch64 and an x64 linux
// archive) for any request, then verifies the fetcher selects the archive
// matching the runtime GOARCH. Uses a RoundTripper (not a real listener)
// because the fetcher hardcodes https://dl.google.com URLs and a plain HTTP
// test server would fail the TLS handshake.
func TestAndroidFetcher_PrefersArchMatchingArchive(t *testing.T) {
	// Use the current platform's OS key so the fetcher matches archives.
	osKey := "windows"
	if runtime.GOOS == "linux" {
		osKey = "linux"
	}
	xmlBody := fmt.Sprintf(`<?xml version="1.0"?>
<sdk-repository>
  <remotePackage path="cmdline-tools;22.0">
    <revision><major>22</major><minor>0</minor><micro>0</micro></revision>
    <archives>
      <archive>
        <complete><url>cmdline-aarch64.zip</url><size>1</size></complete>
        <host-os>%s</host-os>
        <host-arch>aarch64</host-arch>
      </archive>
      <archive>
        <complete><url>cmdline-x64.zip</url><size>1</size></complete>
        <host-os>%s</host-os>
        <host-arch>x64</host-arch>
      </archive>
    </archives>
  </remotePackage>
</sdk-repository>`, osKey, osKey)

	f := &AndroidFetcher{
		httpClient: &http.Client{
			Timeout:   5 * time.Second,
			Transport: &stubXMLRoundTripper{body: xmlBody},
		},
	}

	versions, err := f.FetchRemoteVersions()
	if err != nil {
		t.Fatalf("FetchRemoteVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d: %+v", len(versions), versions)
	}
	want := androidHostArchKey(runtime.GOARCH)
	// On an unknown arch the fetcher falls back to whichever appears first;
	// skip the strict URL check in that case.
	if want == "" {
		return
	}
	gotName := versions[0].FileName
	wantName := "cmdline-" + map[string]string{"aarch64": "aarch64", "x64": "x64"}[want] + ".zip"
	if gotName != wantName {
		t.Errorf("FetchRemoteVersions selected %q on GOARCH=%s; want %q (matching host-arch %s)", gotName, runtime.GOARCH, wantName, want)
	}
}

// stubXMLRoundTripper is a RoundTripper that returns a fixed XML body for any
// request. Used by tests that need to feed the fetcher a synthetic XML payload
// without standing up a real HTTP(S) server (the fetcher hardcodes https URLs
// so a plain-HTTP test server would fail the TLS handshake).
type stubXMLRoundTripper struct{ body string }

func (s *stubXMLRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/xml"}},
		Body:       io.NopCloser(strings.NewReader(s.body)),
	}, nil
}
