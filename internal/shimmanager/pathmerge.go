package shimmanager

import (
	"path/filepath"
	"strings"
)

// mergePathEntry returns the new user PATH after adding shims to current.
// If shims is already present (case-insensitive, separator-agnostic path
// comparison), current is returned unchanged so the caller can skip the
// registry write. shims is prepended. An empty/blank current yields shims
// alone — no trailing separator (item 2d).
func mergePathEntry(current, shims string) string {
	for _, p := range strings.Split(current, ";") {
		if pathEntryEquals(p, shims) {
			return current
		}
	}
	if strings.TrimSpace(current) == "" {
		return shims
	}
	return shims + ";" + current
}

// filterPathEntry returns current with every occurrence of remove dropped
// (case-insensitive, separator-agnostic). Empty segments are dropped too,
// so removing an entry never leaves a stray double separator behind.
func filterPathEntry(current, remove string) string {
	var filtered []string
	for _, p := range strings.Split(current, ";") {
		p = strings.TrimSpace(p)
		if p == "" || pathEntryEquals(p, remove) {
			continue
		}
		filtered = append(filtered, p)
	}
	return strings.Join(filtered, ";")
}

// pathEntryEquals compares two PATH entries as directories: trimmed and
// cleaned, case-insensitively (Windows paths are case-insensitive).
func pathEntryEquals(a, b string) bool {
	a = strings.TrimSpace(a)
	if a == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
