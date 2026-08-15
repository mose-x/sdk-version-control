package extractor

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractTarHardlink creates a tar with a regular file and a hardlink to it,
// then extracts and verifies both files exist and the hardlink resolves to the
// same content.
func TestExtractTarHardlink(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.tar.gz")

	// Create a tar.gz with a regular file and a hardlink entry.
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	content := []byte("hello hardlink")

	// Regular file
	hdr := &tar.Header{
		Name: "original.txt",
		Mode: 0644,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	tw.Write(content)

	// Hardlink to original.txt
	linkHdr := &tar.Header{
		Name:     "linked.txt",
		Typeflag: tar.TypeLink,
		Linkname: "original.txt",
		Mode:     0644,
		Size:     0,
	}
	if err := tw.WriteHeader(linkHdr); err != nil {
		t.Fatal(err)
	}

	tw.Close()
	gw.Close()
	f.Close()

	// Extract
	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	ext := &TarGzExtractor{}
	if err := ext.Extract(archivePath, destDir); err != nil {
		t.Fatalf("extraction failed: %v", err)
	}

	// Verify original file
	origData, err := os.ReadFile(filepath.Join(destDir, "original.txt"))
	if err != nil {
		t.Fatalf("original file missing: %v", err)
	}
	if string(origData) != string(content) {
		t.Fatalf("original content = %q, want %q", origData, content)
	}

	// Verify hardlink exists and has same content
	linkData, err := os.ReadFile(filepath.Join(destDir, "linked.txt"))
	if err != nil {
		t.Fatalf("hardlink file missing: %v", err)
	}
	if string(linkData) != string(content) {
		t.Fatalf("hardlink content = %q, want %q", linkData, content)
	}
}

// TestExtractTarHardlinkEscapingRejected creates a tar with a hardlink whose
// Linkname escapes the dest dir, and verifies extraction is rejected.
func TestExtractTarHardlinkEscapingRejected(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "test.tar.gz")

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// Hardlink with escaping linkname (../../../etc/passwd)
	linkHdr := &tar.Header{
		Name:     "evil.txt",
		Typeflag: tar.TypeLink,
		Linkname: "../../../etc/passwd",
		Mode:     0644,
		Size:     0,
	}
	if err := tw.WriteHeader(linkHdr); err != nil {
		t.Fatal(err)
	}

	tw.Close()
	gw.Close()
	f.Close()

	destDir := filepath.Join(tmpDir, "dest")
	os.MkdirAll(destDir, 0755)
	ext := &TarGzExtractor{}
	err = ext.Extract(archivePath, destDir)
	if err == nil {
		t.Fatal("extraction should have failed for escaping hardlink")
	}
	if !strings.Contains(err.Error(), "invalid hardlink") {
		t.Fatalf("error should mention invalid hardlink, got: %v", err)
	}
}
