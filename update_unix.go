//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func getUpdateFilePath() string {
	return filepath.Join(os.TempDir(), "svc_update_new")
}

// backupPath returns <exe>.bak, used by ApplyUpdate to keep the previous
// binary so RollbackUpdate can restore it on failure.
func backupPath(currentExe string) string {
	return currentExe + ".bak"
}

// shellQuote wraps a string in single quotes for safe shell interpolation.
// Single quotes inside the string are escaped via the standard '\” idiom.
// Unlike double quotes, single-quoted strings have no interpolation, so
// paths containing $, `, ", spaces, etc. are safe.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ApplyUpdate launches a background /bin/sh script that: waits for the
// current process to exit (by PID, not pgrep -f which matches too wide),
// atomically renames the running binary to .bak, renames the downloaded
// payload into place, chmod +x, relaunches, and self-deletes.
//
// Rename (mv) is used instead of cp so the replacement is atomic: a failed
// second step leaves the .bak intact and the current binary untouched,
// rather than overwriting it halfway and leaving a corrupt executable.
// A 60s timeout guards against the wait loop hanging forever if the app
// fails to exit.
func (a *App) ApplyUpdate() error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get current program path: %w", err)
	}

	newExe := getUpdateFilePath()
	if _, err := os.Stat(newExe); err != nil {
		return fmt.Errorf("update file does not exist: %w", err)
	}

	bak := backupPath(currentExe)
	scriptPath := filepath.Join(os.TempDir(), "svc_updater.sh")
	pid := os.Getpid()
	// All paths are shell-quoted to avoid injection / syntax errors when
	// the install path contains spaces, $, ", `, etc.
	exeQ := shellQuote(currentExe)
	bakQ := shellQuote(bak)
	newQ := shellQuote(newExe)
	scriptContent := fmt.Sprintf(`#!/bin/sh
echo "Waiting for application to close..."
timeout=60
while kill -0 %d 2>/dev/null; do
    sleep 1
    timeout=$((timeout - 1))
    if [ "$timeout" -le 0 ]; then
        echo "Update timed out waiting for app to exit, aborting"
        exit 1
    fi
done
echo "Backing up current binary..."
if ! mv -f %s %s 2>/dev/null; then
    # Cross-device: fall back to cp+rm. mv is atomic on same FS only.
    cp -f %s %s && rm -f %s
    if [ $? -ne 0 ]; then
        echo "Backup failed, aborting update"
        exit 1
    fi
fi
echo "Replacing application..."
if ! mv -f %s %s 2>/dev/null; then
    cp -f %s %s && rm -f %s
    if [ $? -ne 0 ]; then
        echo "Update failed! Restoring backup..."
        mv -f %s %s 2>/dev/null || cp -f %s %s
        exit 1
    fi
fi
chmod +x %s
echo "Starting new version..."
nohup %s > /dev/null 2>&1 &
rm -f "$0"
`, pid, exeQ, bakQ, exeQ, bakQ, exeQ, newQ, exeQ, newQ, exeQ, newQ, bakQ, exeQ, bakQ, exeQ, exeQ, exeQ)

	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to create update script: %w", err)
	}

	cmd := createCmd("/bin/sh", scriptPath)
	cmd.Dir = os.TempDir()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch update script: %w", err)
	}

	wailsRuntime.Quit(a.ctx)
	return nil
}

// RollbackUpdate restores the .bak binary created by the previous ApplyUpdate.
// Fails with a clear message if no backup exists (first install or user deleted it).
// Like ApplyUpdate, it shells out to a script that runs after the app closes,
// because the running binary cannot be overwritten on Unix while it's executing
// (the kernel keeps the inode alive, but copy semantics differ across FSes).
func (a *App) RollbackUpdate() error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get current program path: %w", err)
	}
	bak := backupPath(currentExe)
	if _, err := os.Stat(bak); err != nil {
		return fmt.Errorf("no backup found at %s: %w", bak, err)
	}

	scriptPath := filepath.Join(os.TempDir(), "svc_rollback.sh")
	pid := os.Getpid()
	exeQ := shellQuote(currentExe)
	bakQ := shellQuote(bak)
	scriptContent := fmt.Sprintf(`#!/bin/sh
echo "Waiting for application to close..."
timeout=60
while kill -0 %d 2>/dev/null; do
    sleep 1
    timeout=$((timeout - 1))
    if [ "$timeout" -le 0 ]; then
        echo "Rollback timed out waiting for app to exit, aborting"
        exit 1
    fi
done
echo "Restoring previous version..."
if ! mv -f %s %s 2>/dev/null; then
    cp -f %s %s && rm -f %s
    if [ $? -ne 0 ]; then
        echo "Rollback failed!"
        exit 1
    fi
fi
chmod +x %s
echo "Starting restored version..."
nohup %s > /dev/null 2>&1 &
rm -f "$0"
`, pid, bakQ, exeQ, bakQ, exeQ, bakQ, exeQ, exeQ)

	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to create rollback script: %w", err)
	}

	cmd := createCmd("/bin/sh", scriptPath)
	cmd.Dir = os.TempDir()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch rollback script: %w", err)
	}

	wailsRuntime.Quit(a.ctx)
	return nil
}
