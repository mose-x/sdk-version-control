package sdk

import "testing"

func TestNodejsPlatformArch(t *testing.T) {
	tests := []struct {
		goos, goarch, wantSuffix, wantExt string
	}{
		{"windows", "amd64", "win-x64", "zip"},
		{"windows", "arm64", "win-arm64", "zip"},
		{"linux", "amd64", "linux-x64", "tar.xz"},
		{"linux", "arm64", "linux-arm64", "tar.xz"},
		{"darwin", "amd64", "darwin-x64", "tar.gz"},
		{"darwin", "arm64", "darwin-arm64", "tar.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"-"+tt.goarch, func(t *testing.T) {
			suffix, ext := nodejsPlatformArch(tt.goos, tt.goarch)
			if suffix != tt.wantSuffix {
				t.Errorf("suffix: expected %q, got %q", tt.wantSuffix, suffix)
			}
			if ext != tt.wantExt {
				t.Errorf("ext: expected %q, got %q", tt.wantExt, ext)
			}
		})
	}
}
