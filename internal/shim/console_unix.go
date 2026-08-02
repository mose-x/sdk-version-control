//go:build !windows

package shim

// attachParentConsole is a no-op on non-Windows platforms: the shim binary
// is a normal console executable there, so os.Stdin/Stdout/Stderr are
// already valid handles inherited from the parent shell.
func attachParentConsole() {}
