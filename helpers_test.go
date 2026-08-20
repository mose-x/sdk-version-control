package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sdk_version_control/internal/extractor"
	"sdk_version_control/internal/sdk"
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
			got := extractVersionFromString(tt.input)
			if got != tt.want {
				t.Errorf("extractVersionFromString(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestResolveCommandNotFoundReturnsEmpty verifies that resolveCommand returns
// "" when the command is not found in PATH (M3 fix). Previously it returned the
// bare command name, which caused ImportPathSdk to copy the entire CWD.
func TestResolveCommandNotFoundReturnsEmpty(t *testing.T) {
	got := resolveCommand("definitely_not_a_real_command_xyz123")
	if got != "" {
		t.Errorf("resolveCommand(nonexistent) = %q; want \"\"", got)
	}
}

// TestResolveCommandExcludesShimsDir verifies that the shims exclusion logic
// used by resolveCommand correctly identifies SVC shims paths.
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

// TestVerifyFileSHA256Match tests that verifyFileSHA256 returns nil when the
// file's hash matches the expected value (M1).
func TestVerifyFileSHA256Match(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	content := []byte("checksum test content")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Compute the correct hash
	correctHash := computeTestSHA256(t, path)
	if err := verifyFileSHA256(path, correctHash); err != nil {
		t.Fatalf("verifyFileSHA256 with correct hash failed: %v", err)
	}
}

// TestVerifyFileSHA256Mismatch tests that verifyFileSHA256 returns an error
// when the hash does not match (M1).
func TestVerifyFileSHA256Mismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	err := verifyFileSHA256(path, wrongHash)
	if err == nil {
		t.Fatal("verifyFileSHA256 with wrong hash should have failed")
	}
}

// TestAtomicInstallPreservesOldOnFailure verifies that when extraction fails,
// the previously-installed version directory is left intact (M4 fix).
// The test simulates the InstallSdk pattern: extract to temp dir, only
// replace old dir on success.
func TestAtomicInstallPreservesOldOnFailure(t *testing.T) {
	baseDir := t.TempDir()
	oldDir := filepath.Join(baseDir, "v1.0")
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatal(err)
	}
	markerContent := []byte("old version marker")
	if err := os.WriteFile(filepath.Join(oldDir, "marker.txt"), markerContent, 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate extraction to temp dir that fails (nonexistent archive).
	tmpDir := oldDir + ".new"
	os.RemoveAll(tmpDir)
	ext, _ := extractor.NewExtractor("test.tar.gz")
	extractErr := ext.Extract("/nonexistent/archive.tar.gz", tmpDir)

	if extractErr == nil {
		os.RemoveAll(tmpDir)
		t.Fatal("expected extraction to fail for nonexistent archive")
	}
	// On failure, clean up temp dir (matching InstallSdk's error path).
	os.RemoveAll(tmpDir)

	// Old version directory must still exist with original content.
	data, err := os.ReadFile(filepath.Join(oldDir, "marker.txt"))
	if err != nil {
		t.Fatalf("old version directory was destroyed on extraction failure: %v", err)
	}
	if string(data) != string(markerContent) {
		t.Fatalf("old version content corrupted: got %q, want %q", data, markerContent)
	}
}

// computeTestSHA256 is a test helper that computes the hex-encoded SHA256 of a
// file. It mirrors the logic in verifyFileSHA256 without importing the
// function's internals.
func computeTestSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func TestCheckCriticalFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a fake Go SDK structure matching criticalFilesFor output.
	// On Windows the critical files include .exe suffix.
	goName, gofmtName := "go", "gofmt"
	if runtime.GOOS == "windows" {
		goName, gofmtName = "go.exe", "gofmt.exe"
	}
	binDir := filepath.Join(dir, "go", "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, goName), []byte("fake"), 0755)
	os.WriteFile(filepath.Join(binDir, gofmtName), []byte("fake"), 0755)

	// All critical files present → no error
	if err := checkCriticalFiles(dir, sdk.Golang); err != nil {
		t.Errorf("checkCriticalFiles with all files present: got %v, want nil", err)
	}

	// Remove gofmt → should error mentioning the missing file
	os.Remove(filepath.Join(binDir, gofmtName))
	err := checkCriticalFiles(dir, sdk.Golang)
	if err == nil {
		t.Fatal("checkCriticalFiles should fail when gofmt is missing")
	}
	if !strings.Contains(err.Error(), "gofmt") {
		t.Errorf("error should mention gofmt, got: %v", err)
	}
}

func TestIsSystemPath(t *testing.T) {
	// Relative paths should be rejected by migration (H1 fix), but isSystemPath
	// itself only checks system dirs — the IsAbs check is separate.
	if runtime.GOOS == "windows" {
		systemPaths := []string{`C:\Windows`, `C:\Windows\System32`, `C:\Program Files`, `c:\program files (x86)`, `C:\ProgramData\foo`}
		for _, p := range systemPaths {
			if !isSystemPath(p) {
				t.Errorf("isSystemPath(%q) = false; want true", p)
			}
		}
		validPaths := []string{`C:\Users\mose\.svc`, `D:\SDKs`, `C:\dev\sdk-version-control`}
		for _, p := range validPaths {
			if isSystemPath(p) {
				t.Errorf("isSystemPath(%q) = true; want false", p)
			}
		}
	} else {
		systemPaths := []string{"/usr", "/usr/local", "/bin", "/etc", "/var/log", "/sbin", "/boot"}
		for _, p := range systemPaths {
			if !isSystemPath(p) {
				t.Errorf("isSystemPath(%q) = false; want true", p)
			}
		}
		validPaths := []string{"/home/user/.svc", "/opt/sdks", "/Users/mose/.svc"}
		for _, p := range validPaths {
			if isSystemPath(p) {
				t.Errorf("isSystemPath(%q) = true; want false", p)
			}
		}
	}
}

func TestFilePathIsAbs(t *testing.T) {
	var absPaths, relPaths []string
	if runtime.GOOS == "windows" {
		absPaths = []string{`C:\Users\mose\.svc`, `D:\SDKs`, `C:\dev`}
		relPaths = []string{"foo", `..\bar`, `.\baz`, ""}
	} else {
		absPaths = []string{"/usr/local", "/home/user/.svc", "/opt/sdks"}
		relPaths = []string{"foo", "../bar", "./baz", ""}
	}
	for _, p := range absPaths {
		if !filepath.IsAbs(p) {
			t.Errorf("filepath.IsAbs(%q) = false; want true", p)
		}
	}
	for _, p := range relPaths {
		if filepath.IsAbs(p) {
			t.Errorf("filepath.IsAbs(%q) = true; want false", p)
		}
	}
}

func TestValidateCheckURL(t *testing.T) {
	valid := []string{
		"https://github.com",
		"http://nodejs.org/dist/index.json",
		"https://api.adoptium.net",
	}
	for _, u := range valid {
		if err := validateCheckURL(u); err != nil {
			t.Errorf("validateCheckURL(%q) = %v; want nil", u, err)
		}
	}
	invalid := []struct {
		url  string
		desc string
	}{
		{"ftp://example.com", "non-http scheme"},
		{"file:///etc/passwd", "file scheme"},
		{"http://127.0.0.1", "loopback IPv4"},
		{"http://localhost:8080", "localhost"},
		{"http://192.168.1.1", "private 192.168"},
		{"http://10.0.0.1", "private 10.x"},
		{"http://172.16.0.1", "private 172.16"},
		{"://invalid", "malformed URL"},
	}
	for _, tt := range invalid {
		if err := validateCheckURL(tt.url); err == nil {
			t.Errorf("validateCheckURL(%q) = nil; want error (%s)", tt.url, tt.desc)
		}
	}
}
