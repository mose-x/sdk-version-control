package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sdk_version_control/internal/downloader"
	"sdk_version_control/internal/logger"
)

func (a *App) getProxyConfig() downloader.ProxyConfig {
	s := a.settings.Get()
	return downloader.ProxyConfig{
		Enabled:  s.Proxy.Enabled,
		Mode:     s.Proxy.Mode,
		URL:      s.Proxy.URL,
		Protocol: s.Proxy.Protocol,
	}
}

func (a *App) CheckProxy(targetURL string) error {
	// H2: Validate URL to prevent SSRF — only http/https, reject loopback/private.
	if err := validateCheckURL(targetURL); err != nil {
		return err
	}
	proxyCfg := a.getProxyConfig()
	client := downloader.BuildClient(proxyCfg)
	client.Timeout = 10 * time.Second
	// Re-validate every redirect hop: the initial URL may pass validation,
	// but a 3xx chain could otherwise land on an internal address unchecked.
	client.CheckRedirect = checkRedirectPolicy

	resp, err := client.Get(targetURL)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return fmt.Errorf("connection failed: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// checkRedirectPolicy is the http.Client.CheckRedirect hook used by
// CheckProxy. Every hop is re-validated so a redirect cannot escape the SSRF
// constraints enforced on the initial URL. The 10-hop cap replicates the
// net/http default policy, which is not applied when a custom CheckRedirect
// is set.
func checkRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return validateCheckURL(req.URL.String())
}

// validateCheckURL ensures the target URL is safe for proxy checking:
// must be http/https, must not target loopback/private/link-local addresses.
func validateCheckURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http/https URLs are allowed, got scheme: %s", u.Scheme)
	}
	host := u.Hostname()
	// Fast path: common loopback/private hosts by string match.
	if host == "localhost" || host == "127.0.0.1" || host == "::1" ||
		strings.HasPrefix(host, "127.") || strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "172.16.") ||
		strings.HasPrefix(host, "172.17.") || strings.HasPrefix(host, "172.18.") ||
		strings.HasPrefix(host, "172.19.") || strings.HasPrefix(host, "172.20.") ||
		strings.HasPrefix(host, "172.21.") || strings.HasPrefix(host, "172.22.") ||
		strings.HasPrefix(host, "172.23.") || strings.HasPrefix(host, "172.24.") ||
		strings.HasPrefix(host, "172.25.") || strings.HasPrefix(host, "172.26.") ||
		strings.HasPrefix(host, "172.27.") || strings.HasPrefix(host, "172.28.") ||
		strings.HasPrefix(host, "172.29.") || strings.HasPrefix(host, "172.30.") ||
		strings.HasPrefix(host, "172.31.") {
		return fmt.Errorf("loopback/private IP addresses are not allowed: %s", host)
	}
	// Thorough check for IP literals: covers ranges the string blacklist
	// misses (0.0.0.0, 169.254.0.0/16, IPv6 fc00::/7 and fe80::/10,
	// IPv4-mapped IPv6 like ::ffff:127.0.0.1). Non-IP hostnames parse to nil
	// and are allowed here.
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Errorf("loopback/private IP addresses are not allowed: %s", host)
		}
	}
	return nil
}

func (a *App) applyGithubMirror(url string) string {
	mirror := a.settings.Get().GithubMirror
	if mirror == "" {
		return url
	}
	mirror = strings.TrimRight(mirror, "/")
	// Guard against a mirror that already points at github.com (e.g. a user
	// misconfigured it as https://github.com/proxy): without the prefix check
	// we'd prepend mirror to an already-mirrored URL, producing garbage.
	if isGithubHost(url) && !strings.HasPrefix(url, mirror) {
		return mirror + "/" + url
	}
	return url
}

// isGithubHost reports whether url targets github.com or a github.com subdomain.
// Uses net/url parsing so a non-github URL that merely contains "github.com"
// as a substring (e.g. https://evil.example.com/path/github.com/foo or
// https://github.com.evil.example.com/) is NOT treated as a github URL.
func isGithubHost(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Host
	// Strip any port suffix for the host comparison.
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(host)
	return host == "github.com" || strings.HasSuffix(host, ".github.com")
}

// buildProxyTransport builds the transport for non-download HTTP calls
// (the update checker). It delegates to downloader.ApplyProxyConfig, the same
// single implementation used by downloader.BuildClient, so the two cannot
// drift. (Previously this had its own logic: bare "host:port" custom proxies
// failed url.Parse and were silently dropped, and system mode used
// http.ProxyFromEnvironment instead of the platform applySystemProxy.)
func (a *App) buildProxyTransport() *http.Transport {
	transport := &http.Transport{}
	if err := downloader.ApplyProxyConfig(transport, a.getProxyConfig()); err != nil {
		logger.Warn("Invalid proxy configuration %q: %v — proxy will not be applied", a.settings.Get().Proxy.URL, err)
	}
	return transport
}
