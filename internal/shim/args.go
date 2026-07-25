package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Invocation holds the parsed command name and the args to pass through to the
// real binary.
type Invocation struct {
	Command string
	Args    []string
}

// parseInvocation determines the command name and pass-through args based on
// how the shim was invoked:
//   - Hardlink mode (Unix; Windows .exe): argv[0] is the command name,
//     args are argv[1:].
//   - Wrapper mode (Windows .cmd/.bat): argv[0] is "svc-shim.exe" and
//     argv[1] is the command name, args are argv[2:]. This is needed because
//     .cmd/.bat files cannot be hardlinked to a PE binary; a wrapper batch
//     script delegates to svc-shim.exe with the command name as argv[1].
func parseInvocation() Invocation {
	if len(os.Args) == 0 {
		return Invocation{}
	}
	base := filepath.Base(os.Args[0])
	if runtime.GOOS == "windows" {
		base = strings.TrimSuffix(base, ".exe")
	}
	if base == "svc-shim" {
		if len(os.Args) >= 2 {
			return Invocation{Command: os.Args[1], Args: os.Args[2:]}
		}
		return Invocation{}
	}
	return Invocation{Command: base, Args: os.Args[1:]}
}
