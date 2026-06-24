//go:build windows

package sdk

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// getPlatformPath reads the system+user PATH from the Windows registry
func getPlatformPath() string {
	var allPaths []string

	// Prefer the registry (reflects the latest version switches); user-level takes priority over system-level
	for _, key := range []struct {
		root registry.Key
		path string
	}{
		{registry.CURRENT_USER, `Environment`},
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
	} {
		k, err := registry.OpenKey(key.root, key.path, registry.READ)
		if err != nil {
			continue
		}
		val, valType, err := k.GetStringValue("Path")
		k.Close()
		if err != nil || val == "" {
			continue
		}
		if valType == registry.EXPAND_SZ {
			expanded, err := registry.ExpandString(val)
			if err == nil {
				val = expanded
			}
		}
		parts := splitPathString(val)
		allPaths = append(allPaths, parts...)
	}

	// Process PATH as a fallback
	pathEnv := os.Getenv("PATH")
	allPaths = append(allPaths, strings.Split(pathEnv, ";")...)

	// Deduplicate
	seen := make(map[string]bool)
	var result []string
	for _, p := range allPaths {
		p = strings.TrimSpace(p)
		// Strip Unicode control characters
		p = cleanUnicode(p)
		if p == "" {
			continue
		}
		lp := strings.ToLower(strings.TrimRight(p, "\\/"))
		if !seen[lp] {
			seen[lp] = true
			result = append(result, p)
		}
	}
	return strings.Join(result, ";")
}

// splitPathString splits a Windows PATH string
// Handles both semicolon-separated and space-separated cases
func splitPathString(s string) []string {
	// First split by semicolons
	parts := strings.Split(s, ";")
	var result []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// If it contains multiple drive letters, it may be space-separated; split further
		if strings.Count(part, ":\\") > 1 {
			sub := splitByDriveLetter(part)
			result = append(result, sub...)
		} else {
			result = append(result, part)
		}
	}
	return result
}

// splitByDriveLetter splits a space-separated path by drive letter
// e.g. "C:\a b C:\c d" -> ["C:\a b", "C:\c d"]
func splitByDriveLetter(s string) []string {
	// Find all drive letter positions
	var positions []int
	for i := 0; i < len(s)-2; i++ {
		if s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z' {
			if s[i+1] == ':' && s[i+2] == '\\' {
				// Ensure the previous character is a space or the start of the string
				if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
					positions = append(positions, i)
				}
			}
		}
	}
	if len(positions) <= 1 {
		return []string{s}
	}
	var result []string
	for i, pos := range positions {
		var end int
		if i+1 < len(positions) {
			end = positions[i+1]
		} else {
			end = len(s)
		}
		entry := strings.TrimSpace(s[pos:end])
		if entry != "" {
			result = append(result, entry)
		}
	}
	return result
}

// cleanUnicode removes Unicode control characters
func cleanUnicode(s string) string {
	return strings.Map(func(r rune) rune {
		// Remove Unicode bidirectional control characters and BOM
		if r >= 0x202A && r <= 0x202E { return -1 }
		if r >= 0x2066 && r <= 0x2069 { return -1 }
		if r == 0xFEFF { return -1 }
		return r
	}, s)
}
