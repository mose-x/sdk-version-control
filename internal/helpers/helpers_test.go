package helpers

import (
	"path/filepath"
	"testing"

	"svc/internal/sdk"
)

func TestExtractVersionFromString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"rustc version", "rustc 1.75.0 (cf89e91d4 2023-12-18)\n", "1.75.0"},
		{"go version multiline", "go version go1.21.5 darwin/arm64\n", "1.21.5"},
		{"node version", "v20.10.0\n", "20.10.0"},
		{"python version", "Python 3.13.1\n", "3.13.1"},
		{"empty output", "", ""},
		{"no version pattern", "/usr/local/bin", ""},
		{"sysroot path no version", "/usr\n", ""},
		{"two-digit minor", "rustc 1.80.1 (35 compilercentricities)\n", "1.80.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractVersionFromString(tt.input)
			if got != tt.want {
				t.Errorf("ExtractVersionFromString(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestResolveCommandNotFoundReturnsEmpty verifies that ResolveCommand returns
// "" when the command is not found in PATH (M3 fix). Previously it returned the
// bare command name, which caused ImportPathSdk to copy the entire CWD.
func TestResolveCommandNotFoundReturnsEmpty(t *testing.T) {
	got := ResolveCommand("definitely_not_a_real_command_xyz123")
	if got != "" {
		t.Errorf("ResolveCommand(nonexistent) = %q; want \"\"", got)
	}
}

// TestResolveCommandExcludesShimsDir verifies that the shims exclusion logic
// used by ResolveCommand correctly identifies SVC shims paths.
func TestResolveCommandExcludesShimsDir(t *testing.T) {
	shimsDir := sdk.SvcShimsDir()
	if shimsDir == "" {
		t.Fatal("SvcShimsDir() returned empty string")
	}

	// IsShimsDirEntry: a PATH entry equal to shimsDir should be excluded
	if !sdk.IsShimsDirEntry(shimsDir, shimsDir) {
		t.Errorf("IsShimsDirEntry(shimsDir, shimsDir) = false; want true")
	}
	// A different directory should NOT be excluded
	otherDir := filepath.Join(t.TempDir(), "other")
	if sdk.IsShimsDirEntry(otherDir, shimsDir) {
		t.Errorf("IsShimsDirEntry(otherDir, shimsDir) = true; want false")
	}

	// IsShimsPath: a binary inside shimsDir should be detected
	shimBinary := filepath.Join(shimsDir, "go.exe")
	if !sdk.IsShimsPath(shimBinary, shimsDir) {
		t.Errorf("IsShimsPath(%s, %s) = false; want true", shimBinary, shimsDir)
	}
	// A binary outside shimsDir should NOT be detected
	externalBinary := filepath.Join(otherDir, "go.exe")
	if sdk.IsShimsPath(externalBinary, shimsDir) {
		t.Errorf("IsShimsPath(%s, %s) = true; want false", externalBinary, shimsDir)
	}
}

// TestReplacePathEnv pins the PATH-override semantics: existing PATH
// entries (any case on the key) are dropped and the new value is appended.
func TestReplacePathEnv(t *testing.T) {
	env := []string{"A=1", "PATH=/old", "path=/other", "B=2"}
	out := ReplacePathEnv(env, "/new")
	want := []string{"A=1", "B=2", "PATH=/new"}
	if len(out) != len(want) {
		t.Fatalf("ReplacePathEnv = %v; want %v", out, want)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("ReplacePathEnv[%d] = %q; want %q", i, out[i], want[i])
		}
	}
}

// TestValidatePathSegment covers the guard used for every user-supplied path
// segment (happy path, traversal, separators, illegal chars, reserved names).
func TestValidatePathSegment(t *testing.T) {
	valid := []string{"go", "1.21.5", "my-sdk", "1..2", "a b"}
	for _, s := range valid {
		if err := ValidatePathSegment(s); err != nil {
			t.Errorf("ValidatePathSegment(%q) unexpected error: %v", s, err)
		}
	}
	invalid := []string{"", ".", "..", "../x", `a\b`, "a/b", "a\x00b", "a<b", "CON", "nul", "LPT1"}
	for _, s := range invalid {
		if err := ValidatePathSegment(s); err == nil {
			t.Errorf("ValidatePathSegment(%q) = nil; want error", s)
		}
	}
}
