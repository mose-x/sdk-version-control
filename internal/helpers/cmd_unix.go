//go:build !windows

package helpers

import (
	"context"
	"os/exec"
)

// CreateCmd builds an exec.Cmd for spawning external commands.
func CreateCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// CreateCmdContext builds an exec.Cmd bound to ctx for cancellation/timeout.
func CreateCmdContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
