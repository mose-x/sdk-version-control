//go:build !windows

package shimmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"svc/internal/config"
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

// --- shellSingleQuote / fishEscape (pure escaping helpers) ---

// TestShellSingleQuote pins item 8: values are wrapped in single quotes and
// embedded single quotes use the standard POSIX end-quote/backslash-quote/
// reopen sequence. The result is inert for $, backtick and backslash.
func TestShellSingleQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/opt/jdk", `'/opt/jdk'`},
		{"", `''`},
		{"a b", `'a b'`},
		{"$(evil)", `'$(evil)'`},
		{"`id`", "'`id`'"},
		{"it's", `'it'\''s'`},
		{`back\slash`, `'back\slash'`},
	}
	for _, tc := range cases {
		if got := shellSingleQuote(tc.in); got != tc.want {
			t.Errorf("shellSingleQuote(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

// TestFishEscape pins item 6: fish values are backslash-escaped, NOT wrapped
// in quotes. Alphanumerics and path-safe punctuation pass through; spaces
// and shell metacharacters get a backslash.
func TestFishEscape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/opt/jdk-21/bin", `/opt/jdk-21/bin`},
		{"", ``},
		{"/a b/c", `/a\ b/c`},
		{"$HOME/x", `\$HOME/x`},
		{"a;b|c&d", `a\;b\|c\&d`},
		{"q'uote", `q\'uote`},
	}
	for _, tc := range cases {
		if got := fishEscape(tc.in); got != tc.want {
			t.Errorf("fishEscape(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

// --- generateRcContent single-quoting (item 8) ---

func TestGenerateRcContent_singleQuotesEnvVars(t *testing.T) {
	m := newTestManager(t)
	svcHome := m.cfg.SvcDir()
	jdkDir := filepath.Join(svcHome, "jdk", "21")
	content := m.generateRcContent([]envVarEntry{
		{Key: "JAVA_HOME", Value: jdkDir},
		{Key: "EVIL", Value: "$(touch /tmp/pwned)"},
	})
	if !strings.Contains(content, "export JAVA_HOME='"+jdkDir+"'") {
		t.Errorf("JAVA_HOME not single-quoted with its absolute value:\n%s", content)
	}
	if !strings.Contains(content, "export EVIL='$(touch /tmp/pwned)'") {
		t.Errorf("EVIL value not inertly single-quoted:\n%s", content)
	}
	// The old %q form emitted double quotes around env var values.
	if strings.Contains(content, `export JAVA_HOME="`) {
		t.Errorf("env var still double-quoted (%%q form):\n%s", content)
	}
	// No $SVC_HOME rewrite: the value is under svcHome but must stay an
	// absolute path (a $ reference would not expand inside single quotes).
	if strings.Contains(content, "$SVC_HOME/jdk") {
		t.Errorf("env var value rewritten to $SVC_HOME (would not expand inside single quotes):\n%s", content)
	}
}

func TestGenerateRcContent_svcHomeQuoting(t *testing.T) {
	m := newTestManager(t)
	home := m.cfg.HomeDir()

	// Default svcDir (home/.svc): keeps the $HOME reference line.
	m.cfg.SetSvcDir(filepath.Join(home, ".svc"))
	content := m.generateRcContent(nil)
	if !strings.Contains(content, `export SVC_HOME="$HOME/.svc"`) {
		t.Errorf("default SVC_HOME line missing:\n%s", content)
	}

	// Custom svcHome (not home/.svc) must be single-quoted.
	m.cfg.SetSvcDir("/custom/svc dir")
	content = m.generateRcContent(nil)
	if !strings.Contains(content, `export SVC_HOME='/custom/svc dir'`) {
		t.Errorf("custom SVC_HOME not single-quoted:\n%s", content)
	}
}

// --- fish env file (item 6) ---

func TestGenerateFishEnvContent_includesEnvVars(t *testing.T) {
	m := newTestManager(t)
	content := m.generateFishEnvContent([]envVarEntry{
		{Key: "JAVA_HOME", Value: "/opt/jdk 21"},
	})
	shims := fishEscape(m.cfg.ShimsDir())
	if !strings.Contains(content, "set -gx PATH "+shims+" $PATH") {
		t.Errorf("fish PATH line missing/wrong:\n%s", content)
	}
	if !strings.Contains(content, `set -gx JAVA_HOME /opt/jdk\ 21`) {
		t.Errorf("fish env var missing or not escaped:\n%s", content)
	}
}

// TestAddSourceLine_fishWritesEnvVars pins item 6 end-to-end: configuring
// the fish shell writes env.sh.fish with BOTH the PATH line and the SDK env
// vars (previously only PATH was written), and the injected source line in
// conf.d/svc.fish has quoted paths.
func TestAddSourceLine_fishWritesEnvVars(t *testing.T) {
	// config.NewConfig (with redirected HOME) initializes the internal
	// ActiveVersions map that collectEnvVars/GetActiveVersion need.
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := config.NewConfig()
	if err != nil {
		t.Fatal(err)
	}
	svcDir := filepath.Join(home, "svchome")
	if err := os.MkdirAll(filepath.Join(svcDir, "shims"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg.SetSvcDir(svcDir)
	cfg.SetHomeDir(home)
	if err := cfg.SetActiveVersion("jdk", "21"); err != nil {
		t.Fatal(err)
	}
	m := New(cfg)

	shimsJSON := `{
  "commands": {"java": "jdk"},
  "sdkTypes": {"jdk": {"binDirs": ["bin"], "envVars": {"JAVA_HOME": ""}}}
}`
	if err := os.WriteFile(m.cfg.ShimsConfigPath(), []byte(shimsJSON), 0644); err != nil {
		t.Fatal(err)
	}

	if err := m.addSourceLine("fish"); err != nil {
		t.Fatal(err)
	}

	fishEnv := filepath.Join(svcDir, "env.sh.fish")
	data, err := os.ReadFile(fishEnv)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "set -gx PATH") {
		t.Errorf("env.sh.fish missing PATH line:\n%s", content)
	}
	if !strings.Contains(content, "set -gx JAVA_HOME") {
		t.Errorf("env.sh.fish missing JAVA_HOME (item 6: env vars must be written):\n%s", content)
	}
	wantVal := fishEscape(filepath.Join(svcDir, "jdk", "21"))
	if !strings.Contains(content, "set -gx JAVA_HOME "+wantVal) {
		t.Errorf("env.sh.fish JAVA_HOME value wrong; want %q in:\n%s", wantVal, content)
	}

	fishFile := filepath.Join(home, ".config", "fish", "conf.d", "svc.fish")
	data, err = os.ReadFile(fishFile)
	if err != nil {
		t.Fatal(err)
	}
	want := `test -f "` + fishEnv + `"; and source "` + fishEnv + `"`
	if !strings.Contains(string(data), want) {
		t.Errorf("svc.fish source line not quoted;\n got: %s\nwant substring: %s", data, want)
	}
}

// TestAddSourceLine_zshQuotesPath pins that the zsh/bash source line quotes
// the rc path (spaces in the home dir must not break the line). Item 6.
func TestAddSourceLine_zshQuotesPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := config.NewConfig()
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg)
	rcPath := cfg.RcFilePath()

	if err := m.addSourceLine("zsh"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	want := `[[ -f "` + rcPath + `" ]] && source "` + rcPath + `"`
	if !strings.Contains(string(data), want) {
		t.Errorf("zsh source line not quoted;\n got: %s\nwant substring: %s", data, want)
	}
}

// --- backup rotation (item 9) ---

// TestBackupFile_rotates pins item 9: the second backup must not clobber the
// first — the previous .svc.bak is rotated to .svc.bak.1 (one generation).
func TestBackupFile_rotates(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".zshrc")

	// Generation 1: original content backed up.
	if err := os.WriteFile(target, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	backupFile(target)
	bak := target + ".svc.bak"
	if got, _ := os.ReadFile(bak); string(got) != "v1" {
		t.Fatalf("first backup = %q; want v1", got)
	}

	// Generation 2: v1 backup rotates to .svc.bak.1, fresh backup is v2.
	if err := os.WriteFile(target, []byte("v2"), 0644); err != nil {
		t.Fatal(err)
	}
	backupFile(target)
	if got, _ := os.ReadFile(bak); string(got) != "v2" {
		t.Errorf("current backup = %q; want v2", got)
	}
	if got, err := os.ReadFile(bak + ".1"); err != nil || string(got) != "v1" {
		t.Errorf("rotated backup = %q (err=%v); want v1", got, err)
	}

	// Generation 3: only ONE rotation level is kept (.bak.1 is overwritten).
	if err := os.WriteFile(target, []byte("v3"), 0644); err != nil {
		t.Fatal(err)
	}
	backupFile(target)
	if got, _ := os.ReadFile(bak); string(got) != "v3" {
		t.Errorf("current backup = %q; want v3", got)
	}
	if got, _ := os.ReadFile(bak + ".1"); string(got) != "v2" {
		t.Errorf("rotated backup = %q; want v2 (one generation only)", got)
	}
}

func TestUpgradeLegacySourceLine(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	rcPath := "/home/u/.svc.rc"
	legacy := fmt.Sprintf("[[ -f %s ]] && source %s", rcPath, rcPath)
	quoted := fmt.Sprintf(`[[ -f "%s" ]] && source "%s"`, rcPath, rcPath)

	// Legacy line present, quoted absent -> legacy removed.
	os.WriteFile(rc, []byte("# user stuff\n"+legacy+"\nalias x=y\n"), 0644)
	upgradeLegacySourceLine(rc, legacy, quoted)
	got, _ := os.ReadFile(rc)
	if strings.Contains(string(got), legacy) {
		t.Errorf("legacy line not removed:\n%s", got)
	}
	if !strings.Contains(string(got), "alias x=y") {
		t.Errorf("unrelated content lost:\n%s", got)
	}

	// Quoted line already present -> nothing touched.
	content := "# user stuff\n" + quoted + "\n"
	os.WriteFile(rc, []byte(content), 0644)
	upgradeLegacySourceLine(rc, legacy, quoted)
	got, _ = os.ReadFile(rc)
	if string(got) != content {
		t.Errorf("file modified although quoted line present:\n%s", got)
	}

	// Missing file -> no error, no creation.
	upgradeLegacySourceLine(filepath.Join(dir, "missing"), legacy, quoted)
	if _, err := os.Stat(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Errorf("missing file was created")
	}
}
