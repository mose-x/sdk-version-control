package shim

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestCommandAliases pins the python3 -> python alias so `python3` resolves
// to the real python binary on Windows (CPython ships python.exe, no python3).
func TestCommandAliases(t *testing.T) {
	alias, ok := commandAliases["python3"]
	if !ok || alias != "python" {
		t.Fatalf(`commandAliases["python3"] = %q, %v; want "python", true`, alias, ok)
	}
}

// TestIsDeniedEnvVar pins O10: the env-var denylist must reject keys that can
// hijack the child process (LD_PRELOAD, DYLD_INSERT_LIBRARIES, BASH_ENV, ...)
// or break the shim system itself (PATH is shim-managed). Matching is
// case-insensitive because Windows env keys are case-insensitive and because
// an attacker who can write shims.json might try Path= or path= as a bypass.
func TestIsDeniedEnvVar(t *testing.T) {
	denied := []string{
		"LD_PRELOAD", "ld_preload", "Ld_Preload",
		"LD_LIBRARY_PATH",
		"DYLD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES",
		"IFS", "ENV", "BASH_ENV", "PS1", "SHELLOPTS",
		"PATH", "Path", "path",
		// Item 7: interpreter/tool hooks that execute attacker-controlled
		// code or steer module/command resolution at program startup.
		"NODE_OPTIONS", "NPM_CONFIG_PREFIX",
		"GIT_SSH_COMMAND", "GIT_EXEC_PATH",
		"PYTHONSTARTUP", "PYTHONPATH", "pythonpath",
		"PERL5LIB", "PERL5OPT",
		"RUBYLIB", "RUBYOPT",
		"CDPATH", "BROWSER",
	}
	for _, k := range denied {
		if !isDeniedEnvVar(k) {
			t.Errorf("isDeniedEnvVar(%q) = false; want true (denylisted)", k)
		}
	}
	allowed := []string{"JAVA_HOME", "GOROOT", "ANDROID_HOME", "FLUTTER_ROOT", "CARGO_HOME"}
	for _, k := range allowed {
		if isDeniedEnvVar(k) {
			t.Errorf("isDeniedEnvVar(%q) = true; want false (allow)", k)
		}
	}
}

// TestLookupCommand covers O12: on Windows command-name lookup is
// case-insensitive (a hardlink to NODE.exe is the same as node.exe; cmd.exe
// and batch are case-insensitive). On Unix it is a direct (case-sensitive)
// map lookup. The platform-awareness is exercised via runtime.GOOS so the
// test passes on every CI OS.
func TestLookupCommand(t *testing.T) {
	m := map[string]string{
		"node":    "nodejs",
		"python3": "python",
	}
	// Exact match works on every platform.
	if v, ok := lookupCommand(m, "node"); !ok || v != "nodejs" {
		t.Errorf(`lookupCommand(node) = (%q,%v); want ("nodejs", true)`, v, ok)
	}
	if v, ok := lookupCommand(m, "python3"); !ok || v != "python" {
		t.Errorf(`lookupCommand(python3) = (%q,%v); want ("python", true)`, v, ok)
	}
	// Missing key returns false on every platform.
	if _, ok := lookupCommand(m, "rustc"); ok {
		t.Error(`lookupCommand(rustc) = true; want false (not registered)`)
	}
	// Case-insensitive matching is required on Windows.
	if runtime.GOOS == "windows" {
		if v, ok := lookupCommand(m, "NODE"); !ok || v != "nodejs" {
			t.Errorf(`lookupCommand(NODE) on Windows = (%q,%v); want ("nodejs", true)`, v, ok)
		}
		if v, ok := lookupCommand(m, "Python3"); !ok || v != "python" {
			t.Errorf(`lookupCommand(Python3) on Windows = (%q,%v); want ("python", true)`, v, ok)
		}
	} else {
		// On Unix, uppercase NODE must NOT match node.
		if _, ok := lookupCommand(m, "NODE"); ok {
			t.Error(`lookupCommand(NODE) on Unix = true; want false (Unix is case-sensitive)`)
		}
	}
}

