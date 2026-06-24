package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type LogLevel int

const (
	LevelInfo LogLevel = iota
	LevelWarn
	LevelError
)

const logsDirName = "logs"

type Logger struct {
	mu      sync.Mutex
	logDir  string
	file    *os.File
	currentDate string
}

var instance *Logger
var once sync.Once

func Init(logDir string) {
	once.Do(func() {
		instance = &Logger{
			logDir: filepath.Join(logDir, logsDirName),
		}
		instance.ensureLogDir()
		instance.rotateFile()
	})
}

func Get() *Logger {
	return instance
}

func (l *Logger) ensureLogDir() {
	os.MkdirAll(l.logDir, 0755)
}

func (l *Logger) rotateFile() {
	today := time.Now().Format("2006-01-02")
	if today == l.currentDate && l.file != nil {
		return
	}

	if l.file != nil {
		l.file.Close()
	}

	filename := fmt.Sprintf("svc-%s.log", today)
	filePath := filepath.Join(l.logDir, filename)
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		l.file = nil
		l.currentDate = ""
		return
	}

	l.file = f
	l.currentDate = today
}

func (l *Logger) write(level LogLevel, format string, args ...interface{}) {
	if l == nil || l.file == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.rotateFile()
	if l.file == nil {
		return
	}

	levelStr := "INFO"
	switch level {
	case LevelWarn:
		levelStr = "WARN"
	case LevelError:
		levelStr = "ERROR"
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] [%s] %s\n", timestamp, levelStr, msg)

	l.file.WriteString(line)
	l.file.Sync()
}

func Info(format string, args ...interface{}) {
	if l := Get(); l != nil {
		l.write(LevelInfo, format, args...)
	}
}

func Warn(format string, args ...interface{}) {
	if l := Get(); l != nil {
		l.write(LevelWarn, format, args...)
	}
}

func Error(format string, args ...interface{}) {
	if l := Get(); l != nil {
		l.write(LevelError, format, args...)
	}
}

func LogDir() string {
	if l := Get(); l != nil {
		return l.logDir
	}
	return ""
}

type LogFileInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

func ListLogFiles() ([]LogFileInfo, error) {
	l := Get()
	if l == nil {
		return nil, fmt.Errorf("logger not initialized")
	}

	entries, err := os.ReadDir(l.logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []LogFileInfo{}, nil
		}
		return nil, err
	}

	var files []LogFileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, LogFileInfo{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}

	return files, nil
}

func GetLogContent(filename string) (string, error) {
	l := Get()
	if l == nil {
		return "", fmt.Errorf("logger not initialized")
	}

	if err := validateFilename(filename); err != nil {
		return "", err
	}

	filePath := filepath.Join(l.logDir, filename)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func CleanLogs() error {
	l := Get()
	if l == nil {
		return fmt.Errorf("logger not initialized")
	}

	entries, err := os.ReadDir(l.logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		l.file.Close()
		l.file = nil
		l.currentDate = ""
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		os.Remove(filepath.Join(l.logDir, e.Name()))
	}

	l.rotateFile()

	return nil
}

func DeleteLogFile(filename string) error {
	l := Get()
	if l == nil {
		return fmt.Errorf("logger not initialized")
	}

	if err := validateFilename(filename); err != nil {
		return err
	}

	filePath := filepath.Join(l.logDir, filename)

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		currentFile := filepath.Base(l.file.Name())
		if currentFile == filename {
			l.file.Close()
			l.file = nil
			l.currentDate = ""
		}
	}

	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if l.file == nil {
		l.rotateFile()
	}

	return nil
}

func validateFilename(filename string) error {
	if filename == "" || filepath.Base(filename) != filename {
		return fmt.Errorf("invalid filename")
	}
	if len(filename) > 255 {
		return fmt.Errorf("filename too long")
	}
	return nil
}
