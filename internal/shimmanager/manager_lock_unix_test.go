//go:build !windows

package shimmanager

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestLockShimsConfig_flockMutualExclusion pins item 3 on Unix: while one
// fd holds the exclusive flock, a second fd on the same lock file must NOT
// acquire it (LOCK_NB returns EWOULDBLOCK); after release it must succeed.
// Two fds in one process model two processes for flock purposes.
func TestLockShimsConfig_flockMutualExclusion(t *testing.T) {
	dir := t.TempDir()

	f1, err := lockShimsConfig(dir)
	if err != nil {
		t.Fatalf("lockShimsConfig failed: %v", err)
	}

	// Second fd: non-blocking acquire must fail while f1 holds the lock.
	f2, err := os.OpenFile(filepath.Join(dir, shimsLockFile), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()
	if err := syscall.Flock(int(f2.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		syscall.Flock(int(f2.Fd()), syscall.LOCK_UN)
		t.Fatal("second fd acquired the flock while the first fd held it")
	}

	// Release f1; now f2 must be able to acquire.
	unlockShimsConfig(f1)
	if err := syscall.Flock(int(f2.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("second fd could not acquire the flock after release: %v", err)
	}
	syscall.Flock(int(f2.Fd()), syscall.LOCK_UN)
}

// TestLockShimsConfig_createsLockFile verifies the sidecar lock file is
// created next to shims.json (the lock target is NOT shims.json itself,
// which is replaced atomically by rename on save).
func TestLockShimsConfig_createsLockFile(t *testing.T) {
	dir := t.TempDir()
	f, err := lockShimsConfig(dir)
	if err != nil {
		t.Fatalf("lockShimsConfig failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, shimsLockFile)); err != nil {
		t.Errorf("lock file not created: %v", err)
	}
	unlockShimsConfig(f)
}
