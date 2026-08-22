// Package logmgr exposes the application log files (listing, reading,
// cleaning, deleting) with path-segment validation on every user-supplied
// filename.
package logmgr

import (
	"sdk_version_control/internal/helpers"
	"sdk_version_control/internal/logger"
)

// GetLogFiles lists the log files in the active log directory.
func GetLogFiles() ([]logger.LogFileInfo, error) {
	return logger.ListLogFiles()
}

// GetLogContent returns the content of one log file.
func GetLogContent(filename string) (string, error) {
	if err := helpers.ValidatePathSegment(filename); err != nil {
		return "", err
	}
	return logger.GetLogContent(filename)
}

// CleanLogs removes all log files and reopens today's file.
func CleanLogs() error {
	logger.Info("Cleaning log files...")
	err := logger.CleanLogs()
	if err != nil {
		logger.Error("Failed to clean logs: %v", err)
		return err
	}
	logger.Info("Log files cleaned successfully")
	return nil
}

// DeleteLogFile removes a single log file.
func DeleteLogFile(filename string) error {
	if err := helpers.ValidatePathSegment(filename); err != nil {
		return err
	}
	logger.Info("Deleting log file: %s", filename)
	err := logger.DeleteLogFile(filename)
	if err != nil {
		logger.Error("Failed to delete log file %s: %v", filename, err)
		return err
	}
	return nil
}

// GetLogDir returns the active log directory path.
func GetLogDir() string {
	return logger.LogDir()
}
