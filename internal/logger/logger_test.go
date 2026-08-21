package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// todayLogFile returns today's log file path under a logs directory.
func todayLogFile(logsDir string) string {
	return filepath.Join(logsDir, fmt.Sprintf("svc-%s.log", time.Now().Format("2006-01-02")))
}

// closeSingleton registers a cleanup that closes the singleton's open log
// file so t.TempDir cleanup can remove the logs directory on Windows (open
// handles block deletion there).
func closeSingleton(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		l := Get()
		if l == nil {
			return
		}
		l.mu.Lock()
		if l.file != nil {
			l.file.Close()
			l.file = nil
			l.currentDate = ""
		}
		l.mu.Unlock()
	})
}

// TestInitAndReinit_WritesToNewDir verifies that Reinit re-points the logger
// singleton at a new base directory: after Reinit, log lines land in
// <newDir>/logs and not in the old directory's log file.
func TestInitAndReinit_WritesToNewDir(t *testing.T) {
	oldBase := t.TempDir()
	newBase := t.TempDir()
	// Register after the TempDir calls: cleanups run LIFO, so the handle is
	// closed BEFORE TempDir cleanup tries to delete the logs dir (Windows
	// cannot delete open files).
	closeSingleton(t)
	Init(oldBase)
	if Get() == nil {
		t.Fatal("logger not initialized after Init")
	}
	Info("before-reinit marker")

	oldLog := todayLogFile(filepath.Join(oldBase, logsDirName))
	if _, err := os.Stat(oldLog); err != nil {
		t.Fatalf("expected old log file %s to exist: %v", oldLog, err)
	}

	if err := Reinit(newBase); err != nil {
		t.Fatalf("Reinit failed: %v", err)
	}
	wantDir := filepath.Join(newBase, logsDirName)
	if got := LogDir(); got != wantDir {
		t.Errorf("LogDir after Reinit: got %q, want %q", got, wantDir)
	}

	Info("after-reinit marker")

	data, err := os.ReadFile(todayLogFile(wantDir))
	if err != nil {
		t.Fatalf("new log file missing after Reinit: %v", err)
	}
	if !strings.Contains(string(data), "after-reinit marker") {
		t.Errorf("new log file missing post-Reinit entry, content: %q", data)
	}
	if strings.Contains(string(data), "before-reinit marker") {
		t.Errorf("new log file unexpectedly contains pre-Reinit entry")
	}
}

// TestReinit_Error verifies Reinit reports failure when the new log file
// cannot be opened (the logs dir cannot be created under a device file).
// Runs last in this file: it leaves the singleton with a nil file handle,
// which makes subsequent package-level writes harmless no-ops.
func TestReinit_Error(t *testing.T) {
	closeSingleton(t)
	// filepath.Join(os.DevNull, "logs") is un-creatable on all platforms
	// (/dev/null/logs on Unix, NUL\logs on Windows).
	if err := Reinit(os.DevNull); err == nil {
		t.Fatal("expected error when the log dir cannot be created, got nil")
	}
}
