package sdk

import (
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"
)

// flutterFixtureJSON mirrors the real releases_{platform}.json structure
// (verified against storage.googleapis.com): base_url + releases[] with
// hash/channel/version/dart_sdk_arch/release_date/archive/sha256 fields.
// It mixes channels, duplicates a version across x64/arm64 (as the macOS
// file does), and spans several dates to exercise filtering, dedup, arch
// preference and sorting.
const flutterFixtureJSON = `{
  "base_url": "https://storage.googleapis.com/flutter_infra_release/releases",
  "current_release": {
    "beta": "b-hash",
    "stable": "s-hash"
  },
  "releases": [
    {
      "hash": "b-hash",
      "channel": "beta",
      "version": "3.48.0-0.2.pre",
      "dart_sdk_version": "3.14.0 (build 3.14.0-95.2.beta)",
      "dart_sdk_arch": "x64",
      "release_date": "2026-08-19T23:06:18.388479Z",
      "archive": "beta/windows/flutter_windows_3.48.0-0.2.pre-beta.zip",
      "sha256": "beta-sum"
    },
    {
      "hash": "d-hash",
      "channel": "dev",
      "version": "3.49.0-0.0.pre",
      "dart_sdk_arch": "x64",
      "release_date": "2026-08-20T00:00:00Z",
      "archive": "dev/windows/flutter_windows_3.49.0-0.0.pre-dev.zip",
      "sha256": "dev-sum"
    },
    {
      "hash": "s1-hash",
      "channel": "stable",
      "version": "3.47.1",
      "dart_sdk_version": "3.13.1",
      "dart_sdk_arch": "x64",
      "release_date": "2026-08-19T22:09:02.684964Z",
      "archive": "stable/windows/flutter_windows_3.47.1-stable.zip",
      "sha256": "sum-3471-x64"
    },
    {
      "hash": "s2-hash",
      "channel": "stable",
      "version": "3.47.1",
      "dart_sdk_version": "3.13.1",
      "dart_sdk_arch": "arm64",
      "release_date": "2026-08-19T22:01:23.784964Z",
      "archive": "stable/macos/flutter_macos_arm64_3.47.1-stable.zip",
      "sha256": "sum-3471-arm64"
    },
    {
      "hash": "s3-hash",
      "channel": "stable",
      "version": "3.44.8",
      "dart_sdk_version": "3.10.0",
      "dart_sdk_arch": "x64",
      "release_date": "2026-07-23T23:18:37.123456Z",
      "archive": "stable/windows/flutter_windows_3.44.8-stable.zip",
      "sha256": "sum-3448-x64"
    }
  ]
}`

// flutterStubTransport serves a fixed body (with a configurable status) for
// every request, so fetcher tests run fully offline.
type flutterStubTransport struct {
	body   string
	status int
}

func (s *flutterStubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(s.body)),
	}, nil
}

// clearFlutterCacheEntry removes any in-memory Flutter cache entry so
// GetDownloadURL tests never short-circuit on cache state left by other tests.
func clearFlutterCacheEntry(t *testing.T) {
	t.Helper()
	globalVersionCache.mu.Lock()
	delete(globalVersionCache.entries, Flutter)
	globalVersionCache.mu.Unlock()
}

func TestIsStableFlutterTag(t *testing.T) {
	stable := []string{"3.44.8", "v3.24.5", "2.10.0", "v1.22.6"}
	for _, tag := range stable {
		if !isStableFlutterTag(tag) {
			t.Errorf("expected '%s' to be stable (true), got false", tag)
		}
	}

	unstable := []string{"3.47.0-0.3.pre", "3.17.0-0.1.pre", "v3.47.0-beta", "3.9.0-dev", "2.15.0-dev.0"}
	for _, tag := range unstable {
		if isStableFlutterTag(tag) {
			t.Errorf("expected '%s' to be unstable (false), got true", tag)
		}
	}
}

func TestFlutterBuildExt(t *testing.T) {
	f := &FlutterFetcher{}
	ext := f.buildExt()
	switch runtime.GOOS {
	case "linux":
		if ext != "tar.xz" {
			t.Errorf("linux: expected 'tar.xz', got '%s'", ext)
		}
	case "windows", "darwin":
		if ext != "zip" {
			t.Errorf("%s: expected 'zip', got '%s'", runtime.GOOS, ext)
		}
	}
}

