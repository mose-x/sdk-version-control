//go:build !windows

package pathmgr

import (
	"fmt"
	"sort"
	"strings"
)

// shellQuote wraps s in single quotes for POSIX shells; an embedded single
// quote is escaped the standard way (end quoting, backslash-quote, reopen).
// Unlike fmt %q (double quotes), a single-quoted value is fully inert when
// sourced: $, `, and \ are not re-expanded.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// unquoteShellValue reverses shellQuote for values read back from env.sh.
// Legacy double-quoted values are also handled.
func unquoteShellValue(v string) string {
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], `'\''`, "'")
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return v
}

// fishEscapeWord escapes s for use as an UNQUOTED fish word. Fish splits on
// unescaped whitespace and interprets ;|&<>()$ etc., and double quotes still
// expand $ inside, so metacharacters are backslash-escaped instead of
// wrapping the value in quotes.
func fishEscapeWord(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '/' || r == '.' || r == '_' || r == '-' || r == ':' || r == '+' || r == '@':
			b.WriteRune(r)
		default:
			b.WriteByte('\\')
			b.WriteRune(r)
		}
	}
	return b.String()
}

// unescapeFishWord reverses fishEscapeWord for values read back from
// env.sh.fish. Legacy double-quoted values are also handled.
func unescapeFishWord(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	var b strings.Builder
	esc := false
	for _, r := range v {
		if esc {
			b.WriteRune(r)
			esc = false
			continue
		}
		if r == '\\' {
			esc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sortedKeys returns the map keys in sorted order so generated files are
// deterministic across runs (map iteration order is randomized).
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func formatExportLine(k, v string) string {
	return fmt.Sprintf("export %s=%s", k, shellQuote(v))
}

func formatFishLine(k, v string) string {
	if k == "PATH" {
		return fmt.Sprintf("set -gx PATH %s $PATH", fishEscapeWord(v))
	}
	return fmt.Sprintf("set -gx %s %s", k, fishEscapeWord(v))
}
