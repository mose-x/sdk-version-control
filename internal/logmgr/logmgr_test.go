package logmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sdk_version_control/internal/logger"
)

// initLogger points the package logger singleton at a temp dir once per test
// process. logger.Init is sync.Once-guarded; the first call wins, so tests
// share one log dir (they only exercise file operations, not contents).
// NOTE: the singleton keeps the log file open for the process lifetime, so
// t.TempDir (whose cleanup fails on open files on Windows) cannot be used;
// cleanup is best-effort instead.
func initLogger(t *testing.T) string {
	t.Helper()
	base, err := os.MkdirTemp("", "logmgr-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	logger.Init(base)
	return filepath.Join(base, "logs")
}

// TestFilenameValidation pins that user-supplied filenames are validated
// before touching the filesystem: traversal, separators and empty names are
// rejected for both read and delete paths.
func TestFilenameValidation(t *testing.T) {
	invalid := []string{"", "..", "../etc/passwd", `a\b`, "a/b", "CON", "svc-2026.log\x00"}
	for _, name := range invalid {
		if _, err := GetLogContent(name); err == nil {
			t.Errorf("GetLogContent(%q) = nil error; want validation failure", name)
		}
		if err := DeleteLogFile(name); err == nil {
			t.Errorf("DeleteLogFile(%q) = nil error; want validation failure", name)
		}
	}
}

// TestLogLifecycle exercises list/read/clean/delete on a real log dir.
func TestLogLifecycle(t *testing.T) {
	logsDir := initLogger(t)

	// Generate today's log file via a real write.
	logger.Info("logmgr test marker")

	files, err := GetLogFiles()
	if err != nil {
		t.Fatalf("GetLogFiles: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one log file after logger.Info")
	}
	name := files[0].Name

	content, err := GetLogContent(name)
	if err != nil {
		t.Fatalf("GetLogContent(%q): %v", name, err)
	}
	if content == "" {
		t.Error("log file content is empty")
	}

	if err := DeleteLogFile(name); err != nil {
		t.Fatalf("DeleteLogFile(%q): %v", name, err)
	}
	// Today's active log is recreated immediately after deletion (the
	// logger must always have an open file), so the observable effect is
	// the content being reset, not the file disappearing.
	after, err := GetLogContent(name)
	if err != nil {
		t.Fatalf("GetLogContent after delete: %v", err)
	}
	if strings.Contains(after, "logmgr test marker") {
		t.Error("deleted log content survived")
	}

	// CleanLogs must succeed even on an empty/missing dir and recreate the
	// active file.
	if err := CleanLogs(); err != nil {
		t.Fatalf("CleanLogs: %v", err)
	}
	if dir := GetLogDir(); dir != logsDir {
		t.Errorf("GetLogDir = %q; want %q", dir, logsDir)
	}
}
