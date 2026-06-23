package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sdk_version_control/internal/downloader"
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
	proxyCfg := a.getProxyConfig()
	client := downloader.BuildClient(proxyCfg)
	client.Timeout = 10 * time.Second

	resp, err := client.Get(targetURL)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func (a *App) applyGithubMirror(url string) string {
	mirror := a.settings.Get().GithubMirror
	if mirror == "" {
		return url
	}
	mirror = strings.TrimRight(mirror, "/")
	if strings.Contains(url, "github.com") {
		return mirror + "/" + url
	}
	return url
}

func validatePathSegment(segment string) error {
	if segment == "" {
		return fmt.Errorf("路径段不能为空")
	}
	if strings.ContainsAny(segment, "/\\") || strings.Contains(segment, "..") || strings.ContainsRune(segment, 0) {
		return fmt.Errorf("非法路径段: %s", segment)
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
				}
			}
		}
	}
	return transport
}
