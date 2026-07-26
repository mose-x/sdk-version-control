//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

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
	// Flow: wait for the app to close, back up current → .bak, copy new → current,
	// chmod +x, relaunch, self-delete. The backup step overwrites any prior
	// .bak so it always holds the immediately previous version (one-shot
	// rollback, matching nvm-rust's behaviour).
	scriptContent := fmt.Sprintf(`#!/bin/sh
echo "Waiting for application to close..."
while pgrep -f "%s" > /dev/null 2>&1; do
    sleep 1
done
echo "Backing up current binary..."
cp -f "%s" "%s"
if [ $? -ne 0 ]; then
    echo "Backup failed, aborting update"
    exit 1
fi
echo "Replacing application..."
cp -f "%s" "%s"
if [ $? -ne 0 ]; then
    echo "Update failed!"
    exit 1
fi
chmod +x "%s"
echo "Starting new version..."
nohup "%s" > /dev/null 2>&1 &
rm -f "$0"
`, filepath.Base(currentExe), currentExe, bak, newExe, currentExe, currentExe, currentExe)

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
	scriptContent := fmt.Sprintf(`#!/bin/sh
echo "Waiting for application to close..."
while pgrep -f "%s" > /dev/null 2>&1; do
    sleep 1
done
echo "Restoring previous version..."
cp -f "%s" "%s"
if [ $? -ne 0 ]; then
    echo "Rollback failed!"
    exit 1
fi
chmod +x "%s"
echo "Starting restored version..."
nohup "%s" > /dev/null 2>&1 &
rm -f "$0"
`, filepath.Base(currentExe), bak, currentExe, currentExe, currentExe)

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
