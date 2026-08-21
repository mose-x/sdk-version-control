package sdk

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		// Build metadata after "+" must be discarded, not parsed: Atoi used to
		// swallow the error and zero the segment, comparing these as equal.
		{"17.0.20+8", "17.0.9+9", 1},
		{"17.0.9+9", "17.0.20+8", -1},
		{"17.0.20+8", "17.0.20+9", 0}, // metadata never breaks ties
		{"1.2.3+4", "1.2.3", 0},
		// Plain ordering.
		{"1.2.3", "1.2.4", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.2.3", "1.2.3", 0},
		// Leading "v" is ignored.
		{"v1.2.3", "1.2.3", 0},
		{"v2.0.0", "v1.99.99", 1},
		// Unequal segment counts: missing segments count as 0.
		{"1.2", "1.2.0", 0},
		{"1.2", "1.2.1", -1},
		{"10.0.0", "9.9.9", 1}, // numeric, not lexicographic
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			if got := CompareVersions(tt.a, tt.b); got != tt.want {
				t.Errorf("CompareVersions(%q, %q) = %d; want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

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
