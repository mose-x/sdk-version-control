package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"sdk_version_control/internal/config"
)

func TestCheckUpdateSendsAuthorizationWhenTokenSet(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer ts.Close()

	sm := config.NewSettingsManager(t.TempDir())
	s := sm.Get()
	s.GitHubToken = base64.StdEncoding.EncodeToString([]byte("ghp_testtoken"))
	if err := sm.Update(s); err != nil {
		t.Fatalf("sm.Update: %v", err)
	}

	app := &App{settings: sm, appInfo: AppInfo{UpdateURL: ts.URL + "/releases/latest"}}
	if _, err := app.CheckUpdate(); err != nil {
		t.Fatalf("CheckUpdate returned error: %v", err)
	}
	if want := "Bearer ghp_testtoken"; gotAuth != want {
		t.Errorf("Authorization header = %q; want %q", gotAuth, want)
	}
}

func TestCheckUpdateOmitsAuthorizationWhenNoToken(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer ts.Close()

	app := &App{settings: config.NewSettingsManager(t.TempDir()), appInfo: AppInfo{UpdateURL: ts.URL + "/releases/latest"}}
	if _, err := app.CheckUpdate(); err != nil {
		t.Fatalf("CheckUpdate returned error: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q; want empty (no token configured)", gotAuth)
	}
}
