package downloader

import (
	"net/http"
	"net/url"

	"golang.org/x/sys/windows/registry"
)

// applySystemProxy 从 Windows 注册表读取系统代理并应用到 transport
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

	// Windows 系统代理可能是 http=host:port;https=host:port;socks=host:port 格式
	// 优先使用 https 代理，其次 http，最后 socks
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

	// 如果没有 scheme 前缀，默认为 http
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
