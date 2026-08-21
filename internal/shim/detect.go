package shim

import "strings"

// IsShimMode checks whether the binary was invoked as a shim (i.e. its name
// is not the app name but a known SDK command). This works for both invocation
// modes: hardlink (argv[0] is the command name) and wrapper (argv[0] is
// "svc-shim.exe", argv[1] is the command name).
func IsShimMode() bool {
	inv := parseInvocation()
	if inv.Command == "" || isAppName(inv.Command) {
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
	// O12: on Windows command names are case-insensitive (cmd.exe / batch
	// are case-insensitive; a hardlink to NODE.exe is the same as node.exe).
	// lookupCommand does a case-insensitive scan on Windows, exact on Unix.
	_, ok := lookupCommand(cfg.Commands, inv.Command)
	return ok
}

// isAppName returns true if the given name matches the SVC application binary.
func isAppName(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "svc", "svc-shim", "sdk_version_control", "sdk version control":
		return true
	}
	return false
}
