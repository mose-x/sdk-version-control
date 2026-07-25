//go:build windows

package shim

import (
	"os"
	"os/exec"
)

// execBinary runs the target binary as a child process and exits with its code (Windows).
// Windows does not support syscall.Exec (process replacement), so we spawn a child.
func execBinary(realBinary string) {
	cmd := exec.Command(realBinary, osArgs()...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
	os.Exit(0)
}
