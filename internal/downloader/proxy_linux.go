package downloader

import (
	"net/http"
	"net/url"
	"os"
)

// applySystemProxy Linux 下从环境变量读取系统代理
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
