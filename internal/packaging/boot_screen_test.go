package packaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIndexHtmlBootScreen guards the startup flash fix: frontend/index.html
// must carry the inline themed boot/loading screen (#boot + spinner) that
// paints on the WebView's first frame, so the window never shows a blank flash.
func TestIndexHtmlBootScreen(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(findRepoRoot(t), "frontend", "index.html"))
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(data)
	for _, want := range []string{
		`id="boot"`,            // boot container removed by React on mount
		`boot-spin`,            // spinner element + animation class
		`prefers-color-scheme`, // theme-aware background (light/dark)
		`#141414`,              // dark theme background
		`#f5f5f5`,              // light theme background
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html missing boot-screen marker %q", want)
		}
	}
}
