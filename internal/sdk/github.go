package sdk

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"sdk_version_control/internal/config"
)

// githubReleasesUserAgent is sent on every GitHub API request. GitHub rejects
// requests without a User-Agent with 403, which is indistinguishable from a
// rate-limit 403 unless we set one.
const githubReleasesUserAgent = "sdk-version-control"

// DecodeGithubToken returns the plaintext GitHub PAT stored (base64-encoded)
// in settings, or "" when none is configured. A malformed base64 value is
// treated as empty so a corrupted settings.json never breaks version listing.
func DecodeGithubToken(sm *config.SettingsManager) string {
	if sm == nil {
		return ""
	}
	enc := sm.Get().GitHubToken
	if enc == "" {
		return ""
	}
	dec, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return ""
	}
	return string(dec)
}

// MaskGithubToken renders a token as "<first6>***<last6>" for safe display in
// the UI. Tokens shorter than 12 chars are fully masked ("***") so the token
// body is never reconstructable from the mask. Returns "" when token is empty.
func MaskGithubToken(sm *config.SettingsManager) string {
	tok := DecodeGithubToken(sm)
	if tok == "" {
		return ""
	}
	if len(tok) < 12 {
		return "***"
	}
	return tok[:6] + "***" + tok[len(tok)-6:]
}

// applyGithubEndpoint replaces the GitHub API host (and the github.com
// download host) with a per-SDK custom endpoint when one is configured. The
// GitHub releases API lives at api.github.com (NOT github.com), so the
// previous useEndpoint implementations -- which only replaced github.com --
// never mirrored the version-list request and the user's mirror was silently
// ignored for version listing. Replacing both hosts fixes that.
func applyGithubEndpoint(sm *config.SettingsManager, sdkType SdkType, defaultURL string) string {
	if sm == nil {
		return defaultURL
	}
	custom := sm.Get().Endpoints[string(sdkType)]
	if custom == "" {
		return defaultURL
	}
	out := strings.Replace(defaultURL, "https://api.github.com", custom, 1)
	out = strings.Replace(out, "https://github.com", custom, 1)
	return out
}

// fetchGithubReleasesPage fetches one page of GitHub releases from url (which
// the caller has already resolved through its useEndpoint mirror if desired)
// and decodes the JSON into target (a pointer to a slice of release structs
// whose fields match the GitHub releases API shape: tag_name, draft,
// prerelease, published_at, assets). It centralizes two cross-cutting concerns
// that every GitHub-API fetcher (python/ruby/perl/php/rust/flutter) needs:
//
//  1. GitHub token auth: when a PAT is configured it is sent as
//     Authorization: Bearer, raising the rate limit from 60 to 5000/hour and
//     avoiding the 403 that unauthenticated requests hit after a few refreshes.
//  2. GithubMirror fallback: if the primary request fails (network error or
//     403 even with a token), the request is retried through the configured
//     GithubMirror reverse proxy (e.g. https://ghfast.top), but only when url
//     still points at api.github.com -- a per-SDK custom endpoint already
//     mirrored the URL, so prepending GithubMirror would double-proxy.
//
// Endpoint mirroring is NOT done here: each fetcher's useEndpoint decides
// whether its per-SDK endpoint applies to the GitHub API (python/ruby/perl/php
// mirror github.com, so they do; rust/flutter mirror rust-lang.org /
// googleapis, so they must not apply their endpoint to the API).
//
// target is left untouched on error.
func fetchGithubReleasesPage(sm *config.SettingsManager, client *http.Client, url string, target any) error {
	token := DecodeGithubToken(sm)

	// Candidate URLs to try in order. The GithubMirror reverse-proxy fallback
	// is only added when url still points at the real api.github.com -- a
	// per-SDK custom endpoint already mirrored the URL, so prepending
	// GithubMirror would double-proxy.
	type candidate struct {
		url   string
		label string
	}
	var cands []candidate
	cands = append(cands, candidate{url, "primary"})
	if sm != nil {
		mirror := sm.Get().GithubMirror
		if mirror != "" && strings.Contains(url, "api.github.com") {
			cands = append(cands, candidate{strings.TrimRight(mirror, "/") + "/" + url, "mirror"})
		}
	}

	var lastErr error
	for _, c := range cands {
		err := doGithubRequest(client, c.url, token, target)
		if err == nil {
			return nil
		}
		lastErr = fmt.Errorf("%s: %w", c.label, err)
		// A 403 with no token is not retried on the same host -- but the next
		// candidate (mirror) may succeed. A network error likewise falls
		// through to the mirror. Both are covered by simply continuing.
	}
	return lastErr
}

// transientGithubStatus lists HTTP status codes worth retrying: gateway
// timeouts (502/503/504), server error (500), request timeout (408), and
// secondary rate limit (429). GitHub's API gateway intermittently returns 504
// on large release pages (notably python-build-standalone, whose every release
// carries 100+ assets); a short retry with backoff usually succeeds.
var transientGithubStatus = map[int]bool{408: true, 429: true, 500: true, 502: true, 503: true, 504: true}

// doGithubRequest fetches url with token auth, retrying transient gateway
// failures (502/503/504/500/408/429) up to 3 times with exponential backoff
// (1s, 2s). Permanent failures (403 rate-limit, 404, decode errors, network
// errors) return immediately without retry -- a retry would just waste time on
// a condition that will not clear in seconds.
func doGithubRequest(client *http.Client, url, token string, target any) error {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		status, err := doGithubRequestOnce(client, url, token, target)
		if err == nil {
			return nil
		}
		lastErr = err
		// status == 0 means the request never got a response (build error,
		// network error, or decode error on a 200) -- not a transient gateway
		// status, so do not retry. Only retry on the transient status set.
		if status == 0 || !transientGithubStatus[status] {
			return err
		}
		if attempt < maxAttempts {
			time.Sleep(time.Duration(1<<(attempt-1)) * time.Second) // 1s, 2s
		}
	}
	return fmt.Errorf("%w (retried %d times)", lastErr, maxAttempts)
}

// doGithubRequestOnce performs a single GET against url, applies the
// Authorization header when token is non-empty, validates the status code, and
// decodes the body into target. Returns the HTTP status code (0 when the
// request never reached a response: build error, network error, or decode
// error) and a non-nil error on failure.
func doGithubRequestOnce(client *http.Client, url, token string, target any) (int, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", githubReleasesUserAgent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetch failed (check proxy/network, or if GitHub API is reachable): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		hint := "GitHub API rate limit hit (403); unauthenticated requests are capped at 60/hour per IP"
		if token != "" {
			hint = "GitHub API returned 403 even with token (token may be invalid, expired, or its 5000/h quota exhausted)"
		}
		return resp.StatusCode, fmt.Errorf("%s -- retry later, configure a mirror, or set a valid GitHub token", hint)
	}
	if resp.StatusCode != 200 {
		return resp.StatusCode, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return resp.StatusCode, fmt.Errorf("decode response: %w", err)
	}
	return resp.StatusCode, nil
}
