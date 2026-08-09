package sdk

import "testing"

func TestChinaMirrors(t *testing.T) {
	m := ChinaMirrors()
	if len(m) != 3 {
		t.Fatalf("expected 3 mirror entries, got %d", len(m))
	}
	tests := []struct {
		key, want string
	}{
		{"storage.googleapis.com", "storage.flutter-io.cn"},
		{"dl.google.com", "mirrors.tuna.tsinghua.edu.cn/android/repository"},
		{"go.dev", "golang.google.cn"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, ok := m[tt.key]
			if !ok {
				t.Errorf("expected mirror entry for %q, not found", tt.key)
				return
			}
			if got != tt.want {
				t.Errorf("ChinaMirrors[%q]: expected %q, got %q", tt.key, tt.want, got)
			}
		})
	}
}
