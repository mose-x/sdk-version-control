package downloader

import (
	"net/http"
	"net/url"
	"os/exec"
	"strings"
)

// applySystemProxy reads the macOS system proxy via scutil --proxy and applies it to the transport.
//
// scutil --proxy emits host and port on SEPARATE lines, e.g.
//
//	HTTPSEnable : 1
//	HTTPSProxy  : 127.0.0.1
//	HTTPSPort   : 7890
//
// so we must collect host and port independently, then join them. A previous
// version only read the *Proxy host lines and dropped the *Port lines, which
// produced a port-less proxy URL like "https://127.0.0.1"; Go's net/http then
// fell back to the scheme default port (443 for https) and the dial failed
// with "connection refused" because no proxy listens on 443.
//
// The HTTPS proxy entry from scutil is the proxy used to reach https://
// destinations -- the proxy itself still speaks plain HTTP CONNECT, so the
// URL scheme must be "http://" regardless of the HTTP vs HTTPS entry. Using
// "https://" here would tell net/http to TLS-wrap the proxy connection,
// which local proxies (Clash/V2Ray/...) do not expect.
func applySystemProxy(transport *http.Transport) {
	out, err := exec.Command("scutil", "--proxy").Output()
	if err != nil {
		return
	}

	var proxyStr string
	var httpsProxy, httpsPort, httpProxy, httpPort, socksProxy, socksPort string
	var httpsEnable, httpEnable, socksEnable bool

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// Order matters: "HTTPSProxy" contains the substring "HTTPProxy",
		// so the HTTPS-specific cases must be checked BEFORE the HTTP ones
		// (otherwise an HTTPSProxy line would match the HTTPProxy case and
		// populate the wrong variable).
		switch {
		case strings.Contains(line, "HTTPSEnable") && strings.Contains(line, "1"):
			httpsEnable = true
		case strings.Contains(line, "HTTPSProxy"):
			httpsProxy = parseScutilValue(line)
		case strings.Contains(line, "HTTPSPort"):
			httpsPort = parseScutilValue(line)
		case strings.Contains(line, "HTTPEnable") && strings.Contains(line, "1"):
			httpEnable = true
		case strings.Contains(line, "HTTPProxy"):
			httpProxy = parseScutilValue(line)
		case strings.Contains(line, "HTTPPort"):
			httpPort = parseScutilValue(line)
		case strings.Contains(line, "SOCKSEnable") && strings.Contains(line, "1"):
			socksEnable = true
		case strings.Contains(line, "SOCKSProxy"):
			socksProxy = parseScutilValue(line)
		case strings.Contains(line, "SOCKSPort"):
			socksPort = parseScutilValue(line)
		}
	}

	// Prefer HTTPS entry (covers both http and https destinations), then HTTP,
	// then SOCKS. Always use the explicit port when present; fall back to the
	// protocol default only if scutil omitted it (rare in practice).
	if httpsEnable && httpsProxy != "" {
		proxyStr = "http://" + joinHostPort(httpsProxy, httpsPort)
	} else if httpEnable && httpProxy != "" {
		proxyStr = "http://" + joinHostPort(httpProxy, httpPort)
	} else if socksEnable && socksProxy != "" {
		proxyStr = "socks5://" + joinHostPort(socksProxy, socksPort)
	}

	if proxyStr == "" {
		return
	}

	proxyURL, err := url.Parse(proxyStr)
	if err != nil {
		return
	}

	applyProxy(transport, proxyURL)
}

// parseScutilValue extracts the value from a "key : value" scutil line.
// Returns the trimmed value, or empty string if the line has no value.
func parseScutilValue(line string) string {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// joinHostPort joins a host and port with ":port" when port is non-empty,
// returning just the host otherwise. Used to rebuild the proxy address that
// scutil split across two lines.
func joinHostPort(host, port string) string {
	if port == "" {
		return host
	}
	return host + ":" + port
}
