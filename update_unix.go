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

func (a *App) ApplyUpdate() error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前程序路径失败: %w", err)
	}

	newExe := getUpdateFilePath()
	if _, err := os.Stat(newExe); err != nil {
		return fmt.Errorf("更新文件不存在: %w", err)
	}

	exeName := filepath.Base(currentExe)
	scriptPath := filepath.Join(os.TempDir(), "svc_updater.sh")
	scriptContent := fmt.Sprintf(`#!/bin/sh
echo "Waiting for application to close..."
while pgrep -f "%s" > /dev/null 2>&1; do
    sleep 1
done
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
`, exeName, newExe, currentExe, currentExe, currentExe)

	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("创建更新脚本失败: %w", err)
	}

	cmd := createCmd("/bin/sh", scriptPath)
	cmd.Dir = os.TempDir()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动更新脚本失败: %w", err)
	}

	wailsRuntime.Quit(a.ctx)
	return nil
}
