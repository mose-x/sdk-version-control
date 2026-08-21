//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"sdk_version_control/internal/config"
)

// TestCleanTmpCache_RemoveError (Windows): a file held open without
// FILE_SHARE_DELETE cannot be removed while the handle is open, so
// CleanTmpCache must surface the failure instead of returning nil.
func TestCleanTmpCache_RemoveError(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "tmp")
	if err := os.MkdirAll(tmp, 0755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(tmp, "locked.txt")
	if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// Open the file for reading WITHOUT FILE_SHARE_DELETE: any delete attempt
	// while this handle is open fails with a sharing violation.
	utf16, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(utf16, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Skipf("could not obtain exclusive handle: %v", err)
	}
	defer syscall.CloseHandle(h)

	cfg := &config.Config{}
	cfg.SetSvcDir(dir)
	app := &App{cfg: cfg}
	if err := app.CleanTmpCache(); err == nil {
		t.Fatal("expected error when a cache entry is locked, got nil")
	}
}
