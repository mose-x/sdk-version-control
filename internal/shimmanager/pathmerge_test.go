package shimmanager

import "testing"

// TestMergePathEntry pins item 2: prepending the shims dir to the user PATH
// must not duplicate entries, must be case-insensitive (Windows), and must
// not append a trailing separator to an empty PATH (2d).
func TestMergePathEntry(t *testing.T) {
	shims := `C:\Users\u\.svc\shims`
	cases := []struct {
		name    string
		current string
		want    string
	}{
		{"empty path gets shims only (no trailing ;)", "", shims},
		{"blank path gets shims only", "   ", shims},
		{"prepend to existing", `C:\bin;D:\tools`, shims + `;C:\bin;D:\tools`},
		{"already present exact", shims + `;C:\bin`, shims + `;C:\bin`},
		{"already present different case", `c:\users\U\.SVC\Shims` + `;C:\bin`, `c:\users\U\.SVC\Shims` + `;C:\bin`},
		{"already present later in list", `C:\bin;` + shims, `C:\bin;` + shims},
		{"similar prefix is not the same entry", `C:\Users\u\.svc\shims-extra`, shims + `;C:\Users\u\.svc\shims-extra`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergePathEntry(tc.current, shims); got != tc.want {
				t.Errorf("mergePathEntry(%q) = %q; want %q", tc.current, got, tc.want)
			}
		})
	}
}

// TestFilterPathEntry pins item 2's removal path: only the exact shims entry
// (case-insensitive) is dropped; look-alike prefixes survive; empty segments
// are cleaned up so no double separator remains.
func TestFilterPathEntry(t *testing.T) {
	shims := `C:\Users\u\.svc\shims`
	cases := []struct {
		name    string
		current string
		want    string
	}{
		{"remove first", shims + `;C:\bin`, `C:\bin`},
		{"remove middle", `C:\a;` + shims + `;C:\b`, `C:\a;C:\b`},
		{"case-insensitive", `C:\a;` + `c:\USERS\u\.svc\SHIMS`, `C:\a`},
		{"not present", `C:\a;C:\b`, `C:\a;C:\b`},
		{"look-alike prefix kept", `C:\a;` + shims + `-extra`, `C:\a;` + shims + `-extra`},
		{"empty segments dropped", shims + `;;C:\a;`, `C:\a`},
		{"only entry", shims, ``},
		{"empty input", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := filterPathEntry(tc.current, shims); got != tc.want {
				t.Errorf("filterPathEntry(%q) = %q; want %q", tc.current, got, tc.want)
			}
		})
	}
}

// TestMergeFilterRoundTrip: merge then filter restores the original PATH.
func TestMergeFilterRoundTrip(t *testing.T) {
	shims := `/home/u/.svc/shims`
	merged := mergePathEntry(`C:\bin`, shims)
	back := filterPathEntry(merged, shims)
	if back != `C:\bin` {
		t.Errorf("merge+filter round trip = %q; want %q", back, `C:\bin`)
	}
}
