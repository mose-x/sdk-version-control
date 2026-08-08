//go:build !windows

package shimmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sdk_version_control/internal/config"
)

// TestRemoveSourceLineFromFile_preservesComments pins BUG E: the broad
// ".svc.rc" match was stripping any line mentioning .svc.rc, including user
// comments. The tightened rule keeps comments and only drops lines that
// actually source .svc.rc (exact rcPath match, OR a `source` + `.svc.rc`
// line).
func TestRemoveSourceLineFromFile_preservesComments(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".svc.rc")
	rcFile := filepath.Join(dir, ".zshrc")

	// Source line matches both `checkStr == rcPath` and the source+.svc.rc
	// guard. The comment only matches the old broad ".svc.rc" rule; under
	// the tightened rule it must survive.
	lines := []string{
		"# user .zshrc",
		`[[ -f ` + rcPath + ` ]] && source ` + rcPath,
		"# this is a comment about .svc.rc, don't delete me",
		"export EDITOR=vim",
		"",
	}
	if err := os.WriteFile(rcFile, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeSourceLineFromFile(rcFile, rcPath); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(rcFile)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	if strings.Contains(got, rcPath) {
		t.Errorf("source line referencing rcPath was not removed:\n%s", got)
	}
	if !strings.Contains(got, "comment about .svc.rc") {
		t.Errorf("user comment about .svc.rc was stripped; should be preserved:\n%s", got)
	}
	if !strings.Contains(got, "export EDITOR=vim") {
		t.Errorf("unrelated line was stripped; should be preserved:\n%s", got)
	}
}

// TestRemoveSourceLineFromFile_stripsRelativeSourceLine covers the source+.svc.rc
// fallback path: when a shell rc has a source line that uses a relative-ish
// reference instead of the exact rcPath (e.g. `source ~/.svc.rc`), the
// tightened rule still drops it because the line both sources AND mentions
// .svc.rc.
func TestRemoveSourceLineFromFile_stripsRelativeSourceLine(t *testing.T) {
	dir := t.TempDir()
	rcFile := filepath.Join(dir, ".bashrc")
	lines := []string{
		"# header",
		"source ~/.svc.rc",
		"alias ll='ls -la'",
		"",
	}
	if err := os.WriteFile(rcFile, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	// checkStr is the absolute rcPath; the source line doesn't contain it,
	// so only the source+.svc.rc guard can drop the line. This pins that the
	// guard fires for genuine source lines.
	absRc := filepath.Join(dir, ".svc.rc")
	if err := removeSourceLineFromFile(rcFile, absRc); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(rcFile)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "source ~/.svc.rc") {
		t.Errorf("relative source line was kept; should be removed:\n%s", got)
	}
	if !strings.Contains(got, "alias ll") {
		t.Errorf("unrelated alias was stripped; should be preserved:\n%s", got)
	}
}

// TestDetectConfiguredShells_onlySourceLines pins that detectConfiguredShells
// no longer reports a shell as "configured" when its rc file merely mentions
// .svc.rc in a comment — it must contain an actual source line. The exact
// rcPath check remains the primary signal; the source+.svc.rc guard is the
// fallback for relative source lines.
func TestDetectConfiguredShells_onlySourceLines(t *testing.T) {
	// AvailableShells reads os.UserHomeDir directly; redirect HOME to a temp
	// dir so the test is hermetic and never touches the real ~/.zshrc.
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := config.NewConfig()
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg)
	rcPath := cfg.RcFilePath()

	zshrc := filepath.Join(home, ".zshrc")
	content := strings.Join([]string{
		"# user .zshrc",
		`[[ -f ` + rcPath + ` ]] && source ` + rcPath,
		"# comment about .svc.rc, not a source line",
		"",
	}, "\n")
	if err := os.WriteFile(zshrc, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got := m.detectConfiguredShells()
	if !shellInList("zsh", got) {
		t.Errorf(`detectConfiguredShells = %v; want "zsh" present (zshrc has the source line)`, got)
	}
}

// TestDetectConfiguredShells_skipsBareMention is the regression arm: a shell
// rc with ONLY a bare comment mentioning .svc.rc (no source line) must NOT
// be reported as configured.
func TestDetectConfiguredShells_skipsBareMention(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := config.NewConfig()
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg)

	zshrc := filepath.Join(home, ".zshrc")
	content := strings.Join([]string{
		"# user .zshrc",
		"# remember .svc.rc is a thing",
		"export EDITOR=vim",
		"",
	}, "\n")
	if err := os.WriteFile(zshrc, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if shellInList("zsh", m.detectConfiguredShells()) {
		t.Errorf(`detectConfiguredShells reported "zsh"; want absent (zshrc only has a bare mention)`)
	}
}

func shellInList(want string, list []string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestApplyEnvVarsToSystem_unixNoOp pins BUG C's cross-platform contract: on
// Unix, applyEnvVarsToSystem + removeEnvVarsFromSystem are no-ops (the .svc.rc
// file is the single source of truth for env vars; there is no system-level
// store to update). The Windows counterparts write to the registry, which is
// intentionally not unit-tested here. The contract: the Unix methods must not
// touch the filesystem and must not panic on any input.
func TestApplyEnvVarsToSystem_unixNoOp(t *testing.T) {
	m := newTestManager(t)
	shimsDir := m.cfg.ShimsDir()
	rcPath := m.cfg.RcFilePath()

	// Snapshot the shims dir + rc file state before; both should be unchanged
	// after the no-op calls. .svc.rc does not exist under the temp SVC dir
	// (newTestManager only writes the svc-shim binary), so it must remain
	// absent.
	beforeShims, _ := os.ReadDir(shimsDir)
	_, rcErr := os.Stat(rcPath)
	rcMissingBefore := rcErr != nil

	m.applyEnvVarsToSystem([]envVarEntry{
		{Key: "JAVA_HOME", Value: "/x"},
		{Key: "GOROOT", Value: "/y"},
	})
	m.removeEnvVarsFromSystem([]string{"JAVA_HOME", "GOROOT"})

	afterShims, err := os.ReadDir(shimsDir)
	if err != nil {
		t.Fatalf("shims dir unreadable after no-op: %v", err)
	}
	if len(beforeShims) != len(afterShims) {
		t.Errorf("Unix no-op touched the shims dir: %d entries -> %d", len(beforeShims), len(afterShims))
	}
	_, rcErr = os.Stat(rcPath)
	rcMissingAfter := rcErr != nil
	if rcMissingBefore != rcMissingAfter {
		t.Errorf("Unix no-op touched .svc.rc: missingBefore=%v missingAfter=%v", rcMissingBefore, rcMissingAfter)
	}
}
