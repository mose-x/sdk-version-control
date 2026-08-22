package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"sdk_version_control/internal/extractor"
)

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

// TestExtractorFailureDoesNotTouchTargetDir covers the extraction-failure half
// of InstallSdk's atomic-install pattern (sdk_ops.go: extract into
// versionDir+".new", then rename into place only on success — M4 fix).
//
// Scope note (honest coverage): InstallSdk itself cannot be unit-tested here
// (it needs the fetcher registry, downloader, Wails context, and network),
// and the rename/swap sequence is inline in InstallSdk rather than an
// isolated function, so this test exercises the real production behavior the
// pattern depends on: extractor.Extract returns an error for a corrupt
// archive. Per InstallSdk's control flow, that error occurs before any
// os.Rename touches the existing version directory, so the old directory must
// remain intact. The load-bearing assertion is the extraction error itself.
func TestExtractorFailureDoesNotTouchTargetDir(t *testing.T) {
	baseDir := t.TempDir()
	versionDir := filepath.Join(baseDir, "v1.0")
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		t.Fatal(err)
	}
	markerContent := []byte("old version marker")
	if err := os.WriteFile(filepath.Join(versionDir, "marker.txt"), markerContent, 0644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		archive string // filename whose extension selects the extractor
		content []byte // corrupt payload guaranteed to fail extraction
	}{
		{"corrupt tar.gz", "sdk.tar.gz", []byte("this is not gzip data")},
		{"corrupt zip", "sdk.zip", []byte("this is not a zip archive")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			archivePath := filepath.Join(baseDir, tc.archive)
			if err := os.WriteFile(archivePath, tc.content, 0644); err != nil {
				t.Fatal(err)
			}

			// InstallSdk's sequence: extract into a sibling temp dir first.
			tmpDir := versionDir + ".new"
			os.RemoveAll(tmpDir)
			if err := os.MkdirAll(tmpDir, 0755); err != nil {
				t.Fatal(err)
			}
			ext, err := extractor.NewExtractor(tc.archive)
			if err != nil {
				t.Fatalf("NewExtractor(%q) failed: %v", tc.archive, err)
			}
			extractErr := ext.Extract(archivePath, tmpDir)

			// Load-bearing assertion: a corrupt archive must fail extraction.
			// InstallSdk's swap (os.Rename of the version dir) only runs after
			// a successful extraction, so this error is what keeps the old
			// version directory untouched.
			if extractErr == nil {
				os.RemoveAll(tmpDir)
				t.Fatal("expected extraction of corrupt archive to fail, got nil")
			}

			// InstallSdk's error path: remove the temp dir, never the target.
			os.RemoveAll(tmpDir)
			if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
				t.Errorf("temp dir %q still present after failed extraction", tmpDir)
			}

			// Old version directory must still exist with original content.
			data, err := os.ReadFile(filepath.Join(versionDir, "marker.txt"))
			if err != nil {
				t.Fatalf("old version directory was destroyed on extraction failure: %v", err)
			}
			if string(data) != string(markerContent) {
				t.Fatalf("old version content corrupted: got %q, want %q", data, markerContent)
			}
		})
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
