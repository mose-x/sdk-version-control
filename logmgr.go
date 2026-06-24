package main

import (
	"sdk_version_control/internal/logger"
)

func (a *App) GetLogFiles() ([]logger.LogFileInfo, error) {
	return logger.ListLogFiles()
}

func (a *App) GetLogContent(filename string) (string, error) {
	return logger.GetLogContent(filename)
}

func (a *App) CleanLogs() error {
	logger.Info("Cleaning log files...")
	err := logger.CleanLogs()
	if err != nil {
		logger.Error("Failed to clean logs: %v", err)
		return err
	}
	logger.Info("Log files cleaned successfully")
	return nil
}

func (a *App) DeleteLogFile(filename string) error {
	logger.Info("Deleting log file: %s", filename)
	err := logger.DeleteLogFile(filename)
	if err != nil {
		logger.Error("Failed to delete log file %s: %v", filename, err)
		return err
	}
	return nil
}

func (a *App) GetLogDir() string {
	return logger.LogDir()
}
