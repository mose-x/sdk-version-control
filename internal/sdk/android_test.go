package sdk

import (
	"encoding/xml"
	"testing"
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
