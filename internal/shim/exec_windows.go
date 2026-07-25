//go:build windows

package shim

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// execBinary runs the target binary as a child process and exits with its
// code (Windows). Windows has no syscall.Exec, so we spawn a child. Batch
// scripts (.cmd/.bat) must be run via cmd.exe since CreateProcess cannot
// execute them directly.
func execBinary(realBinary string, args []string) {
	var cmd *exec.Cmd
	ext := strings.ToLower(filepath.Ext(realBinary))
	if ext == ".cmd" || ext == ".bat" {
		fullArgs := append([]string{"/c", realBinary}, args...)
		cmd = exec.Command("cmd.exe", fullArgs...)
	} else {
		cmd = exec.Command(realBinary, args...)
	}
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
