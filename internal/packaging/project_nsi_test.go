package packaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// findRepoRoot walks up from the test's working directory (the package dir)
// until it finds go.mod, returning the repository root. go test runs with
// cwd = package directory, so a relative path like "build/..." must be
// anchored at the root explicitly now that this test lives in a subpackage.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repository root (go.mod not found)")
		}
		dir = parent
	}
}

// TestProjectNSI_UpgradeInPlace verifies the NSIS installer template has the
// four upgrade-in-place features: InstallDirRegKey (detect previous),
// SkipDirIfInstalled (skip dir page), taskkill (kill running app), and
// CopyFiles + .bak (backup old version).
func TestProjectNSI_UpgradeInPlace(t *testing.T) {
	nsiPath := filepath.Join(findRepoRoot(t), "build", "windows", "installer", "project.nsi")
	data, err := os.ReadFile(nsiPath)
	if err != nil {
		t.Skipf("project.nsi not found (not on Windows?): %v", err)
	}
	content := string(data)

	checks := []struct {
		desc   string
		substr string
	}{
		{"InstallDirRegKey for auto-detect", `InstallDirRegKey HKLM "${UNINST_KEY}" "InstallLocation"`},
		{"SkipDirIfInstalled function", "Function SkipDirIfInstalled"},
		{"Abort to skip dir page", "Abort"},
		{"silent kill hides console", "-WindowStyle Hidden"},
		{"kills current process", `Stop-Process -Name ${INFO_PROJECTNAME} -Force`},
		{"kills legacy process", `Stop-Process -Name \"${LEGACY_PRODUCTNAME}\" -Force`},
		{"CopyFiles backup", `CopyFiles /SILENT "$INSTDIR\${PRODUCT_EXECUTABLE}" "$INSTDIR\${PRODUCT_EXECUTABLE}.bak"`},
		{"InstallLocation write to registry", `WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"`},
		// Rename migration (SDK Version Control -> svc): legacy installs are
		// fully retired into the fresh directory (no in-place reuse of the
		// old folder), so folder/shortcuts/registry all carry the new name.
		{"legacy product name define", `!define LEGACY_PRODUCTNAME "SDK Version Control"`},
		{"legacy executable define", `!define LEGACY_EXECUTABLE  "SDK Version Control.exe"`},
		{"legacy uninstall key define", `!define LEGACY_UNINST_KEY  "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_COMPANYNAME}${LEGACY_PRODUCTNAME}"`},
		{"legacy shortcuts removal", `Delete "$DESKTOP\${LEGACY_PRODUCTNAME}.lnk"`},
		{"uninstall deletes new desktop shortcuts via wildcard", `Delete "$DESKTOP\${INFO_PRODUCTNAME}*.lnk"`},
		{"uninstall deletes legacy desktop shortcuts via wildcard", `Delete "$DESKTOP\${LEGACY_PRODUCTNAME}*.lnk"`},
		{"uninstall deletes new start-menu shortcuts via wildcard", `Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}*.lnk"`},
		{"uninstall scans all-users context too", "SetShellVarContext all"},
		{"legacy uninstall key cleanup", `DeleteRegKey HKLM "${LEGACY_UNINST_KEY}"`},
		{"legacy WebView2 datapath cleanup", `RMDir /r "$AppData\${LEGACY_EXECUTABLE}"`},
		{"legacy directory removal", `RMDir /r "$1"`},
		{"same-dir guard for legacy removal", `${If} $1 != "$INSTDIR"`},
		// Self-updated legacy installs have no registry entry; the Section's
		// else-branch detects the legacy executable at the default old
		// location and removes that folder, and retires any old-named exe
		// found inside the chosen install directory.
		{"self-update legacy folder detection", `IfFileExists "$PROGRAMFILES64\${LEGACY_PRODUCTNAME}\${LEGACY_EXECUTABLE}"`},
		{"self-update legacy folder removal", `RMDir /r "$PROGRAMFILES64\${LEGACY_PRODUCTNAME}"`},
		{"legacy exe in-place retirement", `Delete "$INSTDIR\${LEGACY_EXECUTABLE}"`},
	}

	for _, c := range checks {
		t.Run(c.desc, func(t *testing.T) {
			if !strings.Contains(content, c.substr) {
				t.Errorf("project.nsi missing %q\nin content:\n%s", c.substr, content)
			}
		})
	}
}
