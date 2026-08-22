//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildLegacyRenameScript pins the structure of the migration script:
// PID wait loop, folder rename with fallback, exe rename with
// rollback-compatible backup, shortcut recreation, relaunch.
func TestBuildLegacyRenameScript(t *testing.T) {
	const pid = 4242
	currentExe := `C:\Program Files\SDK Version Control\SDK Version Control.exe`
	script := buildLegacyRenameScript(pid, currentExe)

	checks := []struct {
		desc string
		want string
	}{
		{"embeds the current exe path", currentExe},
		{"PID wait loop uses space-delimited match", `findstr /C:" 4242 "`},
		{"wait loop aborts on timeout", "Migration aborted: application did not exit in time"},
		{"folder rename to svc", `set "NEW_DIR=%PARENT_DIR%\svc"`},
		{"folder rename fallback on failure", `if errorlevel 1 set "NEW_DIR=%OLD_DIR%"`},
		{"executable renamed to svc.exe", `ren "%NEW_DIR%\%OLD_NAME%" "svc.exe"`},
		{"backup named for RollbackUpdate compatibility", `copy /Y "%NEW_DIR%\%OLD_NAME%" "%NEW_DIR%\svc.exe.bak"`},
		{"legacy shortcut name handled", `'SDK Version Control.lnk'`},
		{"new shortcut name created", `'svc.lnk'`},
		{"relaunches the new executable", `start "" "%NEW_EXE%"`},
	}
	for _, c := range checks {
		t.Run(c.desc, func(t *testing.T) {
			if !strings.Contains(script, c.want) {
				t.Errorf("script missing %q\n--- script ---\n%s", c.want, script)
			}
		})
	}
}

// TestIsLegacyWindowsInstall documents the detection semantics: the test
// binary itself is not the legacy executable, so detection is false here.
func TestIsLegacyWindowsInstall(t *testing.T) {
	if isLegacyWindowsInstall() {
		t.Error("test binary should not be detected as the legacy install")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(filepath.Base(exe), legacyExeName) {
		t.Errorf("unexpected: test binary is named %s", legacyExeName)
	}
}
