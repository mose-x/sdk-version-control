// Package main is the dedicated shim binary (svc-shim).
//
// This is a SEPARATE binary from the Wails GUI app, built as a normal
// console-subsystem executable (the default `go build` subsystem on Windows).
// The Wails app binary is built with -H windowsgui so the GUI does not pop a
// console window; that same GUI binary cannot serve as the shim because a
// windowsgui binary has no console handle, so `node -v` would print nothing
// and cmd.exe would not redraw its prompt (terminal looks hung).
//
// A console-subsystem shim behaves like any real CLI tool (node.exe, go.exe):
// cmd.exe gives it a real console, stdio works natively, and the prompt is
// redrawn when it exits. The app embeds this binary (see
// internal/shimmanager/shimbin_windows.go) and writes it to ~/.svc/shims as
// svc-shim.exe on Windows; command shims (node.exe, ...) hardlink to it.
//
// `go build ./cmd/svc-shim` produces the console binary. It imports only
// internal/shim (stdlib-only), so it cross-compiles without CGO and stays
// small.
package main

import "sdk_version_control/internal/shim"

func main() {
	shim.Run()
}
