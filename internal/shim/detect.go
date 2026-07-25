package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// IsShimMode checks whether the binary was invoked as a shim (i.e. its name
// is not the app name but a known SDK command). This is determined by reading
// argv[0] and checking if the name exists in shims.json.
func IsShimMode() bool {
	if len(os.Args) == 0 {
		return false
	}
	name := filepath.Base(os.Args[0])
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(name, ".exe")
	}

	// If the name matches the app binary name, it's not shim mode
	if isAppName(name) {
		return false
	}

	// Check if the name is a known shim command
	svcHome, err := resolveSvcHome()
	if err != nil {
		return false
	}
	cfg, err := loadShimConfig(svcHome)
	if err != nil {
		return false
	}
	_, ok := cfg.Commands[name]
	return ok
}

// isAppName returns true if the given name matches the SVC application binary.
func isAppName(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "svc", "sdk_version_control", "sdk version control":
		return true
	}
	return false
}
