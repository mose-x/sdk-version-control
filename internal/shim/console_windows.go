//go:build windows

package shim

import (
	"os"
	"syscall"
)

// Windows console handle ids (the Win32 STD_INPUT/OUTPUT/ERROR_HANDLE
// constants are negative DWORDs; express them as unsigned hex so the
// type matches the syscall ABI without a cast).
const (
	attachParentProcess = 0xFFFFFFFF // (DWORD)-1 → attach to the parent's console
	stdInputHandle      = 0xFFFFFFF6 // (DWORD)-10  STD_INPUT_HANDLE
	stdOutputHandle     = 0xFFFFFFF5 // (DWORD)-11  STD_OUTPUT_HANDLE
	stdErrorHandle      = 0xFFFFFFF4 // (DWORD)-12  STD_ERROR_HANDLE
)

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
	procGetStdHandle  = kernel32.NewProc("GetStdHandle")
)

// attachParentConsole attaches this process to its parent's console and
// rebinds os.Stdin/Stdout/Stderr to the parent console's handles.
//
// Why this exists: Wails builds the app binary with -H windowsgui so the
// GUI does not pop a black console window. That same binary is hardlinked
// as node.exe / go.exe / ... and used as the shim. A windowsgui binary has
// NO console attached, so os.Stdin/Stdout/Stderr are invalid handles. When
// the shim spawns the real SDK binary as a child, the child inherits those
// invalid handles and its stdout/stderr are silently dropped — `node -v`
// prints nothing, shim error diagnostics are invisible, and the window
// just flashes and exits.
//
// Attaching to the parent console (the cmd.exe / Windows Terminal / IDE
// terminal that launched the shim) restores real stdio handles so both
// shim diagnostics and the target binary's output reach the user.
func attachParentConsole() {
	// If stdout already resolves to a real handle, a console is already
	// attached (e.g. the binary was rebuilt as a console app, or the shim
	// was re-invoked). Skip — AttachConsole would fail with
	// ERROR_ACCESS_DENIED and needlessly reset working handles.
	if h := getStdHandle(stdOutputHandle); h != 0 {
		return
	}

	r1, _, _ := procAttachConsole.Call(uintptr(attachParentProcess))
	if r1 == 0 {
		// AttachConsole failed: parent has no console (launched from
		// Explorer) or another error. Nothing useful we can do — the
		// shim will run without console I/O (target binary may still
		// allocate its own console on launch).
		return
	}

	// Rebind the Go-level std files to the freshly attached console's
	// handles. exec.Command reads cmd.Stdin/Stdout/Stderr (which default
	// to os.Stdin/Stdout/Stderr) and passes them to CreateProcess as the
	// child's inherited stdin/stdout/stderr, so the target binary inherits
	// valid handles instead of the GUI binary's null handles.
	if h := getStdHandle(stdInputHandle); h != 0 {
		os.Stdin = os.NewFile(uintptr(h), "stdin")
	}
	if h := getStdHandle(stdOutputHandle); h != 0 {
		os.Stdout = os.NewFile(uintptr(h), "stdout")
	}
	if h := getStdHandle(stdErrorHandle); h != 0 {
		os.Stderr = os.NewFile(uintptr(h), "stderr")
	}
}

// getStdHandle returns the handle for a standard device, or 0 if none.
// GetStdHandle returns INVALID_HANDLE_VALUE (-1) — not only 0 — when the
// device has no handle; isValidStdHandle rejects both (and the zero-
// extended 32-bit -1) so os.NewFile never receives a bogus handle.
func getStdHandle(id uint32) syscall.Handle {
	r1, _, _ := procGetStdHandle.Call(uintptr(id))
	if !isValidStdHandle(r1) {
		return 0
	}
	return syscall.Handle(r1)
}
