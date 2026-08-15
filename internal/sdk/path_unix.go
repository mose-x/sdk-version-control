//go:build !windows

package sdk

import (
	"os"
	"os/exec"
	"sync"
)

var (
	cachedShellPath string
	pathOnce        sync.Once
)

// getPlatformPath returns the system PATH. On Unix, GUI apps launched from
// Dock/Finder inherit only /etc/paths, missing .zshrc/.bashrc additions.
// To match what a terminal session sees, we spawn a login shell and capture
// its PATH. The result is cached (sync.Once) so the shell is spawned only once.
func getPlatformPath() string {
	pathOnce.Do(func() {
		cachedShellPath = detectShellPath()
	})
	if cachedShellPath != "" {
		return cachedShellPath
	}
	return os.Getenv("PATH")
}

// detectShellPath spawns a login interactive shell and captures its $PATH.
// -l = login (sources /etc/profile, ~/.zprofile/.bash_profile),
// -i = interactive (sources ~/.zshrc/.bashrc),
// -c = command. printf (not echo) avoids trailing newline issues.
func detectShellPath() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-lic", "printf '%s' \"$PATH\"")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return ""
	}
	return string(out)
}
