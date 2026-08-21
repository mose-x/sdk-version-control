package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestDownloadSingle_PartRename verifies that downloadSingle writes to a .part
// temp file and atomically renames to the final destination on success.
func TestDownloadSingle_PartRename(t *testing.T) {
	payload := make([]byte, 256*1024) // 256KB
	for i := range payload {
		payload[i] = byte(i)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		w.WriteHeader(http.StatusOK)
		w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "testfile.bin")

	d := NewDownloader()
	err := d.Download(context.Background(), srv.URL, destPath, nil, ProxyConfig{Enabled: false}, 1)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify final file exists and has correct content
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	if len(data) != len(payload) {
		t.Fatalf("file size = %d, want %d", len(data), len(payload))
	}
	for i := range payload {
		if data[i] != payload[i] {
			t.Fatalf("byte %d mismatch: got %d, want %d", i, data[i], payload[i])
		}
	}

	// Verify .part file does NOT exist (was renamed away)
	if _, err := os.Stat(destPath + ".part"); !os.IsNotExist(err) {
		t.Errorf(".part file should not exist after successful download")
	}
}

// TestDownloadSingle_ErrorCleansPartFile verifies that .part is cleaned up on error.
func TestDownloadSingle_ErrorCleansPartFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "testfile.bin")

	d := NewDownloader()
	err := d.Download(context.Background(), srv.URL, destPath, nil, ProxyConfig{Enabled: false}, 1)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}

	if _, err := os.Stat(destPath + ".part"); !os.IsNotExist(err) {
		t.Errorf(".part file should be cleaned up on error")
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Errorf("destPath should not exist on error")
	}
}

// TestDownloadMultiThread_PartRename verifies that downloadMultiThread writes to
// .part temp file, goroutines write to .part (not destPath), and atomically
// renames to destPath on success. This is the regression test for the C1 bug
// where goroutines used destPath instead of partPath.
func TestDownloadMultiThread_PartRename(t *testing.T) {
	payload := make([]byte, 10*1024*1024) // 10MB (> minMultiThreadSize)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" {
			// HEAD request
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Parse Range: bytes=start-end
		var start, end int64
		fmt.Sscanf(rangeHdr, "bytes=%d-%d", &start, &end)
		if end >= int64(len(payload)) {
			end = int64(len(payload)) - 1
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(payload[start : end+1])
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "testfile.bin")

	d := NewDownloader()
	err := d.Download(context.Background(), srv.URL, destPath, nil, ProxyConfig{Enabled: false}, 4)
	if err != nil {
		t.Fatalf("Multi-thread download failed: %v", err)
	}

	// Verify final file exists with correct content
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	if len(data) != len(payload) {
		t.Fatalf("file size = %d, want %d", len(data), len(payload))
	}
	for i := 0; i < len(payload); i++ {
		if data[i] != payload[i] {
			t.Fatalf("byte %d mismatch: got %d, want %d", i, data[i], payload[i])
		}
	}

	// Verify .part file does NOT exist
	if _, err := os.Stat(destPath + ".part"); !os.IsNotExist(err) {
		t.Errorf(".part file should not exist after successful download")
	}
}

// TestDownloadMultiThread_ErrorCleansPartFile verifies multi-thread .part cleanup on error.
func TestDownloadMultiThread_ErrorCleansPartFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "testfile.bin")

	d := NewDownloader()
	err := d.Download(context.Background(), srv.URL, destPath, nil, ProxyConfig{Enabled: false}, 4)
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}

	if _, err := os.Stat(destPath + ".part"); !os.IsNotExist(err) {
		t.Errorf(".part file should be cleaned up on error")
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Errorf("destPath should not exist on error")
	}
}

// TestDownload_FallbackToSingleThread verifies that when a server doesn't support
// Range requests, the downloader falls back to single-thread and still succeeds.
func TestDownload_FallbackToSingleThread(t *testing.T) {
	payload := make([]byte, 10*1024*1024) // 10MB
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return 200 with full body (no Range support)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		w.WriteHeader(http.StatusOK)
		if r.Method == "GET" {
			io.Copy(w, &byteReader{data: payload})
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "testfile.bin")

	d := NewDownloader()
	err := d.Download(context.Background(), srv.URL, destPath, nil, ProxyConfig{Enabled: false}, 4)
	if err != nil {
		t.Fatalf("Download with fallback failed: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	if len(data) != len(payload) {
		t.Fatalf("file size = %d, want %d", len(data), len(payload))
	}
}

// byteReader is a simple reader for fixed byte data.
type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
