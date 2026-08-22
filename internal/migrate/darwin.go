//go:build darwin

package migrate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"svc/internal/logger"
	"svc/internal/wailsrt"
)

// legacyAppBundleName is the pre-rename .app bundle folder name. Self-updated
// pre-2.0.0 macOS installs still live inside this bundle; new builds produce
// "svc.app".
const legacyAppBundleName = "SDK Version Control.app"

// isLegacyDarwinInstall reports whether there is still something left to rename
// to "svc". It must NOT only look at the bundle folder name: the 2.0.0
// migration renamed the bundle to svc.app but not the inner executable
// (self-update replaces the executable's CONTENTS, not its FILE NAME), so an
// install can have a svc.app bundle whose Contents/MacOS binary still carries
// the old name. Detect legacy when EITHER the inner executable is not "svc"
// OR the bundle folder is still the old name — covering fresh self-updated
// installs, partially-migrated installs, and the already-renamed-bundle case.
func isLegacyDarwinInstall() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	// exe = <bundle>/Contents/MacOS/<binary>; the bundle is 3 levels up. It
	// must actually be a .app bundle — a real install. The Go test binary is
	// not inside a .app, so this returns false under `go test` and the
	// migration never triggers in tests.
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(strings.ToLower(bundle), ".app") {
		return false
	}
	// A fresh install's inner executable is named "svc" (wails build -o svc).
	// Any other name means it is a self-updated pre-rename executable. This
	// catches installs whose bundle is already svc.app but whose inner binary
	// still carries the old name.
	if filepath.Base(exe) != "svc" {
		return true
	}
	// Fallback: inner executable is already "svc" but the bundle folder is
	// still the old name (a prior migration renamed the binary but not the
	// bundle).
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
LOG="$HOME/svc_migration.log"
log() { echo "[$(date '+%%H:%%M:%%S')] $*" >> "$LOG"; }
log "=== migration start (pid=$PID, exe=$OLD_EXE) ==="

# Wait for the application to exit.
timeout=60
while kill -0 "$PID" 2>/dev/null && [ "$timeout" -gt 0 ]; do
    sleep 1
    timeout=$((timeout-1))
done
[ "$timeout" -le 0 ] && { log "ABORT: app did not exit in 60s"; exit 1; }

# Bundle is three levels up from the executable (<bundle>/Contents/MacOS/bin).
MACOS_DIR=$(dirname "$OLD_EXE")
CONTENTS=$(dirname "$MACOS_DIR")
BUNDLE=$(dirname "$CONTENTS")
APPDIR=$(dirname "$BUNDLE")
NEW_BUNDLE="$APPDIR/svc.app"
log "bundle=$BUNDLE new_bundle=$NEW_BUNDLE"

# Rename the bundle when the parent directory is writable (needs the same
# privileges the app already has; a non-elevated app in /Applications cannot
# rename the bundle, so this silently falls back to keeping it in place).
if [ "$BUNDLE" != "$NEW_BUNDLE" ]; then
    if mv "$BUNDLE" "$NEW_BUNDLE" 2>>"$LOG"; then
        log "bundle renamed -> $NEW_BUNDLE"
    else
        log "bundle rename FAILED (keeping $BUNDLE)"
        NEW_BUNDLE="$BUNDLE"
    fi
fi

# Self-update replaces the inner executable's CONTENTS but not its FILE NAME,
# so Contents/MacOS/<file> still shows the old name. Rename it to "svc" and
# point CFBundleExecutable at the new name so macOS launches it. (The bundle
# is unsigned — the build strips the signature — so renaming is safe.)
INNER_OLD=$(basename "$OLD_EXE")
MACOS_NEW="$NEW_BUNDLE/Contents/MacOS"
log "inner_old=$INNER_OLD macos_new=$MACOS_NEW"
if [ "$INNER_OLD" != "svc" ] && [ -f "$MACOS_NEW/$INNER_OLD" ]; then
    if mv "$MACOS_NEW/$INNER_OLD" "$MACOS_NEW/svc" 2>>"$LOG"; then
        log "inner executable renamed -> svc"
    else
        log "inner executable rename FAILED"
    fi
else
    log "inner rename skipped (already svc or source missing)"
fi

# Update the displayed bundle name and the executable name in Info.plist.
# CFBundleName/CFBundleDisplayName drive the Finder/Dock label;
# CFBundleExecutable must match the renamed inner binary. Best-effort.
PLIST="$NEW_BUNDLE/Contents/Info.plist"
if [ -f "$PLIST" ]; then
    /usr/bin/plutil -replace CFBundleName -string "svc" "$PLIST" 2>>"$LOG" && log "CFBundleName=svc"
    /usr/bin/plutil -replace CFBundleDisplayName -string "svc" "$PLIST" 2>>"$LOG"
    /usr/bin/plutil -replace CFBundleExecutable -string "svc" "$PLIST" 2>>"$LOG" && log "CFBundleExecutable=svc"
fi

# Relaunch only if the migration actually produced the svc executable. If
# the rename failed (e.g. insufficient permission), relaunching would just
# re-trigger this migration on the next launch and flash on every startup;
# leaving the app closed for the user to relaunch manually is safer.
if [ -f "$MACOS_NEW/svc" ]; then
    open "$NEW_BUNDLE" 2>/dev/null || nohup "$MACOS_NEW/svc" >/dev/null 2>&1 &
fi
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
func MaybeShowLegacyMigrationPrompt(ctx context.Context, rt wailsrt.Runtime) {
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
	rt.Quit()
}

// RepairShortcutIcons is a no-op on macOS: there are no .lnk shortcuts to
// repair (the Dock/Launchpad resolve the bundle automatically).
func RepairShortcutIcons() {}
