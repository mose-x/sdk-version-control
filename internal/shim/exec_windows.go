//go:build windows

package shim

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// execBinary runs the target binary as a child process and exits with its
// code (Windows). Windows has no syscall.Exec, so we spawn a child. Batch
// scripts (.cmd/.bat) must be run via cmd.exe since CreateProcess cannot
// execute them directly.
func execBinary(realBinary string, args []string) {
	var cmd *exec.Cmd
	ext := strings.ToLower(filepath.Ext(realBinary))
	if ext == ".cmd" || ext == ".bat" {
		cmd = buildCmdExec(realBinary, args)
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
		// Not an ExitError: the child could not be started at all (bad
		// path, missing DLL, permission denied, ...). Without this line
		// the shim would exit 1 silently, which on a GUI-subsystem binary
		// looks exactly like the "flash and disappear" symptom.
		fmt.Fprintf(os.Stderr, "shim: failed to run %q: %v\n", realBinary, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// buildCmdExec builds the exec.Cmd that runs a .cmd/.bat target through
// cmd.exe /c. The command line is assembled manually (SysProcAttr.CmdLine)
// with cmd.exe-aware escaping: Go's default argument quoting is only
// understood by CommandLineToArgvW consumers, NOT by cmd.exe. Passing args
// via exec.Command("cmd.exe", "/c", script, args...) lets cmd.exe
// re-interpret & | < > ^ inside arguments — truncating them or executing
// injected commands. escapeCmdArg/buildCmdCommandLine implement the safe
// encoding (see cmdescape.go), verified end-to-end against real cmd.exe.
func buildCmdExec(script string, args []string) *exec.Cmd {
	cmdExe, err := exec.LookPath("cmd.exe")
	if err != nil {
		// cmd.exe is always present on Windows; fall back to the bare name
		// and let CreateProcess search the standard locations.
		cmdExe = "cmd.exe"
	}
	cmd := exec.Command(cmdExe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: buildCmdCommandLine(cmdExe, script, args),
	}
	return cmd
}
