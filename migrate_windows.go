//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"svc/internal/config"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// legacyExeName is the pre-rename executable name ("SDK Version Control"
// wails project name + .exe). Self-updated pre-2.0.0 installs still run
// under this name from the old folder; installer-based installs are handled
// by the NSIS migration instead.
const legacyExeName = "SDK Version Control.exe"

// isLegacyWindowsInstall reports whether the running binary is the old-named
// executable, i.e. a self-updated install that never went through the rename
// migration.
func isLegacyWindowsInstall() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Base(exe), legacyExeName)
}

// buildLegacyRenameScript renders the migration .bat: wait for the app to
// exit, rename the install folder to "svc" when the parent directory allows
// it (falls back to exe-only rename otherwise), rename the executable to
// svc.exe (leaving svc.exe.bak so RollbackUpdate keeps working), recreate
// shortcuts where legacy ones exist, and relaunch. Extracted for testing.
func buildLegacyRenameScript(pid int, currentExe string) string {
	return fmt.Sprintf(`@echo off
setlocal EnableExtensions
echo Completing svc rename migration...
echo Waiting for application to close...
set /a timeout=60
:waitloop
tasklist /FI "PID eq %d" /NH 2>NUL | findstr /C:" %d " >NUL
if not errorlevel 1 (
    timeout /t 1 /nobreak >NUL
    set /a timeout-=1
    if %%timeout%% leq 0 (
        echo Migration aborted: application did not exit in time
        exit /b 1
    )
    goto waitloop
)

set "OLD_EXE=%s"
for %%F in ("%%OLD_EXE%%") do set "OLD_DIR=%%~dpF"
if "%%OLD_DIR:~-1%%"=="\" set "OLD_DIR=%%OLD_DIR:~0,-1%%"
for %%F in ("%%OLD_DIR%%") do set "PARENT_DIR=%%~dpF"
if "%%PARENT_DIR:~-1%%"=="\" set "PARENT_DIR=%%PARENT_DIR:~0,-1%%"
set "NEW_DIR=%%PARENT_DIR%%\svc"
set "OLD_NAME=SDK Version Control.exe"

rem Rename the install folder when the parent directory is writable
rem (Program Files usually needs the elevation this script runs with);
rem otherwise fall back to renaming only the executable.
if /i not "%%OLD_DIR%%"=="%%NEW_DIR%%" (
    move /Y "%%OLD_DIR%%" "%%NEW_DIR%%" >NUL 2>&1
    if errorlevel 1 set "NEW_DIR=%%OLD_DIR%%"
)

rem Rename the executable. The backup uses the name RollbackUpdate expects
rem (<current exe>.bak) so rollback keeps working after the migration.
if exist "%%NEW_DIR%%\%%OLD_NAME%%" (
    copy /Y "%%NEW_DIR%%\%%OLD_NAME%%" "%%NEW_DIR%%\svc.exe.bak" >NUL
    ren "%%NEW_DIR%%\%%OLD_NAME%%" "svc.exe"
)
if exist "%%NEW_DIR%%\svc.exe" (set "NEW_EXE=%%NEW_DIR%%\svc.exe") else (set "NEW_EXE=%%NEW_DIR%%\%%OLD_NAME%%")

rem Recreate shortcuts only where legacy ones exist (never add shortcuts
rem where the user had none).
powershell -NoProfile -ExecutionPolicy Bypass -Command "$ws = New-Object -ComObject WScript.Shell; $t = \"%%NEW_EXE%%\"; foreach ($b in @([Environment]::GetFolderPath('Desktop'), [Environment]::GetFolderPath('Programs'), 'C:\Users\Public\Desktop', [Environment]::GetFolderPath('CommonPrograms'))) { $o = Join-Path $b 'SDK Version Control.lnk'; if (Test-Path $o) { Remove-Item $o -Force; $s = $ws.CreateShortcut((Join-Path $b 'svc.lnk')); $s.TargetPath = $t; $s.Save() } }"

echo Starting svc...
start "" "%%NEW_EXE%%"
exit /b 0
`, pid, pid, currentExe)
}

// launchLegacyRenameMigration writes the migration script to a temp file and
// starts it ELEVATED (renaming inside Program Files requires admin). It
// waits for the UAC decision: if the user declines, the script never runs
// and an error is returned so the app can stay up.
func launchLegacyRenameMigration(currentExe string) error {
	script := buildLegacyRenameScript(os.Getpid(), currentExe)
	f, err := os.CreateTemp(os.TempDir(), "svc_migrate_*.bat")
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

	ps := fmt.Sprintf("Start-Process -FilePath \"%s\" -Verb RunAs", scriptPath)
	cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
	if err := cmd.Start(); err != nil {
		os.Remove(scriptPath)
		return fmt.Errorf("failed to launch migration: %w", err)
	}
	// cmd.Wait returns once the UAC prompt is answered: non-zero means the
	// user declined (or elevation failed) and the script never ran.
	if err := cmd.Wait(); err != nil {
		os.Remove(scriptPath)
		return fmt.Errorf("migration was not started (elevation declined or failed): %w", err)
	}
	return nil
}

// maybeShowLegacyMigrationPrompt offers the one-time rename migration on
// startup when the app detects it is still the old-named executable. On
// acceptance the elevated migration script runs and the app quits (the
// script relaunches it as svc.exe). On decline nothing changes and the
// prompt comes back on the next launch.
func maybeShowLegacyMigrationPrompt(ctx context.Context, sm *config.SettingsManager) {
	if !isLegacyWindowsInstall() {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}

	title := "svc migration"
	message := "This app is still installed under the old name (SDK Version Control). " +
		"Complete the rename now? It will briefly request administrator rights, " +
		"rename the install folder and executable to svc, and restart automatically."
	yes, no := "Yes", "No"
	if sm != nil && sm.Get().Language == "zh" {
		title = "svc 改名迁移"
		message = "检测到仍以旧名称（SDK Version Control）安装。是否现在完成改名？" +
			"将请求管理员权限，把安装文件夹和可执行文件改名为 svc，然后自动重启。"
		yes, no = "是", "否"
	}

	resp, err := wailsRuntime.MessageDialog(ctx, wailsRuntime.MessageDialogOptions{
		Title:         title,
		Message:       message,
		Buttons:       []string{yes, no},
		DefaultButton: yes,
	})
	if err != nil || resp != yes {
		return
	}

	if err := launchLegacyRenameMigration(exe); err != nil {
		wailsRuntime.MessageDialog(ctx, wailsRuntime.MessageDialogOptions{
			Title:   title,
			Message: fmt.Sprintf("%v", err),
		})
		return
	}
	// The script waits for this process to exit before touching anything.
	wailsRuntime.Quit(ctx)
}
