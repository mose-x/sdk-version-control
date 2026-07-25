//go:build !windows

package shim

import "syscall"

// execBinary replaces the current process with the target binary (Unix).
func execBinary(realBinary string, args []string) {
	fullArgs := append([]string{realBinary}, args...)
	syscall.Exec(realBinary, fullArgs, nil)
}
