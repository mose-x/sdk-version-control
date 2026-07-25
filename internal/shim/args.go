package shim

import "os"

// osArgs returns the arguments passed to the shim (excluding argv[0] which is the shim name).
func osArgs() []string {
	if len(os.Args) <= 1 {
		return nil
	}
	return os.Args[1:]
}
