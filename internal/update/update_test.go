package update

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"svc/internal/config"
	"svc/internal/downloader"
	"svc/internal/proxy"
)

// newTestUpdater wires an Updater against an httptest releases endpoint with
// no Wails runtime (CheckUpdate never emits events or quits).
func newTestUpdater(t *testing.T, sm *config.SettingsManager, updateURL string) *Updater {
	t.Helper()
	return NewUpdater(AppInfo{UpdateURL: updateURL}, sm, downloader.NewDownloader(), proxy.New(sm), nil)
}

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

	up := newTestUpdater(t, sm, ts.URL+"/releases/latest")
	if _, err := up.CheckUpdate(); err != nil {
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
	up := newTestUpdater(t, sm, ts.URL+"/releases/latest")
	if _, err := up.CheckUpdate(); err != nil {
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

// TestSha256OfFile_roundTrip pins the helper that ApplyUpdate relies on for
// pre-copy hashing: same bytes -> same non-empty digest; different bytes ->
// different digest. ApplyUpdate feeds the digest into the platform script's
// post-copy check, so a regression here silently disables rollback.
// (Cross-platform; lives here so all three CI OSes run it.)
func TestSha256OfFile_roundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake_update.bin")
	payload := []byte("svc-update-payload-v1")
	if err := os.WriteFile(path, payload, 0644); err != nil {
		t.Fatal(err)
	}
	h1, err := sha256OfFile(path)
	if err != nil {
		t.Fatalf("sha256OfFile first call: %v", err)
	}
	if h1 == "" {
		t.Fatal("sha256OfFile returned empty digest")
	}
	h2, err := sha256OfFile(path)
	if err != nil {
		t.Fatalf("sha256OfFile second call: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("sha256OfFile not deterministic: %s then %s", h1, h2)
	}

	// Different bytes -> different digest.
	other := filepath.Join(dir, "other.bin")
	if err := os.WriteFile(other, []byte("different-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	h3, err := sha256OfFile(other)
	if err != nil {
		t.Fatalf("sha256OfFile on other: %v", err)
	}
	if h1 == h3 {
		t.Fatalf("sha256OfFile collision between distinct payloads: %s", h1)
	}
}

// TestParseAppInfo covers valid JSON and the corrupt-payload fallback to the
// safe 0.1.0 defaults.
func TestParseAppInfo(t *testing.T) {
	info := ParseAppInfo([]byte(`{"version":"1.2.3","goVersion":"1.25","license":"MIT","repoUrl":"https://example.invalid/repo","updateUrl":"https://example.invalid/releases/latest"}`))
	if info.Version != "1.2.3" || info.UpdateURL != "https://example.invalid/releases/latest" {
		t.Errorf("ParseAppInfo = %+v; want version 1.2.3 and the configured update URL", info)
	}

	fallback := ParseAppInfo([]byte("{not json"))
	if fallback.Version != "0.1.0" {
		t.Errorf("ParseAppInfo(corrupt).Version = %q; want 0.1.0 fallback", fallback.Version)
	}
	if fb := ParseAppInfo(nil); fb.Version != "0.1.0" {
		t.Errorf("ParseAppInfo(nil).Version = %q; want 0.1.0 fallback", fb.Version)
	}
}
