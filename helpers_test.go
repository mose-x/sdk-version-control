package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sdk_version_control/internal/sdk"
)

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
