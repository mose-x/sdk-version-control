package sdk

import (
	"strings"
	"testing"
)

// TestRubyInstallerAssetSkipsSignatureSidecar verifies the x64 archive picker
// does NOT select ".7z.asc" signature files. The old substring match
// ("-x64.7z" anywhere in the name) hit the sidecar first when it was listed
// before the real archive; only a suffix match is correct.
func TestRubyInstallerAssetSkipsSignatureSidecar(t *testing.T) {
	f := &RubyFetcher{}
	assets := []ghAsset{
		{Name: "rubyinstaller-3.2.2-1-x64.7z.asc", BrowserDownloadURL: "https://github.com/x/y.asc"},
		{Name: "rubyinstaller-3.2.2-1-x64.7z", BrowserDownloadURL: "https://github.com/x/y.7z"},
	}
	url, name := f.rubyInstallerAsset("3.2.2", assets)
	if name != "rubyinstaller-3.2.2-1-x64.7z" {
		t.Errorf("picked %q; want the real 7z archive, not the .asc sidecar", name)
	}
	if !strings.HasSuffix(url, "y.7z") {
		t.Errorf("URL should point at the archive, got %q", url)
	}
}

// TestRubyInstallerAssetIgnoresJavadoc keeps the long-standing exclusion of
// the -javadoc archive intact via the suffix rule.
func TestRubyInstallerAssetIgnoresJavadoc(t *testing.T) {
	f := &RubyFetcher{}
	assets := []ghAsset{
		{Name: "rubyinstaller-3.2.2-1-x64-javadoc.7z", BrowserDownloadURL: "https://github.com/x/javadoc.7z"},
		{Name: "rubyinstaller-3.2.2-1-x64.7z", BrowserDownloadURL: "https://github.com/x/y.7z"},
	}
	_, name := f.rubyInstallerAsset("3.2.2", assets)
	if name != "rubyinstaller-3.2.2-1-x64.7z" {
		t.Errorf("picked %q; want the main archive, not javadoc", name)
	}
}

// TestRubyInstallerAssetFallback exercises the constructed-URL fallback when a
// release lists no matching asset at all.
func TestRubyInstallerAssetFallback(t *testing.T) {
	f := &RubyFetcher{}
	assets := []ghAsset{
		{Name: "rubyinstaller-3.2.2-1-x64.7z.asc", BrowserDownloadURL: "https://github.com/x/y.asc"},
	}
	url, name := f.rubyInstallerAsset("3.2.2", assets)
	if name != "rubyinstaller-3.2.2-1-x64.7z" {
		t.Errorf("fallback fileName = %q; want rubyinstaller-3.2.2-1-x64.7z", name)
	}
	if !strings.Contains(url, "RubyInstaller-3.2.2-1") || !strings.HasSuffix(url, name) {
		t.Errorf("fallback URL malformed: %q", url)
	}
}
