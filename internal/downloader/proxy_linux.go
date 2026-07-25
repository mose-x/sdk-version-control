package downloader

import (
	"net/http"
	"net/url"
	"os"
)

// applySystemProxy reads the system proxy from environment variables on Linux
func applySystemProxy(transport *http.Transport) {
	for _, envKey := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		val := os.Getenv(envKey)
		if val == "" {
			continue
		}
		if !hasScheme(val) {
			val = "http://" + val
		}
		proxyURL, err := url.Parse(val)
		if err != nil {
			continue
		}
		applyProxy(transport, proxyURL)
		return
	}
}
