package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectNSI_UpgradeInPlace verifies the NSIS installer template has the
// four upgrade-in-place features: InstallDirRegKey (detect previous),
// SkipDirIfInstalled (skip dir page), taskkill (kill running app), and
// CopyFiles + .bak (backup old version).
func TestProjectNSI_UpgradeInPlace(t *testing.T) {
	nsiPath := filepath.Join("build", "windows", "installer", "project.nsi")
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
		{"taskkill running app", `taskkill /F /IM "${PRODUCT_EXECUTABLE}"`},
		{"CopyFiles backup", `CopyFiles /SILENT "$INSTDIR\${PRODUCT_EXECUTABLE}" "$INSTDIR\${PRODUCT_EXECUTABLE}.bak"`},
		{"InstallLocation write to registry", `WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"`},
	}

	for _, c := range checks {
		t.Run(c.desc, func(t *testing.T) {
			if !strings.Contains(content, c.substr) {
				t.Errorf("project.nsi missing %q\nin content:\n%s", c.substr, content)
			}
		})
	}
}
