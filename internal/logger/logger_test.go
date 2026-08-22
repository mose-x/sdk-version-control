package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// todayLogFile returns today's log file path under a logs directory.
func todayLogFile(logsDir string) string {
	return filepath.Join(logsDir, fmt.Sprintf("svc-%s.log", time.Now().Format("2006-01-02")))
}

// TestListLogFiles_Descending verifies logs are listed newest-first so the
// most recent day is at the top for troubleshooting.
func TestListLogFiles_Descending(t *testing.T) {
	base := t.TempDir()
	if err := Reinit(base); err != nil {
		t.Fatal(err)
	}
	closeSingleton(t)
	logsDir := filepath.Join(base, "logs")
	// A few older dated files plus today's (created by Reinit).
	for _, d := range []string{"2026-01-01", "2026-01-02", "2026-01-03"} {
		if err := os.WriteFile(filepath.Join(logsDir, "svc-"+d+".log"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := ListLogFiles()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(files); i++ {
		if files[i-1].Name < files[i].Name {
			t.Errorf("logs not descending: %q before %q", files[i-1].Name, files[i].Name)
		}
	}
	if len(files) == 0 || !strings.HasPrefix(files[0].Name, "svc-") {
		t.Errorf("expected newest log first, got %v", files)
	}
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

// TestReinit_OldInstanceStaysDead verifies that a write captured the old
// instance before Reinit cannot reopen the old log file (rotateFile on a
// closed instance must be a no-op) -- otherwise the migration's
// RemoveAll(oldDir) fails on Windows due to the resurrected handle.
func TestReinit_OldInstanceStaysDead(t *testing.T) {
	baseA := t.TempDir()
	baseB := t.TempDir()
	closeSingleton(t)

	if err := Reinit(baseA); err != nil {
		t.Fatalf("Reinit(baseA): %v", err)
	}
	old := Get()
	Info("old-instance marker")
	oldLog := todayLogFile(filepath.Join(baseA, logsDirName))
	before, err := os.ReadFile(oldLog)
	if err != nil {
		t.Fatalf("expected old log file to exist: %v", err)
	}

	if err := Reinit(baseB); err != nil {
		t.Fatalf("Reinit(baseB): %v", err)
	}
	// Simulate a writer that captured the old instance before the swap.
	old.write(LevelInfo, "attempted resurrection")

	after, err := os.ReadFile(oldLog)
	if err != nil {
		t.Fatalf("old log file disappeared: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("old log file changed after Reinit (resurrected write):\n%q", string(after))
	}
	if strings.Contains(string(after), "attempted resurrection") {
		t.Error("superseded instance wrote to the old log file")
	}
	if got, want := LogDir(), filepath.Join(baseB, logsDirName); got != want {
		t.Errorf("LogDir = %q, want %q", got, want)
	}
}

// TestReinit_ConcurrentWrites is a smoke test: writers hammering the logger
// while Reinit swaps the singleton must not panic or deadlock.
func TestReinit_ConcurrentWrites(t *testing.T) {
	baseA := t.TempDir()
	baseB := t.TempDir()
	closeSingleton(t)
	if err := Reinit(baseA); err != nil {
		t.Fatalf("Reinit(baseA): %v", err)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					Info("concurrent write")
				}
			}
		}()
	}
	for i := 0; i < 5; i++ {
		if i%2 == 0 {
			Reinit(baseB)
		} else {
			Reinit(baseA)
		}
	}
	close(done)
	// Block until every writer has exited; otherwise a goroutine still in
	// Info() during t.Cleanup reopens the log file and TempDir cleanup fails
	// on Windows (file in use).
	wg.Wait()
	Info("final marker")
	if Get() == nil {
		t.Fatal("singleton nil after concurrent Reinit")
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
