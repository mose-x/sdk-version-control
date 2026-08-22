package helpers

import (
	"fmt"
	"strings"
)

// windowsReservedNames lists Windows reserved device names that cannot be
// used as file or directory names. Applied on all platforms because a path
// segment that is a Windows reserved name is never a legitimate SDK name and
// would break Windows portability (CON.txt is still the CON device).
var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "NUL": true, "AUX": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// windowsIllegalChars are characters not allowed in Windows filenames.
// Rejected on all platforms for the same cross-platform portability reason.
const windowsIllegalChars = `<>:"|*?`

// ValidatePathSegment rejects path segments that are empty, traversal
// (./..), contain separators/NUL, contain Windows-illegal characters, or are
// Windows reserved device names. It guards every user-supplied segment that
// is joined into SDK paths (sdk types, versions, filenames).
func ValidatePathSegment(segment string) error {
	if segment == "" {
		return fmt.Errorf("path segment cannot be empty")
	}
	// L14: reject ".." only as an exact segment (not a substring), so valid
	// names like "1..2" are allowed. Path separators and NUL are still rejected.
	if segment == "." || segment == ".." || strings.ContainsAny(segment, "/\\") || strings.ContainsRune(segment, 0) {
		return fmt.Errorf("invalid path segment: %s", segment)
	}
	if strings.ContainsAny(segment, windowsIllegalChars) {
		return fmt.Errorf("invalid path segment (contains illegal character): %s", segment)
	}
	if windowsReservedNames[strings.ToUpper(segment)] {
		return fmt.Errorf("invalid path segment (Windows reserved name): %s", segment)
	}
	return nil
}
