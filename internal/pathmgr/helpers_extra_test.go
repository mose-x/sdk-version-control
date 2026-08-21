package pathmgr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasPathPrefix(t *testing.T) {
	sep := string(os.PathSeparator)
	sdkDir := filepath.Join("home", "u", ".svc", "go")
	cases := []struct {
		p, dir string
		want   bool
	}{
		{sdkDir, sdkDir, true},
		{filepath.Join(sdkDir, "bin"), sdkDir, true},
		{sdkDir + "lang", sdkDir, false}, // /a/b must not match /a/bc
		{filepath.Join("home", "u", ".svc", "golang"), sdkDir, false},
		{"", sdkDir, false},
		{filepath.Join("home", "u", ".svc"), sdkDir, false},
		{"anything", "", false}, // empty dir matches nothing (old HasPrefix matched all)
	}
	for _, c := range cases {
		if got := hasPathPrefix(c.p, c.dir); got != c.want {
			t.Errorf("hasPathPrefix(%q,%q)=%v want %v", c.p, c.dir, got, c.want)
		}
	}
	_ = sep
}

func TestHasSvcSegment(t *testing.T) {
	sep := string(os.PathSeparator)
	cases := []struct {
		p    string
		want bool
	}{
		{filepath.Join("home", "u", ".svc", "shims"), true},
		{filepath.Join("home", "u", ".svc"), true},
		{filepath.Join("home", "u", ".svcx"), false},
		{filepath.Join("opt", "my.svc.d", "bin"), false},
		{filepath.Join("repo", ".svc-backup"), false},
		{"", false},
	}
	for _, c := range cases {
		if got := hasSvcSegment(c.p); got != c.want {
			t.Errorf("hasSvcSegment(%q)=%v want %v", c.p, got, c.want)
		}
	}
	_ = sep
}

func TestDetectSdkRootValidationFailure(t *testing.T) {
	tmp := t.TempDir()
	// go: dir without bin/ must now return "" (previously returned candidate)
	binDir := filepath.Join(tmp, "notgoroot")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectSdkRoot(binDir, "go"); got != "" {
		t.Errorf("DetectSdkRoot on invalid go dir = %q, want empty", got)
	}
	// go: dir with bin/ is accepted
	if err := os.MkdirAll(filepath.Join(binDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectSdkRoot(binDir, "go"); got != binDir {
		t.Errorf("DetectSdkRoot on valid go dir = %q, want %q", got, binDir)
	}
	// unknown sdk type keeps returning the candidate unchanged
	if got := DetectSdkRoot(binDir, "perl"); got != binDir {
		t.Errorf("DetectSdkRoot for unknown type = %q, want %q", got, binDir)
	}
}
