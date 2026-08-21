//go:build windows

package shimmanager

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// shimsLockFile is the sidecar lock file that serializes read-modify-write
// cycles of shims.json across processes. Locking shims.json itself is not
// viable: saveShimConfig replaces it atomically via rename, and a lock held
// on the old handle would not exclude a writer that renames over it.
const shimsLockFile = "shims.json.lock"

// lockShimsConfig acquires an exclusive cross-process lock (LockFileEx) on
// <dir>/shims.json.lock, blocking until it is available. The returned file
// must be passed to unlockShimsConfig. Windows has no flock(2); LockFileEx
// on a dedicated sidecar file is the equivalent. The lock is advisory but
// every writer in this codebase goes through withShimsConfigLock.
func lockShimsConfig(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, shimsLockFile), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	// Lock the whole file (full 64-bit range). A zero Overlapped makes the
	// call synchronous: it blocks until the exclusive lock is granted.
	err = windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0,
		0xFFFFFFFF, 0xFFFFFFFF,
		&windows.Overlapped{},
	)
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// unlockShimsConfig releases the lock and closes the file. Closing the
// handle alone would also release the lock, but unlock explicitly so the
// intent is clear and the pair mirrors the Unix flock/funlock.
func unlockShimsConfig(f *os.File) {
	windows.UnlockFileEx(
		windows.Handle(f.Fd()),
		0,
		0xFFFFFFFF, 0xFFFFFFFF,
		&windows.Overlapped{},
	)
	f.Close()
}

// withShimsConfigLock runs fn while holding the exclusive shims.json lock,
// serializing concurrent read-modify-write cycles (GUI + CLI, or two GUI
// instances) so their updates don't clobber each other (lost-update race).
func withShimsConfigLock(dir string, fn func() error) error {
	f, err := lockShimsConfig(dir)
	if err != nil {
		return err
	}
	defer unlockShimsConfig(f)
	return fn()
}
