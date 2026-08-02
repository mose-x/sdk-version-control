//go:build windows

package shimmanager

import _ "embed"

// embeddedShimBinary is the prebuilt console-subsystem svc-shim.exe, baked into
// the app at build time. ensureShimBinary writes these bytes to
// ~/.svc/shims/svc-shim.exe so that command shims (node.exe, ...) hardlink to
// a console binary instead of the GUI-subsystem app binary.
//
// Why a separate console binary: the Wails app binary uses -H windowsgui (no
// console). Hardlinking node.exe to it makes `node -v` either print nothing
// (no console handle) or, with the AttachConsole workaround, print the version
// but leave cmd.exe's prompt stuck (terminal looks hung until Enter). A console
// binary gets a real stdio handle from cmd.exe and the prompt redraws on exit,
// exactly like the real node.exe.
//
// The embedded file is produced by the CI workflow's "Build console shim
// binary" step (go build ./cmd/svc-shim) BEFORE wails build, for the same
// windows/<arch> target. A committed empty placeholder lets `wails build` run
// on a dev machine without the prebuild step; in that case len==0 and
// ensureShimBinary falls back to copying the app binary (AttachConsole keeps
// output working, with the known prompt-hang trade-off).
//
//go:embed svc-shim.windows.exe
var embeddedShimBinary []byte
