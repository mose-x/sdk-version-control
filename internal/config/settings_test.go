package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSettingsLoad_KeepsDefaultsOnParseError is the M9 regression test: when
// settings.json is corrupt, the partially-parsed values must NOT leak into the
// in-memory settings — defaults must remain intact.
func TestSettingsLoad_KeepsDefaultsOnParseError(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, ".svc")
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Partial/invalid JSON: "theme" parses, then the trailing "language":
	// (no value) makes json.Unmarshal fail midway.
	if err := os.WriteFile(filepath.Join(svcDir, settingsFile), []byte(`{"theme":"dark","language":`), 0600); err != nil {
		t.Fatal(err)
	}
	sm := NewSettingsManager(dir)
	got := sm.Get()
	// Defaults must be intact, not partially-unmarshaled from the bad file.
	if got.Theme != "system" {
		t.Errorf("Theme: got %q, want default %q (partially-unmarshaled leak)", got.Theme, "system")
	}
	if got.Language != "zh" {
		t.Errorf("Language: got %q, want default %q", got.Language, "zh")
	}
	if got.DownloadThreads != 4 {
		t.Errorf("DownloadThreads: got %d, want default 4", got.DownloadThreads)
	}
}

// TestSettingsLoad_ValidJSON confirms a well-formed file loads into settings.
func TestSettingsLoad_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	svcDir := filepath.Join(dir, ".svc")
	if err := os.MkdirAll(svcDir, 0755); err != nil {
		t.Fatal(err)
	}
	valid := []byte(`{"theme":"dark","language":"en","downloadThreads":8,"proxy":{"enabled":true,"mode":"custom","url":"http://127.0.0.1:7890","protocol":"http"}}`)
	if err := os.WriteFile(filepath.Join(svcDir, settingsFile), valid, 0600); err != nil {
		t.Fatal(err)
	}
	sm := NewSettingsManager(dir)
	got := sm.Get()
	if got.Theme != "dark" || got.Language != "en" || got.DownloadThreads != 8 {
		t.Errorf("unexpected settings: %+v", got)
	}
	if !got.Proxy.Enabled || got.Proxy.Mode != "custom" || got.Proxy.URL != "http://127.0.0.1:7890" {
		t.Errorf("proxy not loaded: %+v", got.Proxy)
	}
}

// TestSettingsLoad_MissingFile keeps defaults when there is no settings.json
// (fresh install / first run).
func TestSettingsLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	sm := NewSettingsManager(dir)
	got := sm.Get()
	if got.Theme != "system" || got.Language != "zh" || got.DownloadThreads != 4 {
		t.Errorf("expected defaults, got %+v", got)
	}
}
