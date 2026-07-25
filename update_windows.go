package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func getUpdateFilePath() string {
	return filepath.Join(os.TempDir(), "svc_update_new.exe")
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

	batSpecialChars := `&|<>^%"'`
	for _, p := range []string{currentExe, newExe} {
		if strings.ContainsAny(p, batSpecialChars) {
			return fmt.Errorf("program path contains illegal characters: %s", p)
		}
	}

	batPath := filepath.Join(os.TempDir(), "svc_updater.bat")
	batContent := fmt.Sprintf(`@echo off
echo Waiting for application to close...
:WAIT_LOOP
tasklist /FI "IMAGENAME eq %s" 2>NUL | find /I "%s" >NUL
if "%%ERRORLEVEL%%"=="0" (
    timeout /t 1 /nobreak >NUL
    goto WAIT_LOOP
)
echo Replacing application...
copy /Y "%s" "%s" >NUL
if errorlevel 1 (
    echo Update failed!
    pause
    exit /b 1
)
echo Starting new version...
start "" "%s"
del "%%~f0"
`, filepath.Base(currentExe), filepath.Base(currentExe), newExe, currentExe, currentExe)

	if err := os.WriteFile(batPath, []byte(batContent), 0644); err != nil {
		return fmt.Errorf("failed to create update script: %w", err)
	}

	cmd := createCmd("cmd", "/c", "start", "/b", batPath)
	cmd.Dir = os.TempDir()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch update script: %w", err)
	}

	wailsRuntime.Quit(a.ctx)
	return nil
}
