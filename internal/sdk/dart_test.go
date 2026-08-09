package sdk

import "testing"

func TestDartArch(t *testing.T) {
	// Dart provides arm64 builds for all platforms, so the result depends
	// only on goarch (not goos). Exercise all 6 combos to confirm.
	tests := []struct {
		goos, goarch, want string
	}{
		{"windows", "amd64", "x64"},
		{"windows", "arm64", "arm64"},
		{"linux", "amd64", "x64"},
		{"linux", "arm64", "arm64"},
		{"darwin", "amd64", "x64"},
		{"darwin", "arm64", "arm64"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"-"+tt.goarch, func(t *testing.T) {
			got := dartArch(tt.goos, tt.goarch)
			if got != tt.want {
				t.Errorf("dartArch(%q, %q): expected %q, got %q", tt.goos, tt.goarch, tt.want, got)
			}
		})
	}
}
