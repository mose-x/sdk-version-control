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
		return fmt.Errorf("获取当前程序路径失败: %w", err)
	}

	newExe := getUpdateFilePath()
	if _, err := os.Stat(newExe); err != nil {
		return fmt.Errorf("更新文件不存在: %w", err)
	}

	batSpecialChars := `&|<>^%"'`
	for _, p := range []string{currentExe, newExe} {
		if strings.ContainsAny(p, batSpecialChars) {
			return fmt.Errorf("程序路径包含非法字符: %s", p)
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
		return fmt.Errorf("创建更新脚本失败: %w", err)
	}

	cmd := createCmd("cmd", "/c", "start", "/b", batPath)
	cmd.Dir = os.TempDir()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动更新脚本失败: %w", err)
	}

	wailsRuntime.Quit(a.ctx)
	return nil
}
