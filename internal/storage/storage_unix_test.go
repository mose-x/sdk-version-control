//go:build !windows

package storage

import (
	"os"
	"path/filepath"
	"testing"

	"svc/internal/config"
)

// TestCleanTmpCache_RemoveError (Unix): when a cache entry cannot be removed
// (directory chmod'd read-only while it still contains a file), CleanTmpCache
// must surface the error instead of silently returning nil. Skipped when
// running as root (chmod does not block root).
func TestCleanTmpCache_RemoveError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root; chmod does not block root from deleting")
	}
	dir := t.TempDir()
	tmp := filepath.Join(dir, "tmp")
	locked := filepath.Join(tmp, "locked")
	if err := os.MkdirAll(filepath.Join(locked, "inner"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "inner", "f"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// Read-only parent: entries inside cannot be unlinked -> RemoveAll fails.
	if err := os.Chmod(locked, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0755) }) // restore so TempDir cleanup works

	cfg := &config.Config{}
	cfg.SetSvcDir(dir)
	app := NewManager(cfg, nil, nil, nil, nil)
	if err := app.CleanTmpCache(); err == nil {
		t.Fatal("expected error when a cache entry cannot be removed, got nil")
	}
}
