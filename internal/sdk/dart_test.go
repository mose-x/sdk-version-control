package sdk

import (
	"runtime"
	"strings"
	"testing"
)

func TestDartArch(t *testing.T) {
	// Dart provides arm64 builds for all platforms, so the result depends
	// only on goarch (not goos). Exercise all 6 combos to confirm.
	tests := []struct {
		goos, goarch, want string
	}{
		{"windows", "amd64", "x64"},
		{"windows", "arm64", "arm64"},
		{"linux", "amd64", "x64"},
		{"linux", "arm64", "arm64"},
		{"darwin", "amd64", "x64"},
		{"darwin", "arm64", "arm64"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"-"+tt.goarch, func(t *testing.T) {
			got := dartArch(tt.goos, tt.goarch)
			if got != tt.want {
				t.Errorf("dartArch(%q, %q): expected %q, got %q", tt.goos, tt.goarch, tt.want, got)
			}
		})
	}
}

func TestDartOSName(t *testing.T) {
	tests := []struct {
		goos, want string
	}{
		{"windows", "windows"},
		{"linux", "linux"},
		{"darwin", "macos"},
		{"freebsd", "windows"}, // unknown OS falls back to the windows token
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			if got := dartOSName(tt.goos); got != tt.want {
				t.Errorf("dartOSName(%q) = %q; want %q", tt.goos, got, tt.want)
			}
		})
	}
}

// TestDartFileNameIncludesVersion pins the versioned download file name: the
// FileName is used as the temp file in TmpDir during install, and the old
// version-less name made different versions collide on the same file.
func TestDartFileNameIncludesVersion(t *testing.T) {
	tests := []struct {
		osName, arch, version, want string
	}{
		{"windows", "x64", "3.5.0", "dartsdk-windows-x64-3.5.0-release.zip"},
		{"linux", "arm64", "3.4.1", "dartsdk-linux-arm64-3.4.1-release.zip"},
		{"macos", "x64", "2.19.6", "dartsdk-macos-x64-2.19.6-release.zip"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := dartFileName(tt.osName, tt.arch, tt.version); got != tt.want {
				t.Errorf("dartFileName(%q, %q, %q) = %q; want %q", tt.osName, tt.arch, tt.version, got, tt.want)
			}
		})
	}
}

// TestDartGetDownloadURLVersionedFileName checks the public GetDownloadURL
// returns a FileName containing the requested version on the current host.
func TestDartGetDownloadURLVersionedFileName(t *testing.T) {
	f := &DartFetcher{}
	url, fileName, err := f.GetDownloadURL("3.5.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(fileName, "3.5.0") {
		t.Errorf("FileName should contain the version, got %q", fileName)
	}
	if !strings.HasSuffix(fileName, "-release.zip") {
		t.Errorf("FileName should keep the -release.zip suffix for the extractor, got %q", fileName)
	}
	if !strings.Contains(url, "/3.5.0/sdk/") {
		t.Errorf("URL should keep the version in the path, got %q", url)
	}
	wantName := dartFileName(dartOSName(runtime.GOOS), dartArch(runtime.GOOS, runtime.GOARCH), "3.5.0")
	if fileName != wantName {
		t.Errorf("FileName = %q; want %q", fileName, wantName)
	}
}
