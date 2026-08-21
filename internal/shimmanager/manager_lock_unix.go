//go:build !windows

package shimmanager

import (
	"os"
	"path/filepath"
	"syscall"
)

// shimsLockFile is the sidecar lock file that serializes read-modify-write
// cycles of shims.json across processes. Locking shims.json itself is not
// viable: saveShimConfig replaces it atomically via rename, and a lock held
// on the old inode would not exclude a writer that renames over it.
const shimsLockFile = "shims.json.lock"

// lockShimsConfig acquires an exclusive cross-process lock (flock LOCK_EX) on
// <dir>/shims.json.lock, blocking until it is available. The returned file
// must be passed to unlockShimsConfig.
func lockShimsConfig(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, shimsLockFile), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// unlockShimsConfig releases the lock and closes the file.
func unlockShimsConfig(f *os.File) {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
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