// TestFlutterGetDownloadURL exercises the pattern-based fallback (metadata
// fetch fails -> known naming pattern), which must match the historical URL
// shape on the current host.
func TestFlutterGetDownloadURL(t *testing.T) {
	clearFlutterCacheEntry(t)
	// A transport that always answers 500 forces the fallback path without
	// touching the network.
	f := &FlutterFetcher{httpClient: &http.Client{Timeout: 5 * time.Second, Transport: &flutterStubTransport{body: "", status: http.StatusInternalServerError}}}
	url, fileName, err := f.GetDownloadURL("3.44.8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ext := f.buildExt()
	osName := f.buildOSName()

	if !strings.HasSuffix(url, "."+ext) {
		t.Errorf("URL should end with '.%s', got: %s", ext, url)
	}
	if !strings.Contains(url, osName) {
		t.Errorf("URL should contain OS name '%s', got: %s", osName, url)
	}
	if !strings.Contains(fileName, "3.44.8") {
		t.Errorf("fileName should contain version '3.44.8', got: %s", fileName)
	}
	if !strings.HasSuffix(fileName, "-stable."+ext) {
		t.Errorf("fileName should end with '-stable.%s', got: %s", ext, fileName)
	}
	// On darwin/arm64 the URL + fileName must include the _arm64 suffix
	// (C3 fix). A bare "macos" check would false-pass if the suffix were
	// missing, so explicitly assert it.
	suffix := flutterArchSuffix(runtime.GOOS, runtime.GOARCH)
	if suffix != "" {
		if !strings.Contains(url, suffix) {
			t.Errorf("URL should contain arch suffix %q, got: %s", suffix, url)
		}
		if !strings.Contains(fileName, suffix) {
			t.Errorf("fileName should contain arch suffix %q, got: %s", suffix, fileName)
		}
	}
}

func TestFlutterArchSuffix(t *testing.T) {
	tests := []struct {
		goos, goarch, want string
	}{
		{"darwin", "amd64", ""},
		{"darwin", "arm64", "_arm64"},
		{"linux", "amd64", ""},
		{"windows", "arm64", ""},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"-"+tt.goarch, func(t *testing.T) {
			got := flutterArchSuffix(tt.goos, tt.goarch)
			if got != tt.want {
				t.Errorf("flutterArchSuffix(%q, %q): expected %q, got %q", tt.goos, tt.goarch, tt.want, got)
			}
		})
	}
}

// TestFlutterReleasesJSONURL pins the per-platform metadata URL selection:
// Flutter stable releases are git tags only (the GitHub Releases API is
// always empty), so this JSON is the sole version source.
func TestFlutterReleasesJSONURL(t *testing.T) {
	tests := []struct {
		goos, want string
	}{
		{"windows", "https://storage.googleapis.com/flutter_infra_release/releases/releases_windows.json"},
		{"darwin", "https://storage.googleapis.com/flutter_infra_release/releases/releases_macos.json"},
		{"linux", "https://storage.googleapis.com/flutter_infra_release/releases/releases_linux.json"},
		{"freebsd", "https://storage.googleapis.com/flutter_infra_release/releases/releases_windows.json"},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			if got := flutterReleasesJSONURL(tt.goos); got != tt.want {
				t.Errorf("flutterReleasesJSONURL(%q) = %q; want %q", tt.goos, got, tt.want)
			}
		})
	}
}

