package sdk

import "testing"

func TestJdkArch(t *testing.T) {
	tests := []struct {
		goarch, want string
	}{
		{"amd64", "x64"},
		{"arm64", "aarch64"},
	}
	for _, tt := range tests {
		t.Run(tt.goarch, func(t *testing.T) {
			got := jdkArch(tt.goarch)
			if got != tt.want {
				t.Errorf("jdkArch(%q): expected %q, got %q", tt.goarch, tt.want, got)
			}
		})
	}
}
