//go:build windows

package helpers

import (
	"context"
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// CreateCmd builds an exec.Cmd for spawning external commands. On Windows
// the child runs with CREATE_NO_WINDOW so background operations (package
// manager installs, update checks) never flash a console window.
func CreateCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
	}
	return cmd
}

// CreateCmdContext builds an exec.Cmd bound to ctx for cancellation/timeout.
func CreateCmdContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
	}
	return cmd
}