func TestFlutterDesiredArch(t *testing.T) {
	tests := []struct {
		goos, goarch, want string
	}{
		{"darwin", "arm64", "arm64"},
		{"darwin", "amd64", "x64"},
		{"windows", "arm64", "x64"}, // no arm64 builds for Windows
		{"linux", "arm64", "x64"},   // nor for Linux
		{"linux", "amd64", "x64"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"-"+tt.goarch, func(t *testing.T) {
			if got := flutterDesiredArch(tt.goos, tt.goarch); got != tt.want {
				t.Errorf("flutterDesiredArch(%q, %q) = %q; want %q", tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

func parseFlutterFixture(t *testing.T) *flutterReleasesDoc {
	t.Helper()
	var doc flutterReleasesDoc
	if err := json.Unmarshal([]byte(flutterFixtureJSON), &doc); err != nil {
		t.Fatalf("fixture JSON invalid: %v", err)
	}
	return &doc
}

// TestFlutterStableVersions verifies parsing/filtering/dedup/sort of the
// official releases JSON: only stable-channel entries survive, duplicate
// versions collapse (arch-matching entry wins), URLs are base_url+"/"+archive
// and the list is sorted semantically descending.
func TestFlutterStableVersions(t *testing.T) {
	doc := parseFlutterFixture(t)

	// x64 host: the 3.47.1 duplicate must resolve to the x64 entry.
	versions := flutterStableVersions(doc, "windows", "amd64")
	if len(versions) != 2 {
		t.Fatalf("expected 2 stable versions (dedup applied), got %d: %+v", len(versions), versions)
	}
	if versions[0].Version != "3.47.1" || versions[1].Version != "3.44.8" {
		t.Errorf("expected descending order [3.47.1, 3.44.8], got [%s, %s]", versions[0].Version, versions[1].Version)
	}
	wantURL := "https://storage.googleapis.com/flutter_infra_release/releases/stable/windows/flutter_windows_3.47.1-stable.zip"
	if versions[0].DownloadURL != wantURL {
		t.Errorf("DownloadURL = %q; want %q (base_url + \"/\" + archive)", versions[0].DownloadURL, wantURL)
	}
	if versions[0].FileName != "flutter_windows_3.47.1-stable.zip" {
		t.Errorf("FileName = %q; want flutter_windows_3.47.1-stable.zip", versions[0].FileName)
	}
	if versions[0].ReleaseDate != "2026-08-19" {
		t.Errorf("ReleaseDate = %q; want 2026-08-19 (RFC3339 with fractional seconds)", versions[0].ReleaseDate)
	}
	if versions[0].Major != 3 {
		t.Errorf("Major = %d; want 3", versions[0].Major)
	}

	// arm64 macOS host: the same version must resolve to the arm64 archive.
	versions = flutterStableVersions(doc, "darwin", "arm64")
	if len(versions) != 2 {
		t.Fatalf("expected 2 stable versions on darwin/arm64, got %d", len(versions))
	}
	wantURL = "https://storage.googleapis.com/flutter_infra_release/releases/stable/macos/flutter_macos_arm64_3.47.1-stable.zip"
	if versions[0].DownloadURL != wantURL {
		t.Errorf("darwin/arm64 DownloadURL = %q; want %q", versions[0].DownloadURL, wantURL)
	}
}

// TestFlutterStableVersionsNilAndEmpty guards degenerate inputs.
func TestFlutterStableVersionsNilAndEmpty(t *testing.T) {
	if got := flutterStableVersions(nil, "windows", "amd64"); got != nil {
		t.Errorf("nil doc: expected nil, got %+v", got)
	}
	empty := &flutterReleasesDoc{BaseURL: "https://example.invalid"}
	if got := flutterStableVersions(empty, "windows", "amd64"); len(got) != 0 {
		t.Errorf("empty releases: expected empty list, got %+v", got)
	}
}

func TestFlutterLookupStableArchive(t *testing.T) {
	doc := parseFlutterFixture(t)

	// Known stable version, x64 host.
	url, name, ok := flutterLookupStableArchive(doc, "3.44.8", "windows", "amd64")
	if !ok {
		t.Fatal("expected to find 3.44.8")
	}
	if url != "https://storage.googleapis.com/flutter_infra_release/releases/stable/windows/flutter_windows_3.44.8-stable.zip" {
		t.Errorf("unexpected URL: %q", url)
	}
	if name != "flutter_windows_3.44.8-stable.zip" {
		t.Errorf("unexpected name: %q", name)
	}

	// arm64 macOS host picks the arm64 entry for the duplicated version.
	url, _, ok = flutterLookupStableArchive(doc, "3.47.1", "darwin", "arm64")
	if !ok || !strings.Contains(url, "flutter_macos_arm64_3.47.1-stable.zip") {
		t.Errorf("darwin/arm64 lookup = %q (ok=%v); want the arm64 archive", url, ok)
	}

	// Beta-only version must not resolve (stable channel only).
	if _, _, ok := flutterLookupStableArchive(doc, "3.48.0-0.2.pre", "windows", "amd64"); ok {
		t.Error("beta version must not resolve via the stable lookup")
	}
	// Unknown version.
	if _, _, ok := flutterLookupStableArchive(doc, "0.0.1", "windows", "amd64"); ok {
		t.Error("unknown version must not resolve")
	}
}

func TestFlutterLookupChecksum(t *testing.T) {
	doc := parseFlutterFixture(t)
	if got := flutterLookupChecksum(doc, "3.44.8", "windows", "amd64"); got != "sum-3448-x64" {
		t.Errorf("checksum = %q; want sum-3448-x64", got)
	}
	// Arch preference for the duplicated version.
	if got := flutterLookupChecksum(doc, "3.47.1", "darwin", "arm64"); got != "sum-3471-arm64" {
		t.Errorf("checksum = %q; want sum-3471-arm64", got)
	}
	if got := flutterLookupChecksum(doc, "3.47.1", "windows", "amd64"); got != "sum-3471-x64" {
		t.Errorf("checksum = %q; want sum-3471-x64", got)
	}
	// Unknown version -> no checksum (skip verification), not an error.
	if got := flutterLookupChecksum(doc, "0.0.1", "windows", "amd64"); got != "" {
		t.Errorf("checksum for unknown version = %q; want empty", got)
	}
}

// TestFlutterFetchRemoteVersions runs the full fetcher against the fixture
// (offline stub transport) and asserts the resulting version list.
func TestFlutterFetchRemoteVersions(t *testing.T) {
	clearFlutterCacheEntry(t)
	f := &FlutterFetcher{httpClient: &http.Client{Timeout: 5 * time.Second, Transport: &flutterStubTransport{body: flutterFixtureJSON}}}

	versions, err := f.FetchRemoteVersions()
	if err != nil {
		t.Fatalf("FetchRemoteVersions: %v", err)
	}
	if len(versions) == 0 {
		t.Fatal("expected non-empty stable version list (GitHub Releases for flutter/flutter is always empty; the JSON source must be used)")
	}
	for _, v := range versions {
		if strings.Contains(v.Version, "beta") || strings.Contains(v.Version, "dev") || strings.Contains(v.Version, "pre") {
			t.Errorf("non-stable version leaked into the list: %q", v.Version)
		}
		if v.DownloadURL == "" || v.FileName == "" {
			t.Errorf("version %s missing URL/FileName: %+v", v.Version, v)
		}
	}
	// Descending semantic order.
	for i := 1; i < len(versions); i++ {
		if CompareVersions(versions[i-1].Version, versions[i].Version) < 0 {
			t.Errorf("list not sorted descending at index %d: %s before %s", i, versions[i-1].Version, versions[i].Version)
		}
	}
}

// TestFlutterFetchRemoteVersionsError verifies HTTP errors surface instead of
// producing a silently empty list.
func TestFlutterFetchRemoteVersionsError(t *testing.T) {
	f := &FlutterFetcher{httpClient: &http.Client{Timeout: 5 * time.Second, Transport: &flutterStubTransport{body: "", status: http.StatusNotFound}}}
	if _, err := f.FetchRemoteVersions(); err == nil {
		t.Fatal("expected an error on HTTP 404")
	}
}

// TestFlutterGetDownloadURLUsesMetadata checks GetDownloadURL prefers the
// archive recorded in the releases metadata over the constructed pattern.
func TestFlutterGetDownloadURLUsesMetadata(t *testing.T) {
	clearFlutterCacheEntry(t)
	f := &FlutterFetcher{httpClient: &http.Client{Timeout: 5 * time.Second, Transport: &flutterStubTransport{body: flutterFixtureJSON}}}

	url, fileName, err := f.GetDownloadURL("3.44.8")
	if err != nil {
		t.Fatalf("GetDownloadURL: %v", err)
	}
	if url != "https://storage.googleapis.com/flutter_infra_release/releases/stable/windows/flutter_windows_3.44.8-stable.zip" {
		t.Errorf("URL = %q; want the metadata-derived URL", url)
	}
	if fileName != "flutter_windows_3.44.8-stable.zip" {
		t.Errorf("fileName = %q; want flutter_windows_3.44.8-stable.zip", fileName)
	}

	// Unknown version falls back to the naming pattern (no error).
	url, fileName, err = f.GetDownloadURL("1.0.0")
	if err != nil {
		t.Fatalf("GetDownloadURL fallback: %v", err)
	}
	if !strings.Contains(url, "1.0.0-stable") || fileName == "" {
		t.Errorf("fallback URL/fileName malformed: %q / %q", url, fileName)
	}
}

// TestFlutterFetchChecksum verifies the ChecksumFetcher implementation reads
// the sha256 embedded in the releases JSON.
func TestFlutterFetchChecksum(t *testing.T) {
	f := &FlutterFetcher{httpClient: &http.Client{Timeout: 5 * time.Second, Transport: &flutterStubTransport{body: flutterFixtureJSON}}}

	// The fetcher must satisfy the optional ChecksumFetcher interface.
	var _ ChecksumFetcher = f

	got, err := f.FetchChecksum("3.44.8")
	if err != nil {
		t.Fatalf("FetchChecksum: %v", err)
	}
	// On a darwin/arm64 host 3.44.8 (x64-only entry) still resolves via the
	// arch fallback, so accept the fixture's recorded sum.
	if got != "sum-3448-x64" {
		t.Errorf("FetchChecksum(3.44.8) = %q; want sum-3448-x64", got)
	}

	// Unknown version: ("", nil) -> verification skipped, not an error.
	got, err = f.FetchChecksum("0.0.1")
	if err != nil {
		t.Fatalf("FetchChecksum(unknown): %v", err)
	}
	if got != "" {
		t.Errorf("FetchChecksum(unknown) = %q; want empty", got)
	}
}
