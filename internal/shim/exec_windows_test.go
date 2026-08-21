//go:build windows

package shim

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// printerProgram prints each received argument as ARG<i>=[<value>] so tests
// can assert exactly what survived the cmd.exe -> batch %* -> CRT chain.
const printerProgram = `package main

import (
	"fmt"
	"os"
)

func main() {
	for i, a := range os.Args[1:] {
		fmt.Printf("ARG%d=[%s]\n", i, a)
	}
}
`

// buildPrinter compiles the arg printer into dir and returns its path.
func buildPrinter(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "printer.go")
	if err := os.WriteFile(src, []byte(printerProgram), 0644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "printer.exe")
	out, err := exec.Command("go", "build", "-o", exe, src).CombinedOutput()
	if err != nil {
		t.Fatalf("go build printer failed: %v\n%s", err, out)
	}
	return exe
}

// runViaCmd runs target.cmd through the same code path execBinary uses
// (buildCmdCommandLine + SysProcAttr.CmdLine) and returns combined output.
func runViaCmd(t *testing.T, script string, args []string) string {
	t.Helper()
	cmdExe, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Skipf("cmd.exe not found: %v", err)
	}
	cmd := exec.Command(cmdExe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: buildCmdCommandLine(cmdExe, script, args),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cmd.exe /c failed: %v\noutput:\n%s", err, out)
	}
	return string(out)
}

// TestCmdEscapeEndToEnd verifies the escaping scheme against real cmd.exe:
// arguments carrying cmd metacharacters must arrive at the final program
// verbatim through the cmd.exe /c -> batch %* -> CreateProcess chain. This
// is the regression test for the argument-truncation + command-injection
// bug in the previous exec.Command("cmd.exe", "/c", script, args...) path.
func TestCmdEscapeEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping end-to-end cmd.exe test in -short mode")
	}
	dir := t.TempDir()
	printer := buildPrinter(t, dir)

	// Forwarding wrapper mimicking an SDK batch script (%* pass-through).
	target := filepath.Join(dir, "target.cmd")
	wrapper := "@echo off\r\n\"" + printer + "\" %*\r\n"
	if err := os.WriteFile(target, []byte(wrapper), 0644); err != nil {
		t.Fatal(err)
	}

	args := []string{
		"plain",
		"two words",
		"a&b",
		"a|b",
		"a^b",
		"100%",
		"%PATH%",
		"(parens)",
		"x<y>z",
		"!bang!",
		`C:\out\`,
	}
	out := runViaCmd(t, target, args)
	for i, want := range args {
		line := "ARG" + strconv.Itoa(i) + "=[" + want + "]"
		if !strings.Contains(out, line) {
			t.Errorf("arg %d (%q) did not arrive verbatim; output:\n%s", i, want, out)
		}
	}
}

// TestCmdEscapeEndToEnd_noInjection feeds an adversarial argument whose
// quotes and & characters would execute `echo INJECTED` under the old
// unescaped pass-through. The payload must arrive as one inert literal
// argument (cmd.exe strips the quotes during its re-parse; the content and
// argument count are what matter — no INJECTED line may be executed).
func TestCmdEscapeEndToEnd_noInjection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping end-to-end cmd.exe test in -short mode")
	}
	dir := t.TempDir()
	printer := buildPrinter(t, dir)
	target := filepath.Join(dir, "target.cmd")
	wrapper := "@echo off\r\n\"" + printer + "\" %*\r\n"
	if err := os.WriteFile(target, []byte(wrapper), 0644); err != nil {
		t.Fatal(err)
	}

	payload := `x" & echo INJECTED & "`
	out := runViaCmd(t, target, []string{payload, "safe after"})
	if strings.Contains(out, "INJECTED\n") || strings.Contains(out, "INJECTED \n") ||
		strings.Contains(out, "\nINJECTED") {
		t.Fatalf("command injection succeeded; output:\n%s", out)
	}
	// The payload arrives as one argument (cmd strips the embedded quotes —
	// a documented cmd.exe limitation), and the following arg is intact.
	if !strings.Contains(out, "ARG0=[x & echo INJECTED & ]") {
		t.Errorf("payload did not arrive as a single literal arg; output:\n%s", out)
	}
	if !strings.Contains(out, "ARG1=[safe after]") {
		t.Errorf("arg after the payload was corrupted; output:\n%s", out)
	}
}