// TestResolveRealBinaryMulti_python3AliasFallback simulates shim.Run's alias
// fallback: with a "python" binary present but no "python3", a direct lookup
// of python3 fails, and the alias (python3 -> python) resolves to python.
func TestResolveRealBinaryMulti_python3AliasFallback(t *testing.T) {
	versionDir := t.TempDir()
	pyName := "python"
	if runtime.GOOS == "windows" {
		pyName = "python.exe"
	}
	if err := os.WriteFile(filepath.Join(versionDir, pyName), []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}
	// Direct lookup of python3 must fail (no python3 binary on disk).
	if got := resolveRealBinaryMulti(versionDir, []string{""}, "python3"); got != "" {
		t.Fatalf(`resolveRealBinaryMulti(python3) = %q; want "" (not found)`, got)
	}
	// Alias fallback: python3 -> python resolves to the real python binary.
	alias, ok := commandAliases["python3"]
	if !ok {
		t.Fatal("commandAliases missing python3 entry")
	}
	got := resolveRealBinaryMulti(versionDir, []string{""}, alias)
	want := filepath.Join(versionDir, pyName)
	if got != want {
		t.Fatalf(`resolveRealBinaryMulti(alias=%q) = %q; want %q`, alias, got, want)
	}
}

// redirectHome points os.UserHomeDir at a temp dir for the duration of the
// test (HOME on Unix, USERPROFILE on Windows; both are set so the test is
// platform-agnostic).
func redirectHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func writeSettings(t *testing.T, svcDir string, installPath string) {
	t.Helper()
	data, err := json.Marshal(settings{InstallPath: installPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "settings.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

// TestResolveSvcHome pins L14: the settings.json installPath is validated
// before use. A missing/non-directory installPath must fall back to the
// default ~/.svc (with a diagnostic) instead of being used blindly — blind
// use made every later config read fail with confusing errors.
func TestResolveSvcHome(t *testing.T) {
	home := redirectHome(t)
	defaultDir := filepath.Join(home, ".svc")
	if err := os.MkdirAll(defaultDir, 0755); err != nil {
		t.Fatal(err)
	}

	// No settings.json -> default.
	got, err := resolveSvcHome()
	if err != nil || got != defaultDir {
		t.Fatalf("no settings.json: resolveSvcHome = (%q, %v); want (%q, nil)", got, err, defaultDir)
	}

	// Valid installPath (existing directory) is used.
	custom := t.TempDir()
	writeSettings(t, defaultDir, custom)
	got, err = resolveSvcHome()
	if err != nil || got != custom {
		t.Fatalf("valid installPath: resolveSvcHome = (%q, %v); want (%q, nil)", got, err, custom)
	}

	// Nonexistent installPath -> fallback to default.
	writeSettings(t, defaultDir, filepath.Join(home, "definitely-missing-dir"))
	got, err = resolveSvcHome()
	if err != nil || got != defaultDir {
		t.Fatalf("missing installPath: resolveSvcHome = (%q, %v); want (%q, nil)", got, err, defaultDir)
	}

	// installPath pointing at a FILE (not a directory) -> fallback.
	filePath := filepath.Join(home, "not-a-dir.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, defaultDir, filePath)
	got, err = resolveSvcHome()
	if err != nil || got != defaultDir {
		t.Fatalf("file installPath: resolveSvcHome = (%q, %v); want (%q, nil)", got, err, defaultDir)
	}

	// Relative installPath is resolved against the working directory via
	// filepath.Abs; if the resolved path doesn't exist, fall back.
	writeSettings(t, defaultDir, "relative-missing")
	got, err = resolveSvcHome()
	if err != nil || got != defaultDir {
		t.Fatalf("relative missing installPath: resolveSvcHome = (%q, %v); want (%q, nil)", got, err, defaultDir)
	}
}

// TestResolveSvcHome_parseError covers the existing fallback for a corrupt
// settings.json (parse error -> default dir, no crash).
func TestResolveSvcHome_parseError(t *testing.T) {
	home := redirectHome(t)
	defaultDir := filepath.Join(home, ".svc")
	if err := os.MkdirAll(defaultDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaultDir, "settings.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSvcHome()
	if err != nil || got != defaultDir {
		t.Fatalf("corrupt settings.json: resolveSvcHome = (%q, %v); want (%q, nil)", got, err, defaultDir)
	}
}
