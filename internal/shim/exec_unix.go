//go:build !windows

package shim

import (
	"os"
	"syscall"
)

// execBinary replaces the current process with the target binary (Unix).
// The envv MUST be passed explicitly: syscall.Exec passes nil to execve as
// an empty environment, dropping PATH/HOME and any os.Setenv'd vars
// (JAVA_HOME, GOROOT, ...) set by Run() above. os.Environ() preserves the
// current process environment so the target binary inherits everything,
// matching the Windows exec_windows.go behaviour (cmd.Env = os.Environ()).
func execBinary(realBinary string, args []string) {
	fullArgs := append([]string{realBinary}, args...)
	syscall.Exec(realBinary, fullArgs, os.Environ())
}
