package sdk

import (
	"runtime"
	"strings"
	"testing"
)

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

func TestFlutterGetDownloadURL(t *testing.T) {
	f := &FlutterFetcher{}
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
}
