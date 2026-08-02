//go:build !windows

package shimmanager

// embeddedShimBinary is unused on non-Windows: the app binary is already a
// normal executable there (no GUI/console subsystem split), so ensureShimBinary
// copies it directly. Declared (nil) so the same ensureShimBinary code path
// compiles on every platform; len(embeddedShimBinary)==0 routes Unix through
// the copy-app-binary branch.
var embeddedShimBinary []byte
