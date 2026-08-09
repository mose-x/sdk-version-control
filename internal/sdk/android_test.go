package sdk

import (
	"context"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAndroidXMLHostOSParsing(t *testing.T) {
	xmlData := []byte(`<?xml version="1.0"?>
<sdk-repository>
  <remotePackage path="cmdline-tools;22.0">
    <revision>
      <major>22</major>
      <minor>0</minor>
      <micro>0</micro>
    </revision>
    <archives>
      <archive>
        <complete>
          <url>commandlinetools-linux-15859902_latest.zip</url>
          <size>155000000</size>
        </complete>
        <host-os>linux</host-os>
      </archive>
      <archive>
        <complete>
          <url>commandlinetools-win-15859902_latest.zip</url>
          <size>155000000</size>
        </complete>
        <host-os>windows</host-os>
      </archive>
    </archives>
  </remotePackage>
</sdk-repository>`)

	var repo androidRepository
	if err := xml.Unmarshal(xmlData, &repo); err != nil {
		t.Fatalf("failed to parse XML: %v", err)
	}

	if len(repo.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(repo.Packages))
	}

	pkg := repo.Packages[0]
	if pkg.Path != "cmdline-tools;22.0" {
		t.Errorf("package path: expected 'cmdline-tools;22.0', got '%s'", pkg.Path)
	}
	if pkg.Revision.Major != 22 {
		t.Errorf("major: expected 22, got %d", pkg.Revision.Major)
	}

	archives := pkg.Archives.Archive
	if len(archives) != 2 {
		t.Fatalf("expected 2 archives, got %d", len(archives))
	}

	if archives[0].OS != "linux" {
		t.Errorf("archive[0] OS: expected 'linux', got '%s' (host-os is a child element, not an attribute — the struct tag must not use ,attr)", archives[0].OS)
	}
	if archives[1].OS != "windows" {
		t.Errorf("archive[1] OS: expected 'windows', got '%s'", archives[1].OS)
	}

	if archives[0].URL != "commandlinetools-linux-15859902_latest.zip" {
		t.Errorf("archive[0] URL: expected linux zip, got '%s'", archives[0].URL)
	}
	if archives[1].URL != "commandlinetools-win-15859902_latest.zip" {
		t.Errorf("archive[1] URL: expected windows zip, got '%s'", archives[1].URL)
	}
}

func TestAndroidFetcher_GetDownloadURL_noFallback(t *testing.T) {
	// Clear any cached Android versions so GetDownloadURL performs a real
	// fetch instead of short-circuiting on the in-memory cache.
	globalVersionCache.mu.Lock()
	delete(globalVersionCache.entries, Android)
	globalVersionCache.mu.Unlock()

	// An HTTP client whose Transport always fails to dial, so
	// FetchRemoteVersions returns an error without touching the real network.
	// With f.sm == nil, useEndpoint returns the default dl.google.com URL.
	f := &AndroidFetcher{
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return nil, fmt.Errorf("simulated unreachable endpoint")
				},
			},
		},
	}

	_, _, err := f.GetDownloadURL("14.0")
	if err == nil {
		t.Fatal("expected GetDownloadURL to return an error when fetch fails (no silent fallback), got nil")
	}
	if !strings.Contains(err.Error(), "failed to fetch Android cmdline-tools versions") {
		t.Errorf("expected error to mention the fetch failure, got: %v", err)
	}
}
