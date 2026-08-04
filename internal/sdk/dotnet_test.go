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
