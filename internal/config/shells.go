package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// ShellInfo describes a shell that can be configured with the SVC source line.
type ShellInfo struct {
	Name     string `json:"name"`     // e.g. "zsh", "bash", "fish"
	RcFile   string `json:"rcFile"`   // e.g. "~/.zshrc"
	FullPath string `json:"fullPath"` // absolute path
}

// AvailableShells returns the list of shells that can be configured on this platform.
func AvailableShells() []ShellInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	if runtime.GOOS == "windows" {
		// On Windows, the "shell" concept is different.
		// PATH is set in the registry; PowerShell profile is optional.
		psProfile := filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
		return []ShellInfo{
			{Name: "powershell", RcFile: "~/Documents/PowerShell/Microsoft.PowerShell_profile.ps1", FullPath: psProfile},
		}
	}

	var shells []ShellInfo
	shells = append(shells, ShellInfo{Name: "zsh", RcFile: "~/.zshrc", FullPath: filepath.Join(home, ".zshrc")})
	shells = append(shells, ShellInfo{Name: "bash", RcFile: "~/.bashrc", FullPath: filepath.Join(home, ".bashrc")})

	if runtime.GOOS == "darwin" {
		shells = append(shells, ShellInfo{Name: "bash_profile", RcFile: "~/.bash_profile", FullPath: filepath.Join(home, ".bash_profile")})
	}
	if runtime.GOOS == "linux" {
		shells = append(shells, ShellInfo{Name: "profile", RcFile: "~/.profile", FullPath: filepath.Join(home, ".profile")})
	}

	shells = append(shells, ShellInfo{Name: "zshenv", RcFile: "~/.zshenv", FullPath: filepath.Join(home, ".zshenv")})
	shells = append(shells, ShellInfo{Name: "fish", RcFile: "~/.config/fish/conf.d/svc.fish", FullPath: filepath.Join(home, ".config", "fish", "conf.d", "svc.fish")})

	return shells
}
