//go:build !windows

package sdk

import "os"

// getPlatformPath 非 Windows 平台直接返回进程 PATH
func getPlatformPath() string {
	return os.Getenv("PATH")
}
