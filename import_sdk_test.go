package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sdk_version_control/internal/sdk"
)

// goExeNames returns the platform-specific executable basenames for the Go
// SDK critical files, mirroring criticalFilesFor(sdk.Golang).
func goExeNames() (goName, gofmtName string) {
	if runtime.GOOS == "windows" {
		return "go.exe", "gofmt.exe"
	}
	return "go", "gofmt"
}

// writeFlatGoLayout creates a FLAT Go SDK layout (bin/<go>, bin/<gofmt> with
// no go/ wrapper dir) under dir — the shape produced by importing a GOROOT
// directory directly or resolving `go` from the system PATH.
func writeFlatGoLayout(t *testing.T, dir string) {
	t.Helper()
	goName, gofmtName := goExeNames()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{goName, gofmtName} {
		if err := os.WriteFile(filepath.Join(binDir, n), []byte("fake"), 0755); err != nil {
			t.Fatal(err)
		}
	}
}

// writeWrappedGoLayout creates the post-alignment / download-install layout
// (go/bin/<go>, go/bin/<gofmt>) under dir.
func writeWrappedGoLayout(t *testing.T, dir string) {
	t.Helper()
	goName, gofmtName := goExeNames()
	binDir := filepath.Join(dir, "go", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{goName, gofmtName} {
		if err := os.WriteFile(filepath.Join(binDir, n), []byte("fake"), 0755); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAlignAndCheckCriticalFiles_FlatLayoutPassesAfterAlignment is the core
// regression test for the layer-2 ordering bug: a complete-but-flat Go layout
// used to be rejected with "SDK incomplete" because checkCriticalFiles ran
// BEFORE AlignImportLayout. The check must run on the aligned layout.
func TestAlignAndCheckCriticalFiles_FlatLayoutPassesAfterAlignment(t *testing.T) {
	dir := t.TempDir()
	writeFlatGoLayout(t, dir)

	// Sanity: the raw layer-2 check on the PRE-alignment layout fails. This
	// documents exactly the behavior that wrongly rejected flat imports when
	// the check ran too early.
	if err := checkCriticalFiles(dir, sdk.Golang); err == nil {
		t.Fatal("flat layout unexpectedly passed the pre-alignment critical check; test premise broken")
	}

	// The fixed order: align first, then check — must pass.
	binDirs := (&sdk.GolangFetcher{}).GetBinDirs()
	if err := alignAndCheckCriticalFiles(dir, binDirs, sdk.Golang); err != nil {
		t.Fatalf("alignAndCheckCriticalFiles on flat complete layout = %v; want nil", err)
	}

	// The wrapper dir must now exist (alignment really happened).
	goName, _ := goExeNames()
	if _, err := os.Stat(filepath.Join(dir, "go", "bin", goName)); err != nil {
		t.Errorf("aligned layout missing go/bin/%s: %v", goName, err)
	}
}

// TestAlignAndCheckCriticalFiles_WrappedLayoutNoOp verifies that an
// already-wrapped layout (archive imports, download-install shape) passes
// without being double-wrapped.
func TestAlignAndCheckCriticalFiles_WrappedLayoutNoOp(t *testing.T) {
	dir := t.TempDir()
	writeWrappedGoLayout(t, dir)

	binDirs := (&sdk.GolangFetcher{}).GetBinDirs()
	if err := alignAndCheckCriticalFiles(dir, binDirs, sdk.Golang); err != nil {
		t.Fatalf("alignAndCheckCriticalFiles on wrapped layout = %v; want nil", err)
	}
	// No double-wrapping: go/go must NOT exist.
	if _, err := os.Stat(filepath.Join(dir, "go", "go")); !os.IsNotExist(err) {
		t.Errorf("wrapped layout was double-wrapped (go/go exists, stat err = %v)", err)
	}
}

// TestAlignAndCheckCriticalFiles_IncompleteStillRejected verifies the layer-2
// check still rejects genuinely incomplete SDKs after alignment (the fix must
// not weaken verification).
func TestAlignAndCheckCriticalFiles_IncompleteStillRejected(t *testing.T) {
	dir := t.TempDir()
	goName, _ := goExeNames()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Only `go`, no `gofmt` -> incomplete.
	if err := os.WriteFile(filepath.Join(binDir, goName), []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}

	binDirs := (&sdk.GolangFetcher{}).GetBinDirs()
	err := alignAndCheckCriticalFiles(dir, binDirs, sdk.Golang)
	if err == nil {
		t.Fatal("alignAndCheckCriticalFiles should reject an incomplete SDK")
	}
	if !strings.Contains(err.Error(), "gofmt") {
		t.Errorf("error should mention the missing file (gofmt), got: %v", err)
	}
}

// TestCopyToTargetAtomically_AlignsBeforeVerification exercises the full copy
// path: a flat source is copied, aligned, checked and only then renamed into
// place; the verify callback observes the ALIGNED directory.
func TestCopyToTargetAtomically_AlignsBeforeVerification(t *testing.T) {
	src := t.TempDir()
	writeFlatGoLayout(t, src)

	base := t.TempDir()
	target := filepath.Join(base, "go", "1.21.5")
	binDirs := (&sdk.GolangFetcher{}).GetBinDirs()

	var verifiedDir string
	verify := func(dir string) error {
		verifiedDir = dir
		// The verify callback must see the aligned layout.
		goName, _ := goExeNames()
		if _, err := os.Stat(filepath.Join(dir, "go", "bin", goName)); err != nil {
			return fmt.Errorf("verify saw pre-alignment layout: %w", err)
		}
		return nil
	}

	if err := copyToTargetAtomically(src, target, binDirs, sdk.Golang, verify); err != nil {
		t.Fatalf("copyToTargetAtomically on flat complete layout = %v; want nil", err)
	}
	if verifiedDir != target+".new" {
		t.Errorf("verify callback ran on %q; want the temp dir %q", verifiedDir, target+".new")
	}

	goName, _ := goExeNames()
	if _, err := os.Stat(filepath.Join(target, "go", "bin", goName)); err != nil {
		t.Errorf("target missing aligned go/bin/%s: %v", goName, err)
	}
	// Temp siblings must be cleaned up.
	for _, leftover := range []string{target + ".new", target + ".old"} {
		if _, err := os.Stat(leftover); !os.IsNotExist(err) {
			t.Errorf("leftover temp dir %s still exists (stat err = %v)", leftover, err)
		}
	}
}

// TestCopyToTargetAtomically_RejectsIncompleteAfterAlignment verifies that an
// incomplete flat source is rejected by the post-alignment check WITHOUT
// touching the target location or leaving temp siblings behind.
func TestCopyToTargetAtomically_RejectsIncompleteAfterAlignment(t *testing.T) {
	src := t.TempDir()
	goName, _ := goExeNames()
	binDir := filepath.Join(src, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, goName), []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}
	// gofmt missing -> incomplete.

	base := t.TempDir()
	target := filepath.Join(base, "go", "1.21.5")
	binDirs := (&sdk.GolangFetcher{}).GetBinDirs()

	err := copyToTargetAtomically(src, target, binDirs, sdk.Golang, func(string) error { return nil })
	if err == nil {
		t.Fatal("copyToTargetAtomically should reject an incomplete SDK")
	}
	if !strings.Contains(err.Error(), "SDK incomplete") {
		t.Errorf("error = %v; want an 'SDK incomplete' message", err)
	}
	for _, p := range []string{target, target + ".new", target + ".old"} {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Errorf("%s should not exist after a rejected import (stat err = %v)", p, statErr)
		}
	}
}

// TestCopyToTargetAtomically_PreservesOldOnVerifyFailure verifies that when
// the verify callback fails (after alignment + critical check), the existing
// target directory is left intact and the temp dir is cleaned up.
func TestCopyToTargetAtomically_PreservesOldOnVerifyFailure(t *testing.T) {
	src := t.TempDir()
	writeFlatGoLayout(t, src)

	base := t.TempDir()
	target := filepath.Join(base, "go", "1.21.5")
	marker := []byte("existing version marker")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "marker.txt"), marker, 0644); err != nil {
		t.Fatal(err)
	}

	binDirs := (&sdk.GolangFetcher{}).GetBinDirs()
	err := copyToTargetAtomically(src, target, binDirs, sdk.Golang, func(string) error {
		return fmt.Errorf("simulated verify failure")
	})
	if err == nil {
		t.Fatal("copyToTargetAtomically should propagate the verify failure")
	}

	got, readErr := os.ReadFile(filepath.Join(target, "marker.txt"))
	if readErr != nil || string(got) != string(marker) {
		t.Fatalf("existing target was destroyed on verify failure: content=%q err=%v", got, readErr)
	}
	for _, leftover := range []string{target + ".new", target + ".old"} {
		if _, statErr := os.Stat(leftover); !os.IsNotExist(statErr) {
			t.Errorf("leftover temp dir %s still exists (stat err = %v)", leftover, statErr)
		}
	}
}
