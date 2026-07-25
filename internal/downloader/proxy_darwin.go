package downloader

import (
	"net/http"
	"net/url"
	"os/exec"
	"strings"
)

// applySystemProxy reads the macOS system proxy via scutil --proxy and applies it to the transport
func applySystemProxy(transport *http.Transport) {
	out, err := exec.Command("scutil", "--proxy").Output()
	if err != nil {
		return
	}

	var proxyStr string
	var httpsProxy, httpProxy, socksProxy string
	var httpsEnable, httpEnable, socksEnable bool

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.Contains(line, "HTTPEnable") && strings.Contains(line, "1"):
			httpEnable = true
		case strings.Contains(line, "HTTPProxy"):
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				httpProxy = strings.TrimSpace(parts[1])
			}
		case strings.Contains(line, "HTTPSEnable") && strings.Contains(line, "1"):
			httpsEnable = true
		case strings.Contains(line, "HTTPSProxy"):
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				httpsProxy = strings.TrimSpace(parts[1])
			}
		case strings.Contains(line, "SOCKSEnable") && strings.Contains(line, "1"):
			socksEnable = true
		case strings.Contains(line, "SOCKSProxy"):
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				socksProxy = strings.TrimSpace(parts[1])
			}
		}
	}

	if httpsEnable && httpsProxy != "" {
		proxyStr = "https://" + httpsProxy
	} else if httpEnable && httpProxy != "" {
		proxyStr = "http://" + httpProxy
	} else if socksEnable && socksProxy != "" {
		proxyStr = "socks5://" + socksProxy
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
