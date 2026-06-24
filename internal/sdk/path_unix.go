//go:build !windows

package sdk

import "os"

// getPlatformPath returns the process PATH directly on non-Windows platforms
func getPlatformPath() string {
	return os.Getenv("PATH")
}
