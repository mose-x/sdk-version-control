package sdk

import (
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"svc/internal/config"
)

func TestIsGithubAPIHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"api.github.com", true},
		{"API.GitHub.COM", true}, // case-insensitive
		{"github.com", false},
		{"www.github.com", false},
		{"mirror.example.com", false},
		{"api.github.com.evil.com", false}, // suffix trick
		{"ghproxy.com", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if got := isGithubAPIHost(tt.host); got != tt.want {
				t.Errorf("isGithubAPIHost(%q) = %v; want %v", tt.host, got, tt.want)
			}
		})
	}
}

// newTestSettingsManager builds a SettingsManager backed by a temp home dir
// (never the real ~/.svc) with the given endpoint map and optional token.
func newTestSettingsManager(t *testing.T, endpoints map[string]string, githubToken string) *config.SettingsManager {
	t.Helper()
	sm := config.NewSettingsManager(t.TempDir())
	if err := sm.Update(config.AppSettings{
		Endpoints:   endpoints,
		GitHubToken: githubToken,
	}); err != nil {
		t.Fatalf("failed to write test settings: %v", err)
	}
	return sm
}

func TestApplyGithubEndpoint(t *testing.T) {
	const apiURL = "https://api.github.com/repos/foo/bar/releases?per_page=30"
	const dlURL = "https://github.com/foo/bar/releases/download/v1/asset.zip"

	tests := []struct {
		name       string
		endpoint   string // custom endpoint for the SDK ("" = unset)
		nilSM      bool
		input      string
		want       string
		wantPrefix string // optional: assert exact single-prefixing
	}{
		{
			name:  "nil settings manager leaves URL untouched",
			nilSM: true,
			input: apiURL,
			want:  apiURL,
		},
		{
			name:     "empty endpoint leaves URL untouched",
			endpoint: "",
			input:    apiURL,
			want:     apiURL,
		},
		{
			name:     "api host replaced",
			endpoint: "https://mirror.example.com",
			input:    apiURL,
			want:     "https://mirror.example.com/repos/foo/bar/releases?per_page=30",
		},
		{
			name:     "download host replaced",
			endpoint: "https://mirror.example.com",
			input:    dlURL,
			want:     "https://mirror.example.com/foo/bar/releases/download/v1/asset.zip",
		},
		{
			// ghproxy-style custom endpoints contain "github.com" themselves;
			// the old chained-Replace code replaced the just-inserted prefix
			// again, doubling the mirror. Exactly one substitution must occur.
			name:     "ghproxy-style custom does not double-prefix api URL",
			endpoint: "https://ghproxy.com/https://github.com",
			input:    apiURL,
			want:     "https://ghproxy.com/https://github.com/repos/foo/bar/releases?per_page=30",
		},
		{
			name:     "ghproxy-style custom does not double-prefix download URL",
			endpoint: "https://ghproxy.com/https://github.com",
			input:    dlURL,
			want:     "https://ghproxy.com/https://github.com/foo/bar/releases/download/v1/asset.zip",
		},
		{
			name:     "non-github URL untouched",
			endpoint: "https://mirror.example.com",
			input:    "https://nodejs.org/dist/index.json",
			want:     "https://nodejs.org/dist/index.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sm *config.SettingsManager
			if !tt.nilSM {
				sm = newTestSettingsManager(t, map[string]string{string(Ruby): tt.endpoint}, "")
			}
			got := applyGithubEndpoint(sm, Ruby, tt.input)
			if got != tt.want {
				t.Errorf("applyGithubEndpoint(...) = %q; want %q", got, tt.want)
			}
			// The mirror prefix must appear exactly once in the output.
			if tt.endpoint != "" && strings.HasPrefix(tt.input, "https://") && got != tt.input {
				if n := strings.Count(got, "https://ghproxy.com/"); tt.endpoint == "https://ghproxy.com/https://github.com" && n != 1 {
					t.Errorf("ghproxy prefix appears %d times in %q; want exactly 1", n, got)
				}
			}
		})
	}
}

// recordingGithubTransport records every request's URL + Authorization header
// and answers with a canned 200 body, so token scoping can be asserted without
// touching the network.
type recordingGithubTransport struct {
	mu     sync.Mutex
	urls   []string
	auths  []string
	body   string
	status int
}

func (t *recordingGithubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.urls = append(t.urls, req.URL.String())
	t.auths = append(t.auths, req.Header.Get("Authorization"))
	t.mu.Unlock()
	status := t.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Header:     make(http.Header),
	}, nil
}

func (t *recordingGithubTransport) snapshot() ([]string, []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.urls...), append([]string(nil), t.auths...)
}

// TestFetchGithubReleasesPageTokenScoping verifies the PAT is attached ONLY
// when the request goes to the real api.github.com host. Per-SDK custom
// endpoints rewrite the URL host (python/ruby/perl/php) and must never
// receive the token, even when the rewritten URL still contains the literal
// string "github.com" (ghproxy-style mirrors).
func TestFetchGithubReleasesPageTokenScoping(t *testing.T) {
	const rawToken = "ghp_testtoken123456"
	encToken := base64.StdEncoding.EncodeToString([]byte(rawToken))

	tests := []struct {
		name     string
		url      string
		wantAuth string // expected Authorization header on the primary request
	}{
		{
			name:     "real github API receives the token",
			url:      "https://api.github.com/repos/foo/bar/releases",
			wantAuth: "Bearer " + rawToken,
		},
		{
			name:     "rewritten host receives no token",
			url:      "https://mirror.example.com/repos/foo/bar/releases",
			wantAuth: "",
		},
		{
			// Contains "api.github.com" as a substring but the HOST is the
			// proxy: the old strings.Contains gate mishandled exactly this.
			name:     "ghproxy-style URL receives no token",
			url:      "https://ghproxy.com/https://api.github.com/repos/foo/bar/releases",
			wantAuth: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := newTestSettingsManager(t, nil, encToken)
			tr := &recordingGithubTransport{body: "[]"}
			client := &http.Client{Transport: tr}

			var releases []ghRelease
			if err := fetchGithubReleasesPage(sm, client, tt.url, &releases); err != nil {
				t.Fatalf("fetchGithubReleasesPage: %v", err)
			}
			urls, auths := tr.snapshot()
			if len(urls) != 1 {
				t.Fatalf("expected exactly 1 request (primary), got %d: %v", len(urls), urls)
			}
			if auths[0] != tt.wantAuth {
				t.Errorf("Authorization header = %q; want %q", auths[0], tt.wantAuth)
			}
		})
	}
}

// TestFetchGithubReleasesPageNoTokenUnchanged checks that without a token no
// Authorization header is ever sent, regardless of the URL.
func TestFetchGithubReleasesPageNoTokenUnchanged(t *testing.T) {
	sm := newTestSettingsManager(t, nil, "")
	tr := &recordingGithubTransport{body: "[]"}
	client := &http.Client{Transport: tr}

	var releases []ghRelease
	if err := fetchGithubReleasesPage(sm, client, "https://api.github.com/repos/foo/bar/releases", &releases); err != nil {
		t.Fatalf("fetchGithubReleasesPage: %v", err)
	}
	_, auths := tr.snapshot()
	if len(auths) != 1 || auths[0] != "" {
		t.Errorf("expected no Authorization header without a token, got %v", auths)
	}
}
