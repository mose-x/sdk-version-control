package sdk

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPerlWindowsJSONParsing(t *testing.T) {
	jsonData := `[{"version":"5.42.2.1","date":"2026-04-07","edition":{"portable":{"url":"https://github.com/StrawberryPerl/Perl-Dist-Strawberry/releases/download/SP_54221_64bit/strawberry-perl-5.42.2.1-64bit-portable.zip","sha256":"abc","size":304301401}}},{"version":"5.40.4.1","date":"2026-04-07","edition":{"portable":{"url":"https://github.com/StrawberryPerl/Perl-Dist-Strawberry/releases/download/SP_54041_64bit/strawberry-perl-5.40.4.1-64bit-portable.zip","sha256":"def","size":300000000}}}]`

	var releases []strawberryRelease
	if err := json.Unmarshal([]byte(jsonData), &releases); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if len(releases) != 2 {
		t.Fatalf("expected 2 releases, got %d", len(releases))
	}

	if releases[0].Version != "5.42.2.1" {
		t.Errorf("version: expected '5.42.2.1', got '%s'", releases[0].Version)
	}

	if releases[0].Edition.Portable.URL == "" {
		t.Error("portable URL should not be empty")
	}

	if !strings.Contains(releases[0].Edition.Portable.URL, "github.com") {
		t.Errorf("URL should point to GitHub, got: %s", releases[0].Edition.Portable.URL)
	}

	if strings.Contains(releases[0].Edition.Portable.URL, "strawberryperl.com/download") {
		t.Errorf("URL should NOT use old strawberryperl.com/download/ path (404), got: %s", releases[0].Edition.Portable.URL)
	}
}

func TestPerlWindowsFileNameExtraction(t *testing.T) {
	releases := []strawberryRelease{
		{Version: "5.42.2.1", Edition: struct {
			Portable struct {
				URL    string `json:"url"`
				Sha256 string `json:"sha256"`
				Size   int64  `json:"size"`
			} `json:"portable"`
		}{
			Portable: struct {
				URL    string `json:"url"`
				Sha256 string `json:"sha256"`
				Size   int64  `json:"size"`
			}{
				URL: "https://github.com/StrawberryPerl/Perl-Dist-Strawberry/releases/download/SP_54221_64bit/strawberry-perl-5.42.2.1-64bit-portable.zip",
			},
		},
		},
	}

	for _, r := range releases {
		urlParts := strings.Split(r.Edition.Portable.URL, "/")
		fileName := urlParts[len(urlParts)-1]
		if fileName != "strawberry-perl-5.42.2.1-64bit-portable.zip" {
			t.Errorf("fileName: expected 'strawberry-perl-5.42.2.1-64bit-portable.zip', got '%s'", fileName)
		}
	}
}
