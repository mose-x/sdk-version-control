package settings

import (
	"testing"

	"sdk_version_control/internal/config"
)

// TestSavePreservesEndpointsAndInstallPath pins the stale-snapshot
// fix: SettingsManager.Update replaces the WHOLE object, and the frontend
// echoes back a snapshot that can predate SaveEndpoints / MigrateInstallPath
// writes. Save must preserve GitHubToken, Endpoints and InstallPath
// from the stored settings instead of letting the snapshot clobber them,
// while still applying the fields the user actually edited (theme etc.).
func TestSavePreservesEndpointsAndInstallPath(t *testing.T) {
	sm := config.NewSettingsManager(t.TempDir())
	svc := New(sm)

	// Endpoints written via the dedicated flow.
	endpoints := map[string]string{"go": "https://goproxy.cn", "jdk": "https://mirrors.example/adoptium"}
	if err := svc.SaveEndpoints(endpoints); err != nil {
		t.Fatalf("SaveEndpoints: %v", err)
	}

	// Token written via the dedicated flow (stored base64-encoded).
	if err := svc.SaveGithubToken("ghp_secrettoken123"); err != nil {
		t.Fatalf("SaveGithubToken: %v", err)
	}

	// InstallPath written the same way MigrateInstallPath persists it:
	// a direct SettingsManager.Update bypassing Save.
	migrated := sm.Get()
	migrated.InstallPath = "/custom/svc-install"
	if err := sm.Update(migrated); err != nil {
		t.Fatalf("sm.Update (migrate-style): %v", err)
	}

	// The frontend now echoes back a STALE snapshot: it predates all three
	// writes above, so endpoints/installPath are empty and the token is the
	// masked form. Only the theme actually changed.
	stale := config.AppSettings{
		Theme:           "dark",
		Language:        "en",
		DownloadThreads: 8,
		GitHubToken:     "ghp_se***3123", // masked junk from Get
		Endpoints:       nil,
		InstallPath:     "",
	}
	if err := svc.Save(stale); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := sm.Get()
	// The edited fields are applied.
	if got.Theme != "dark" {
		t.Errorf("Theme = %q; want %q (Save must apply real edits)", got.Theme, "dark")
	}
	if got.Language != "en" {
		t.Errorf("Language = %q; want %q", got.Language, "en")
	}
	if got.DownloadThreads != 8 {
		t.Errorf("DownloadThreads = %d; want 8", got.DownloadThreads)
	}
	// The owned fields are preserved, NOT clobbered by the stale snapshot.
	if got.Endpoints["go"] != "https://goproxy.cn" || got.Endpoints["jdk"] != "https://mirrors.example/adoptium" {
		t.Errorf("Endpoints clobbered by stale snapshot: %v", got.Endpoints)
	}
	if got.InstallPath != "/custom/svc-install" {
		t.Errorf("InstallPath = %q; want %q (stale snapshot must not revert migration)", got.InstallPath, "/custom/svc-install")
	}
	if got.GitHubToken == "" || got.GitHubToken == "ghp_se***3123" {
		t.Errorf("GitHubToken clobbered by stale snapshot: %q", got.GitHubToken)
	}
}

// TestSavePersistsToEndpointsReload checks the preserved values
// survive a fresh SettingsManager load from disk (they were actually written
// to settings.json, not just kept in memory).
func TestSavePersistsToEndpointsReload(t *testing.T) {
	home := t.TempDir()
	sm := config.NewSettingsManager(home)
	svc := New(sm)

	if err := svc.SaveEndpoints(map[string]string{"go": "https://goproxy.cn"}); err != nil {
		t.Fatalf("SaveEndpoints: %v", err)
	}
	migrated := sm.Get()
	migrated.InstallPath = "/custom/svc-install"
	if err := sm.Update(migrated); err != nil {
		t.Fatal(err)
	}

	if err := svc.Save(config.AppSettings{Theme: "light", Language: "zh"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload from disk.
	sm2 := config.NewSettingsManager(home)
	got := sm2.Get()
	if got.Endpoints["go"] != "https://goproxy.cn" {
		t.Errorf("reloaded Endpoints = %v; want go endpoint preserved", got.Endpoints)
	}
	if got.InstallPath != "/custom/svc-install" {
		t.Errorf("reloaded InstallPath = %q; want /custom/svc-install", got.InstallPath)
	}
	if got.Theme != "light" {
		t.Errorf("reloaded Theme = %q; want light", got.Theme)
	}
}

// TestGetMasksToken verifies the binding never returns the raw token.
func TestGetMasksToken(t *testing.T) {
	sm := config.NewSettingsManager(t.TempDir())
	svc := New(sm)
	if err := svc.SaveGithubToken("ghp_abcdef123456"); err != nil {
		t.Fatal(err)
	}
	got := svc.Get()
	if got.GitHubToken == "ghp_abcdef123456" || got.GitHubToken == "" {
		t.Errorf("Get().GitHubToken = %q; want masked form", got.GitHubToken)
	}
	if stored := sm.Get().GitHubToken; stored == got.GitHubToken {
		t.Error("stored token equals the masked display value")
	}
}

// TestGetEndpointsNeverNil pins the non-nil contract the frontend relies on.
func TestGetEndpointsNeverNil(t *testing.T) {
	svc := New(config.NewSettingsManager(t.TempDir()))
	if got := svc.GetEndpoints(); got == nil {
		t.Error("GetEndpoints returned nil; want empty map")
	}
}
