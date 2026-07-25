//go:build !windows

package shim

import "syscall"

// execBinary replaces the current process with the target binary (Unix).
func execBinary(realBinary string) {
	args := append([]string{realBinary}, osArgs()...)
	syscall.Exec(realBinary, args, nil)
}
