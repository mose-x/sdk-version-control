//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildLegacyRenamePs pins the structure of the migration script: PID
// wait loop, folder rename with fallback, exe rename with rollback-compatible
// backup, shortcut recreation, relaunch.
func TestBuildLegacyRenamePs(t *testing.T) {
	const pid = 4242
	currentExe := `C:\Program Files\SDK Version Control\SDK Version Control.exe`
	script := buildLegacyRenamePs(pid, currentExe)

	checks := []struct {
		desc string
		want string
	}{
		{"embeds the current exe path", currentExe},
		{"embeds the PID to wait on", "$targetPid = 4242"},
		{"waits via Get-Process polling", "Get-Process -Id $targetPid"},
		{"wait loop bounded by timeout", "$timeout = 60"},
		{"folder renamed to svc", "Rename-Item -LiteralPath $oldDir -NewName 'svc'"},
		{"folder rename fallback keeps old dir", "if (-not (Test-Path -LiteralPath $newDir)) { $newDir = $oldDir }"},
		{"executable renamed to svc.exe", "Rename-Item -LiteralPath $legacyExe -NewName 'svc.exe'"},
		{"backup named for RollbackUpdate compatibility", "'svc.exe.bak'"},
		{"legacy shortcut name handled", "'SDK Version Control.lnk'"},
		{"new shortcut name created", "'svc.lnk'"},
		{"relaunches the new executable", "Start-Process -FilePath $newExe"},
	}
	for _, c := range checks {
		t.Run(c.desc, func(t *testing.T) {
			if !strings.Contains(script, c.want) {
				t.Errorf("script missing %q\n--- script ---\n%s", c.want, script)
			}
		})
	}
}

// TestBuildLegacyRenamePsEscapesQuotes ensures a single quote in the exe path
// is doubled so the generated PowerShell stays syntactically valid.
func TestBuildLegacyRenamePsEscapesQuotes(t *testing.T) {
	script := buildLegacyRenamePs(1, `C:\we'ird\SDK Version Control.exe`)
	if !strings.Contains(script, `C:\we''ird\SDK Version Control.exe`) {
		t.Errorf("single quote not doubled in path\n--- script ---\n%s", script)
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
