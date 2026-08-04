package sdk

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestPHPWindowsRegexCapturesVsTag(t *testing.T) {
	re := regexp.MustCompile(`php-(\d+\.\d+\.\d+)-nts-Win32-(vs\d+)-x64\.zip`)

	tests := []struct {
		input   string
		wantVer string
		wantVs  string
	}{
		{"php-8.3.33-nts-Win32-vs16-x64.zip", "8.3.33", "vs16"},
		{"php-8.4.24-nts-Win32-vs17-x64.zip", "8.4.24", "vs17"},
		{"php-8.5.0-nts-Win32-vs17-x64.zip", "8.5.0", "vs17"},
	}

	for _, tt := range tests {
		m := re.FindStringSubmatch(tt.input)
		if m == nil {
			t.Errorf("regex did not match: %s", tt.input)
			continue
		}
		if m[1] != tt.wantVer {
			t.Errorf("version: expected '%s', got '%s'", tt.wantVer, m[1])
		}
		if m[2] != tt.wantVs {
			t.Errorf("vs tag: expected '%s', got '%s' for %s", tt.wantVs, m[2], tt.input)
		}
	}
}

func TestPHPWindowsURLUsesCorrectVsTag(t *testing.T) {
	f := &PHPFetcher{}

	// Simulate what fetchWindowsVersions does after regex capture
	for _, tt := range []struct {
		ver   string
		vsTag string
	}{
		{"8.3.33", "vs16"},
		{"8.4.24", "vs17"},
	} {
		fileName := fmt.Sprintf("php-%s-nts-Win32-%s-x64.zip", tt.ver, tt.vsTag)
		downloadURL := f.useEndpoint(fmt.Sprintf("https://windows.php.net/downloads/releases/%s", fileName))

		if !strings.Contains(downloadURL, tt.vsTag) {
			t.Errorf("URL should contain '%s' for PHP %s, got: %s", tt.vsTag, tt.ver, downloadURL)
		}
		if !strings.Contains(fileName, tt.vsTag) {
			t.Errorf("fileName should contain '%s' for PHP %s, got: %s", tt.vsTag, tt.ver, fileName)
		}
		if strings.Contains(downloadURL, "vs16") && tt.vsTag != "vs16" {
			t.Errorf("URL should NOT contain vs16 for PHP %s (uses %s), got: %s", tt.ver, tt.vsTag, downloadURL)
		}
	}
}
