package downloader

import (
	"net/http"
	"net/url"

	"golang.org/x/sys/windows/registry"
)

// applySystemProxy reads the system proxy from the Windows registry and applies it to the transport
func applySystemProxy(transport *http.Transport) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.READ)
	if err != nil {
		return
	}
	defer k.Close()

	proxyEnable, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || proxyEnable == 0 {
		return
	}

	proxyServer, _, err := k.GetStringValue("ProxyServer")
	if err != nil || proxyServer == "" {
		return
	}

	// Windows system proxy may be in the format http=host:port;https=host:port;socks=host:port
	// Prefer the https proxy, then http, and finally socks
	var proxyStr string
	for _, part := range splitProxyParts(proxyServer) {
		scheme, addr := parseProxyPart(part)
		switch scheme {
		case "https":
			proxyStr = "https://" + addr
		case "http":
			if proxyStr == "" {
				proxyStr = "http://" + addr
			}
		case "socks":
			if proxyStr == "" {
				proxyStr = "socks5://" + addr
			}
		}
	}

	// If there is no scheme prefix, default to http
	if proxyStr == "" && proxyServer != "" {
		if !hasScheme(proxyServer) {
			proxyStr = "http://" + proxyServer
		} else {
			proxyStr = proxyServer
		}
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

func splitProxyParts(s string) []string {
	var parts []string
	for _, p := range splitBy(s, ';') {
		p = trimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func parseProxyPart(s string) (scheme, addr string) {
	idx := indexOf(s, '=')
	if idx > 0 {
		return s[:idx], s[idx+1:]
	}
	return "", s
}

func splitBy(s string, sep byte) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

func trimSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}

func indexOf(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
