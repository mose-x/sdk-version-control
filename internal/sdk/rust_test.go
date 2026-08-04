package sdk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRustMergeComponents(t *testing.T) {
	dir := t.TempDir()

	// Create per-component layout mimicking the extracted Rust tarball.
	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{dir}, parts...)...)
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		return p
	}
	mkFile := func(dir, name, content string) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	cargoBin := mk("cargo", "bin")
	mkFile(cargoBin, "cargo.exe", "fake")

	rustcBin := mk("rustc", "bin")
	mkFile(rustcBin, "rustc.exe", "fake")

	rustfmtBin := mk("rustfmt-preview", "bin")
	mkFile(rustfmtBin, "rustfmt.exe", "fake")

	stdLib := mk("rust-std-x86_64-pc-windows-msvc", "lib", "rustlib", "x86_64-pc-windows-msvc", "lib")
	mkFile(stdLib, "libstd-test.rlib", "std content")
	mkFile(stdLib, "libcore-test.rlib", "core content")

	f := &RustFetcher{}
	if err := f.MergeComponents(dir); err != nil {
		t.Fatalf("MergeComponents failed: %v", err)
	}

	// Verify rustlib was copied to cargo/lib/rustlib and rustc/lib/rustlib.
	for _, comp := range []string{"cargo", "rustc"} {
		dst := filepath.Join(dir, comp, "lib", "rustlib", "x86_64-pc-windows-msvc", "lib", "libstd-test.rlib")
		if _, err := os.Stat(dst); err != nil {
			t.Errorf("expected %s to exist after merge: %v", dst, err)
		}
		coreDst := filepath.Join(dir, comp, "lib", "rustlib", "x86_64-pc-windows-msvc", "lib", "libcore-test.rlib")
		if _, err := os.Stat(coreDst); err != nil {
			t.Errorf("expected %s to exist after merge: %v", coreDst, err)
		}
	}

	// Verify the original rust-std source is still intact.
	srcFile := filepath.Join(dir, "rust-std-x86_64-pc-windows-msvc", "lib", "rustlib", "x86_64-pc-windows-msvc", "lib", "libstd-test.rlib")
	if _, err := os.Stat(srcFile); err != nil {
		t.Errorf("original rust-std source should still exist: %v", err)
	}
}

func TestRustMergeComponentsNoStdDir(t *testing.T) {
	dir := t.TempDir()

	// Create only cargo/ and rustc/, no rust-std-* directory.
	mk := func(parts ...string) {
		p := filepath.Join(append([]string{dir}, parts...)...)
		os.MkdirAll(p, 0755)
	}
	mk("cargo", "bin")
	mk("rustc", "bin")

	f := &RustFetcher{}
	// Should not error when there's no rust-std-* directory.
	if err := f.MergeComponents(dir); err != nil {
		t.Fatalf("MergeComponents should not error without rust-std dir: %v", err)
	}

	// Should not have created lib/rustlib.
	if _, err := os.Stat(filepath.Join(dir, "cargo", "lib", "rustlib")); !os.IsNotExist(err) {
		t.Errorf("cargo/lib/rustlib should not exist when there's no rust-std dir")
	}
}
