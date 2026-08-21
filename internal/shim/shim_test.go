package shim

import (
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
	}
	for _, k := range denied {
		if !isDeniedEnvVar(k) {
			t.Errorf("isDeniedEnvVar(%q) = false; want true (denylisted)", k)
		}
	}
	allowed := []string{"JAVA_HOME", "GOROOT", "ANDROID_HOME", "FLUTTER_ROOT", "PYTHONPATH"}
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
