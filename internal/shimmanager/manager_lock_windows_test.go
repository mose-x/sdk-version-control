//go:build windows

package shimmanager

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// tryLockNonBlocking attempts a non-blocking exclusive LockFileEx on f.
func tryLockNonBlocking(f *os.File) error {
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		0xFFFFFFFF, 0xFFFFFFFF,
		&windows.Overlapped{},
	)
}

// TestLockShimsConfig_windowsMutualExclusion pins item 3 on Windows: while
// one handle holds the exclusive LockFileEx, a second handle on the same
// lock file must NOT acquire it (FAIL_IMMEDIATELY returns
// ERROR_LOCK_VIOLATION); after release it must succeed. This is the
// Windows counterpart of the Unix flock test.
func TestLockShimsConfig_windowsMutualExclusion(t *testing.T) {
	dir := t.TempDir()

	f1, err := lockShimsConfig(dir)
	if err != nil {
		t.Fatalf("lockShimsConfig failed: %v", err)
	}

	f2, err := os.OpenFile(filepath.Join(dir, shimsLockFile), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()

	if err := tryLockNonBlocking(f2); err == nil {
		windows.UnlockFileEx(windows.Handle(f2.Fd()), 0, 0xFFFFFFFF, 0xFFFFFFFF, &windows.Overlapped{})
		t.Fatal("second handle acquired the lock while the first held it")
	}

	// Release f1; now f2 must be able to acquire.
	unlockShimsConfig(f1)
	if err := tryLockNonBlocking(f2); err != nil {
		t.Fatalf("second handle could not acquire the lock after release: %v", err)
	}
	if err := windows.UnlockFileEx(windows.Handle(f2.Fd()), 0, 0xFFFFFFFF, 0xFFFFFFFF, &windows.Overlapped{}); err != nil {
		t.Fatalf("UnlockFileEx failed: %v", err)
	}
}

// TestLockShimsConfig_acquireReleaseNoError covers the minimal contract on
// Windows: acquiring and releasing the lock must not error, and the sidecar
// lock file must exist afterwards.
func TestLockShimsConfig_acquireReleaseNoError(t *testing.T) {
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
