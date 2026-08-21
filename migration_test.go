package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sdk_version_control/internal/config"
)

// TestValidateMigrationPaths is the table-driven guard test for migration
// target validation: existing targets (directory OR plain file) and nested
// source/target pairs must be rejected; disjoint absolute paths must pass.
func TestValidateMigrationPaths(t *testing.T) {
	base := t.TempDir()

	oldDir := filepath.Join(base, "old")
	if err := os.MkdirAll(filepath.Join(oldDir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	existingDir := filepath.Join(base, "existing-dir")
	if err := os.MkdirAll(existingDir, 0755); err != nil {
		t.Fatal(err)
	}
	existingFile := filepath.Join(base, "existing-file")
	if err := os.WriteFile(existingFile, []byte("user data"), 0644); err != nil {
		t.Fatal(err)
	}
	// For the "source nested inside target" case: use paths that do not
	// exist on disk so the lexical nesting branch (not the existence check)
	// is what rejects them.
	ghostOuter := filepath.Join(base, "ghost-outer")

	tests := []struct {
		name    string
		oldDir  string
		newDir  string
		wantErr string // substring expected in the error; empty means success
	}{
		{"normal nonexistent target ok", oldDir, filepath.Join(base, "new-target"), ""},
		{"existing directory rejected", oldDir, existingDir, "already exists"},
		{"existing plain file rejected", oldDir, existingFile, "already exists"},
		{"target nested in source rejected", oldDir, filepath.Join(oldDir, "nested"), "nested"},
		{"target deep-nested in source rejected", oldDir, filepath.Join(oldDir, "sub", "deep"), "nested"},
		{"same path rejected", filepath.Join(base, "old2"), filepath.Join(base, "old2"), "nested"},
		{"source nested in target rejected", filepath.Join(ghostOuter, "inner"), ghostOuter, "nested"},
		{"sibling with shared prefix ok", oldDir, filepath.Join(base, "old2"), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMigrationPaths(tt.oldDir, tt.newDir)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected success, got error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestMigrateInstallPath_RejectsExistingFile verifies the end-to-end guard:
// migrating onto an existing regular file must fail BEFORE any copy/cleanup,
// so the user's pre-existing file survives (previously the M10 cleanup could
// RemoveAll it after CopyDir failed).
func TestMigrateInstallPath_RejectsExistingFile(t *testing.T) {
	base := t.TempDir()
	oldDir := filepath.Join(base, "old")
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "target-file")
	if err := os.WriteFile(target, []byte("user data"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	cfg.SetSvcDir(oldDir)
	app := &App{cfg: cfg}

	err := app.MigrateInstallPath(target)
	if err == nil {
		t.Fatal("expected error when migrating onto an existing file, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}
	// The pre-existing user file must be untouched.
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("pre-existing target file disappeared: %v", err)
	}
	if string(data) != "user data" {
		t.Errorf("target file content changed: %q", data)
	}
}

// TestMigrateInstallPath_RejectsNestedTarget verifies the end-to-end guard:
// migrating into a subdirectory of the current install dir must be rejected
// (the final RemoveAll(oldDir) would otherwise delete the migrated data).
func TestMigrateInstallPath_RejectsNestedTarget(t *testing.T) {
	base := t.TempDir()
	oldDir := filepath.Join(base, "old")
	if err := os.MkdirAll(filepath.Join(oldDir, "keep"), 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	cfg.SetSvcDir(oldDir)
	app := &App{cfg: cfg}

	err := app.MigrateInstallPath(filepath.Join(oldDir, "nested"))
	if err == nil {
		t.Fatal("expected error for nested target, got nil")
	}
	if !strings.Contains(err.Error(), "nested") {
		t.Errorf("unexpected error: %v", err)
	}
	// Old directory content must be intact.
	if _, err := os.Stat(filepath.Join(oldDir, "keep")); err != nil {
		t.Errorf("old directory content lost: %v", err)
	}
}

func TestIsSystemPath_AllowsTempDir(t *testing.T) {
	// macOS per-user temp lives under /var/folders and must NOT be treated
	// as a system directory (regression: CI macOS runners failed migration
	// tests because t.TempDir() matched the /var system root).
	if isSystemPath(filepath.Join(os.TempDir(), "svc-target")) {
		t.Errorf("path under os.TempDir() (%s) wrongly flagged as system path", os.TempDir())
	}
	if runtime.GOOS == "windows" {
		if !isSystemPath(`C:\Windows\System32`) {
			t.Error(`C:\Windows\System32 must be a system path`)
		}
	} else {
		if !isSystemPath("/usr") {
			t.Error("/usr must be a system path")
		}
	}
}
