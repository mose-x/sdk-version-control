package main

import "testing"

func TestParsePipVersion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"standard", "pip 24.0 from /opt/python/lib/python3.12/site-packages/pip (python 3.12)\n", "24.0"},
		{"patch version", "pip 23.2.1 from /usr/lib/python3.11/site-packages/pip (python 3.11)", "23.2.1"},
		{"trailing newline only", "pip 22.0.2\n", "22.0.2"},
		{"no leading pip", "Package Installer Program v9", "Package Installer Program v9"},
		{"empty output", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePipVersion(tt.input)
			if got != tt.want {
				t.Errorf("parsePipVersion(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}
