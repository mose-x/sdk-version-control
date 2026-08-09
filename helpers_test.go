package main

import "testing"

func TestExtractVersionFromString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"rustc version", "rustc 1.75.0 (cf89e91d4 2023-12-18)\n", "1.75.0"},
		{"go version multiline", "go version go1.21.5 darwin/arm64\n", "1.21.5"},
		{"node version", "v20.10.0\n", "20.10.0"},
		{"python version", "Python 3.13.1\n", "3.13.1"},
		{"empty output", "", ""},
		{"no version pattern", "/usr/local/bin", ""},
		{"sysroot path no version", "/usr\n", ""},
		{"two-digit minor", "rustc 1.80.1 (35 compilercentricities)\n", "1.80.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractVersionFromString(tt.input)
			if got != tt.want {
				t.Errorf("extractVersionFromString(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}
