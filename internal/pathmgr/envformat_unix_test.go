//go:build !windows

package pathmgr

import "testing"

func TestShellQuoteRoundtrip(t *testing.T) {
	cases := []string{
		"plain",
		"/path/with space",
		"has'single",
		`has"double`,
		"$(cmd) and `backtick` and $VAR",
		"",
	}
	for _, v := range cases {
		q := shellQuote(v)
		if got := unquoteShellValue(q); got != v {
			t.Errorf("roundtrip %q -> %q -> %q", v, q, got)
		}
	}
	// single-quoted output must not contain a raw expandable $ outside quotes
	if q := shellQuote("$HOME"); q != `'$HOME'` {
		t.Errorf("shellQuote($HOME) = %q", q)
	}
	// legacy double-quoted values still parse
	if got := unquoteShellValue(`"legacy"`); got != "legacy" {
		t.Errorf("legacy unquote = %q", got)
	}
}

func TestFishEscapeRoundtrip(t *testing.T) {
	cases := []string{
		"/plain/path",
		"/path/with space",
		"dollar$VAR",
		"semi;colon",
	}
	for _, v := range cases {
		e := fishEscapeWord(v)
		if got := unescapeFishWord(e); got != v {
			t.Errorf("roundtrip %q -> %q -> %q", v, e, got)
		}
	}
	if got := unescapeFishWord(`"legacy"`); got != "legacy" {
		t.Errorf("legacy fish unquote = %q", got)
	}
}

func TestFormatLinesDeterministic(t *testing.T) {
	m := map[string]string{"PATH": "/a", "JAVA_HOME": "/j dk"}
	var first []string
	for i := 0; i < 5; i++ {
		var lines []string
		for _, k := range sortedKeys(m) {
			lines = append(lines, formatExportLine(k, m[k]))
		}
		if first == nil {
			first = lines
			continue
		}
		for j := range lines {
			if lines[j] != first[j] {
				t.Fatalf("non-deterministic output on run %d", i)
			}
		}
	}
	// sorted order: JAVA_HOME before PATH
	if first[0] != `export JAVA_HOME='/j dk'` {
		t.Errorf("line0 = %q", first[0])
	}
	if got := formatFishLine("PATH", "/shims dir"); got != `set -gx PATH /shims\ dir $PATH` {
		t.Errorf("fish PATH line = %q", got)
	}
}
