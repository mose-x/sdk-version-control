package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"sdk_version_control/internal/config"
)

// TestNoVersionsLeft_MissingDir: a non-existent SDK dir is the normal "no
// versions" state -> (true, nil), NOT an error (M7).
func TestNoVersionsLeft_MissingDir(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetSvcDir(t.TempDir())
	app := &App{cfg: cfg}
	left, err := app.noVersionsLeft("nodejs")
	if err != nil {
		t.Fatalf("expected no error for missing dir (IsNotExist), got %v", err)
	}
	if !left {
		t.Errorf("expected true (no versions) for missing dir, got false")
	}
}

// TestNoVersionsLeft_WithVersions: an SDK dir containing a version subdir ->
// (false, nil).
func TestNoVersionsLeft_WithVersions(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.SetSvcDir(dir)
	if err := os.MkdirAll(filepath.Join(dir, "nodejs", "v18.0.0"), 0755); err != nil {
		t.Fatal(err)
	}
	app := &App{cfg: cfg}
	left, err := app.noVersionsLeft("nodejs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if left {
		t.Errorf("expected false (versions present), got true")
	}
}

// TestNoVersionsLeft_ReadErrorPropagates is the core M7 regression test:
// when reading the SDK dir fails with a non-IsNotExist error (permission
// denied), noVersionsLeft must return (false, err) so the caller does NOT
// tear down shims under the mistaken belief that zero versions remain.
// Uses chmod 0000 to trigger EACCES on Unix; skipped on Windows (where
// triggering a non-IsNotExist ReadDir error is not reliable) and when
// running as root (chmod does not block root).
func TestNoVersionsLeft_ReadErrorPropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("triggering a non-IsNotExist ReadDir error is not reliable on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root; chmod 0000 does not block root from reading")
	}
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.SetSvcDir(dir)
	sdkDir := filepath.Join(dir, "nodejs")
	if err := os.MkdirAll(sdkDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sdkDir, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sdkDir, 0755) }) // restore so TempDir cleanup works
	app := &App{cfg: cfg}
	left, err := app.noVersionsLeft("nodejs")
	if err == nil {
		t.Fatalf("expected an error for unreadable dir, got nil; left=%v", left)
	}
	if left {
		t.Errorf("expected false (must not claim 0 remaining) on read error, got true")
	}
}

// TestCleanTmpCache_RemovesEntries: files and nested directories in the tmp
// cache are removed and nil is returned.
func TestCleanTmpCache_RemovesEntries(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "tmp")
	if err := os.MkdirAll(filepath.Join(tmp, "subdir", "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "a.zip"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "subdir", "nested", "f"), []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	cfg.SetSvcDir(dir)
	app := &App{cfg: cfg}
	if err := app.CleanTmpCache(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty tmp dir after clean, %d entries remain", len(entries))
	}
}

// TestCleanTmpCache_EmptyDir: an empty tmp cache cleans successfully.
func TestCleanTmpCache_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.SetSvcDir(dir)
	app := &App{cfg: cfg}
	if err := app.CleanTmpCache(); err != nil {
		t.Fatalf("unexpected error for empty cache: %v", err)
	}
}

// TestCleanTmpCache_MissingDir: an unreadable/non-existent tmp dir returns
// an error (the read failure is surfaced, not swallowed).
func TestCleanTmpCache_MissingDir(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetSvcDir(filepath.Join(t.TempDir(), "no-such-svc"))
	app := &App{cfg: cfg}
	if err := app.CleanTmpCache(); err == nil {
		t.Fatal("expected error for missing tmp dir, got nil")
	}
}
