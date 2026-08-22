//go:build darwin

package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildLegacyRenameSh pins the structure of the macOS migration script:
// PID wait, bundle rename with fallback, Info.plist display-name update,
// relaunch.
func TestBuildLegacyRenameSh(t *testing.T) {
	const pid = 4242
	exe := "/Applications/SDK Version Control.app/Contents/MacOS/SDK Version Control"
	script := buildLegacyRenameSh(pid, exe)

	checks := []struct {
		desc string
		want string
	}{
		{"embeds the current exe path", exe},
		{"embeds the PID to wait on", `PID="4242"`},
		{"waits via kill -0 polling", `kill -0 "$PID"`},
		{"wait bounded by timeout", "timeout=60"},
		{"bundle renamed to svc.app", `NEW_BUNDLE="$APPDIR/svc.app"`},
		{"bundle rename fallback", `NEW_BUNDLE="$BUNDLE"`},
		{"inner executable renamed to svc", `mv "$MACOS_NEW/$INNER_OLD" "$MACOS_NEW/svc"`},
		{"updates CFBundleName", `plutil -replace CFBundleName -string "svc"`},
		{"updates CFBundleExecutable", `plutil -replace CFBundleExecutable -string "svc"`},
		{"relaunch guarded by svc existence", `if [ -f "$MACOS_NEW/svc" ]; then`},
		{"relaunches via open", `open "$NEW_BUNDLE"`},
	}
	for _, c := range checks {
		t.Run(c.desc, func(t *testing.T) {
			if !strings.Contains(script, c.want) {
				t.Errorf("script missing %q\n--- script ---\n%s", c.want, script)
			}
		})
	}
}

// TestIsLegacyDarwinInstall checks detection via the bundle folder name using
// a fake executable path layout.
func TestIsLegacyDarwinInstall(t *testing.T) {
	// The test binary itself is not inside a legacy bundle.
	if isLegacyDarwinInstall() {
		t.Error("test binary should not be detected as a legacy install")
	}
	// Sanity: the legacy constant matches the documented old bundle name.
	if legacyAppBundleName != "SDK Version Control.app" {
		t.Errorf("legacyAppBundleName = %q", legacyAppBundleName)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if strings.EqualFold(filepath.Base(bundle), legacyAppBundleName) {
		t.Errorf("unexpected: test runs inside legacy bundle %s", bundle)
	}
}
