//go:build darwin

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"svc/internal/logger"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// legacyAppBundleName is the pre-rename .app bundle folder name. Self-updated
// pre-2.0.0 macOS installs still live inside this bundle; new builds produce
// "svc.app".
const legacyAppBundleName = "SDK Version Control.app"

// isLegacyDarwinInstall reports whether the running binary sits inside the
// old-named .app bundle.
func isLegacyDarwinInstall() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	// exe = <bundle>/Contents/MacOS/<binary>; the bundle is 3 levels up.
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	return strings.EqualFold(filepath.Base(bundle), legacyAppBundleName)
}

// buildLegacyRenameSh renders the migration shell script: wait for the app to
// exit, rename the .app bundle to svc.app when the parent directory allows it
// (falls back to leaving the bundle in place otherwise), update the displayed
// bundle name in Info.plist, and relaunch. Extracted for testing.
func buildLegacyRenameSh(pid int, currentExe string) string {
	return fmt.Sprintf(`#!/bin/bash
PID="%d"
OLD_EXE="%s"

# Wait for the application to exit.
timeout=60
while kill -0 "$PID" 2>/dev/null && [ "$timeout" -gt 0 ]; do
    sleep 1
    timeout=$((timeout-1))
done
[ "$timeout" -le 0 ] && exit 1

# Bundle is three levels up from the executable (<bundle>/Contents/MacOS/bin).
MACOS_DIR=$(dirname "$OLD_EXE")
CONTENTS=$(dirname "$MACOS_DIR")
BUNDLE=$(dirname "$CONTENTS")
APPDIR=$(dirname "$BUNDLE")
NEW_BUNDLE="$APPDIR/svc.app"

# Rename the bundle when the parent directory is writable (needs the same
# privileges the app already has; a non-elevated app in /Applications cannot
# rename the bundle, so this silently falls back to keeping it in place).
if [ "$BUNDLE" != "$NEW_BUNDLE" ]; then
    mv "$BUNDLE" "$NEW_BUNDLE" 2>/dev/null || NEW_BUNDLE="$BUNDLE"
fi

# Update the displayed bundle name in Info.plist (CFBundleName drives the
# Finder/Dock label). Best-effort; ignore failures.
PLIST="$NEW_BUNDLE/Contents/Info.plist"
if [ -f "$PLIST" ]; then
    /usr/bin/plutil -replace CFBundleName -string "svc" "$PLIST" 2>/dev/null
    /usr/bin/plutil -replace CFBundleDisplayName -string "svc" "$PLIST" 2>/dev/null
fi

# Relaunch.
open "$NEW_BUNDLE" 2>/dev/null || nohup "$NEW_BUNDLE/Contents/MacOS/$(basename "$OLD_EXE")" >/dev/null 2>&1 &
exit 0
`, pid, currentExe)
}

// launchLegacyRenameMigration writes the migration script to a temp file,
// makes it executable, and starts it detached — NO elevation prompt. The
// child survives the app's exit and waits for it before renaming.
func launchLegacyRenameMigration(currentExe string) error {
	script := buildLegacyRenameSh(os.Getpid(), currentExe)
	f, err := os.CreateTemp(os.TempDir(), "svc_migrate_*.sh")
	if err != nil {
		return fmt.Errorf("failed to create migration script: %w", err)
	}
	scriptPath := f.Name()
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(scriptPath)
		return fmt.Errorf("failed to write migration script: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(scriptPath)
		return fmt.Errorf("failed to write migration script: %w", err)
	}
	if err := os.Chmod(scriptPath, 0700); err != nil {
		os.Remove(scriptPath)
		return fmt.Errorf("failed to make migration script executable: %w", err)
	}

	cmd := exec.Command("/bin/bash", scriptPath)
	if err := cmd.Start(); err != nil {
		os.Remove(scriptPath)
		return fmt.Errorf("failed to launch migration: %w", err)
	}
	// Deliberately do NOT Wait: the script waits for THIS process to exit
	// before renaming. The child keeps running after the app quits.
	return nil
}

// maybeShowLegacyMigrationPrompt silently completes the rename migration on
// startup when the app detects it is still inside the old-named .app bundle.
// No dialog and no elevation prompt. Idempotent — once the bundle is
// svc.app, isLegacyDarwinInstall is false and this is a no-op.
func maybeShowLegacyMigrationPrompt(ctx context.Context) {
	if !isLegacyDarwinInstall() {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	logger.Info("Legacy macOS install detected (%s); starting silent rename migration", exe)
	if err := launchLegacyRenameMigration(exe); err != nil {
		logger.Warn("Failed to start rename migration: %v", err)
		return
	}
	// The script waits for this process to exit before touching anything.
	wailsRuntime.Quit(ctx)
}
