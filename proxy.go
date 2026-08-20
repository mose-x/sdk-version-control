package main

import (
	"fmt"
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

	resp, err := client.Get(targetURL)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// validateCheckURL ensures the target URL is safe for proxy checking:
// must be http/https, must not resolve to loopback or private IP ranges.
func validateCheckURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http/https URLs are allowed, got scheme: %s", u.Scheme)
	}
	host := u.Hostname()
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
	if strings.Contains(url, "github.com") && !strings.HasPrefix(url, mirror) {
		return mirror + "/" + url
	}
	return url
}

func validatePathSegment(segment string) error {
	if segment == "" {
		return fmt.Errorf("path segment cannot be empty")
	}
	if strings.ContainsAny(segment, "/\\") || strings.Contains(segment, "..") || strings.ContainsRune(segment, 0) {
		return fmt.Errorf("invalid path segment: %s", segment)
	}
	return nil
}

func (a *App) buildProxyTransport() *http.Transport {
	transport := &http.Transport{}
	s := a.settings.Get()
	if s.Proxy.Enabled {
		switch s.Proxy.Mode {
		case "system":
			transport.Proxy = http.ProxyFromEnvironment
		case "custom":
			if s.Proxy.URL != "" {
				proxyURL, err := url.Parse(s.Proxy.URL)
				if err == nil {
					transport.Proxy = http.ProxyURL(proxyURL)
				} else {
					logger.Warn("Invalid proxy URL %q: %v — proxy will not be applied", s.Proxy.URL, err)
				}
			}
		}
	}
	return transport
}
