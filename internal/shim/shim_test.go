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
