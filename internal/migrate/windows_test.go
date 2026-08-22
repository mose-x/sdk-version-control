//go:build windows

package migrate

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
		{"folder rename fallback keeps old dir", "$newDir = $oldDir"},
		{"executable renamed to svc.exe", "Rename-Item -LiteralPath $legacyExe -NewName 'svc.exe'"},
		{"backup named for RollbackUpdate compatibility", "'svc.exe.bak'"},
		{"rollback backup prefers the self-update backup", "Move-Item -LiteralPath $legacyBak -Destination $newBak -Force"},
		{"fallback copies current exe when no self-update backup", "Copy-Item -LiteralPath $legacyExe -Destination $newBak -Force"},
		{"legacy shortcut matched by name pattern", "-like 'SDK Version Control*'"},
		{"legacy shortcut matched by target path", "-like '*SDK Version Control*'"},
		{"retargets legacy shortcut in place", "$s.TargetPath = $newExe"},
		{"renames legacy shortcut to svc.lnk in place", "Rename-Item -LiteralPath $lnk.FullName -NewName 'svc.lnk'"},
		{"removes legacy duplicate when svc.lnk exists", "Remove-Item -LiteralPath $lnk.FullName -Force"},
		{"new shortcut name created", "'svc.lnk'"},
		{"relaunch guarded by svc.exe existence", "if (Test-Path -LiteralPath (Join-Path $newDir 'svc.exe'))"},
		{"relaunches svc.exe", "Start-Process -FilePath (Join-Path $newDir 'svc.exe')"},
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

// TestBuildShortcutIconRepairPs pins the startup icon/target repair script so
// shortcuts left pointing at the old renamed folder get their logo back.
func TestBuildShortcutIconRepairPs(t *testing.T) {
	exe := `C:\Program Files\svc\svc.exe`
	script := buildShortcutIconRepairPs(exe)

	checks := []struct {
		desc string
		want string
	}{
		{"embeds the current exe path", exe},
		{"icon from migrated install", "icon-white.ico"},
		{"icon falls back to exe", "$icon = $exe"},
		{"retargets shortcut to current exe", "$s.TargetPath = $exe"},
		{"re-points icon location", `$s.IconLocation = "$icon, 0"`},
		{"matches svc.lnk", "'svc.lnk'"},
		{"matches legacy target", "*SDK Version Control*"},
	}
	for _, c := range checks {
		t.Run(c.desc, func(t *testing.T) {
			if !strings.Contains(script, c.want) {
				t.Errorf("script missing %q\n--- script ---\n%s", c.want, script)
			}
		})
	}
}
