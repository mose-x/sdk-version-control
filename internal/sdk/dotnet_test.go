package sdk

import (
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
