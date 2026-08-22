package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"sdk_version_control/internal/config"
	"sdk_version_control/internal/proxy"
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

	app := &App{settings: sm, proxySvc: proxy.New(sm), appInfo: AppInfo{UpdateURL: ts.URL + "/releases/latest"}}
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

	sm := config.NewSettingsManager(t.TempDir())
	app := &App{settings: sm, proxySvc: proxy.New(sm), appInfo: AppInfo{UpdateURL: ts.URL + "/releases/latest"}}
	if _, err := app.CheckUpdate(); err != nil {
		t.Fatalf("CheckUpdate returned error: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q; want empty (no token configured)", gotAuth)
	}
}

// TestSha256Matches pins the case-insensitive digest comparison used by
// DownloadUpdate's integrity check. sha256OfFile always returns lowercase
// while release manifests may publish uppercase hex; a case-sensitive
// comparison would wrongly reject valid downloads.
func TestSha256Matches(t *testing.T) {
	const lower = "deadbeefcafef00dba5eba11cafebabedeadbeefcafef00dba5eba11cafebabe"
	tests := []struct {
		name     string
		actual   string
		expected string
		want     bool
	}{
		{"identical lowercase", lower, lower, true},
		{"uppercase manifest vs lowercase computed", lower, "DEADBEEFCAFEF00DBA5EBA11CAFEBABEDEADBEEFCAFEF00DBA5EBA11CAFEBABE", true},
		{"mixed case both sides", "DeadBeef" + lower[8:], "dEADbEEF" + lower[8:], true},
		{"surrounding whitespace tolerated", lower + "\n", "  " + lower, true},
		{"different digest rejected", lower, "0000000000000000000000000000000000000000000000000000000000000000", false},
		{"prefix digest rejected", lower, lower[:63] + "0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sha256Matches(tt.actual, tt.expected); got != tt.want {
				t.Errorf("sha256Matches(%q, %q) = %v; want %v", tt.actual, tt.expected, got, tt.want)
			}
		})
	}
}
