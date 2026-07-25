package pathmgr

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// detectSdkTypeFromPath infers the SDK type from keywords in the path
func detectSdkTypeFromPath(p string) string {
	lower := strings.ToLower(filepath.ToSlash(p))
	switch {
	case strings.Contains(lower, "/node") || strings.Contains(lower, "/nodejs") || strings.Contains(lower, "\\node"):
		return "nodejs"
	case strings.Contains(lower, "/jdk") || strings.Contains(lower, "/java") || strings.Contains(lower, "\\jdk"):
		return "jdk"
	case strings.Contains(lower, "/go/") || strings.Contains(lower, "\\go\\") ||
		strings.HasSuffix(lower, "/go/bin") || strings.HasSuffix(lower, "\\go\\bin"):
		return "go"
	case strings.Contains(lower, "/python") || strings.Contains(lower, "\\python"):
		return "python"
	case strings.Contains(lower, "/rust") || strings.Contains(lower, "\\rust") ||
		strings.Contains(lower, "/cargo") || strings.Contains(lower, "\\cargo"):
		return "rust"
	case strings.Contains(lower, "/ruby") || strings.Contains(lower, "\\ruby"):
		return "ruby"
	case strings.Contains(lower, "/dotnet") || strings.Contains(lower, "\\dotnet"):
		return "dotnet"
	case strings.Contains(lower, "/php") || strings.Contains(lower, "\\php"):
		return "php"
	case strings.Contains(lower, "/perl") || strings.Contains(lower, "\\perl") ||
		strings.Contains(lower, "/strawberry") || strings.Contains(lower, "\\strawberry"):
		return "perl"
	case strings.Contains(lower, "/maven") || strings.Contains(lower, "/mvn") || strings.Contains(lower, "\\maven"):
		return "maven"
	case strings.Contains(lower, "/gradle") || strings.Contains(lower, "\\gradle"):
		return "gradle"
	case strings.Contains(lower, "/flutter") || strings.Contains(lower, "\\flutter"):
		return "flutter"
	case strings.Contains(lower, "/android") || strings.Contains(lower, "\\android"):
		return "android"
	case strings.Contains(lower, "/dart") || strings.Contains(lower, "\\dart"):
		return "dart"
	}
	return ""
}

// detectSdkTypeByBin checks whether a directory contains a characteristic executable
func detectSdkTypeByBin(dir string) string {
	dirs := []string{dir, filepath.Join(dir, "bin")}
	// Characteristic executables (cross-platform)
	checks := []struct {
		bin     string
		sdkType string
	}{
		{"node", "nodejs"},
		{"javac", "jdk"},
		{"go", "go"},
		{"python3", "python"},
		{"python", "python"},
		{"rustc", "rust"},
		{"cargo", "rust"},
		{"ruby", "ruby"},
		{"dotnet", "dotnet"},
		{"php", "php"},
		{"perl", "perl"},
		{"mvn", "maven"},
		{"gradle", "gradle"},
		{"flutter", "flutter"},
		{"sdkmanager", "android"},
		{"dart", "dart"},
	}
	for _, d := range dirs {
		for _, c := range checks {
			// Check no extension (Unix) and with extension (Windows)
			for _, ext := range []string{"", ".exe", ".cmd", ".bat"} {
				if _, err := os.Stat(filepath.Join(d, c.bin+ext)); err == nil {
					return c.sdkType
				}
			}
		}
	}
	return detectSdkTypeFromPath(dir)
}

// DetectSdkRoot walks up from the bin directory to find the SDK root
func DetectSdkRoot(binDir string, sdkType string) string {
	binDir = filepath.Clean(binDir)
	candidate := binDir
	if strings.ToLower(filepath.Base(candidate)) == "bin" {
		candidate = filepath.Dir(candidate)
	}

	// Verify root directory
	switch sdkType {
	case "go":
		if _, err := os.Stat(filepath.Join(candidate, "bin")); err == nil {
			return candidate
		}
	case "jdk":
		if _, err := os.Stat(filepath.Join(candidate, "release")); err == nil {
			return candidate
		}
	case "nodejs":
		// Node.js root contains the node executable
		for _, ext := range []string{"", ".exe"} {
			if _, err := os.Stat(filepath.Join(candidate, "node"+ext)); err == nil {
				return candidate
			}
			if _, err := os.Stat(filepath.Join(candidate, "bin", "node"+ext)); err == nil {
				return candidate
			}
		}
	}
	return candidate
}

// DeduplicateEntries dedupes entries by SDK root, keeping only one record per SDK install
// When duplicate entries exist, the IsManaged=true flag is preserved with priority
func DeduplicateEntries(entries []PathEntry) []PathEntry {
	seen := make(map[string]int) // key -> index in result
	var result []PathEntry

	for _, e := range entries {
		var key string
		if e.SdkType == "" {
			key = "unknown:" + strings.ToLower(filepath.Clean(e.Path))
		} else {
			root := DetectSdkRoot(e.Path, e.SdkType)
			if e.SdkType == "jdk" {
				root = normalizeJdkRoot(root)
			}
			key = e.SdkType + ":" + strings.ToLower(root)
		}

		if idx, exists := seen[key]; exists {
			if e.IsManaged {
				result[idx].IsManaged = true
			}
			continue
		}

		seen[key] = len(result)
		if e.SdkType != "" {
			e.Path = DetectSdkRoot(e.Path, e.SdkType)
			if e.SdkType == "jdk" {
				e.Path = normalizeJdkRoot(e.Path)
			}
		}
		result = append(result, e)
	}
	return result
}

// normalizeJdkRoot normalizes a JRE path to the JDK root path
// e.g. jdk-1.8.0/jre -> jdk-1.8.0
func normalizeJdkRoot(root string) string {
	// If the root directory name is "jre", check whether the parent directory is the JDK root
	if strings.ToLower(filepath.Base(root)) == "jre" {
		parent := filepath.Dir(root)
		// Check whether the parent directory has bin/javac, confirming it is the JDK root
		for _, ext := range []string{"", ".exe"} {
			if _, err := os.Stat(filepath.Join(parent, "bin", "javac"+ext)); err == nil {
				return parent
			}
		}
		// A release file in the parent also confirms it is the JDK root
		if _, err := os.Stat(filepath.Join(parent, "release")); err == nil {
			return parent
		}
	}
	return root
}

// ExtractVersion extracts a pure version number from a directory name
// e.g. jdk-1.8.412 -> 1.8.412, node-v18.14.0-win-x64 -> 18.14.0, go1.25.11 -> 1.25.11
func ExtractVersion(dirName string) string {
	// Match version patterns: digits.digits[.digits][_digits][-rc1 etc.]
	re := regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?(?:[._]\d+)?(?:[-.](?:rc|alpha|beta)\d*)?)`)
	match := re.FindString(dirName)
	if match != "" {
		// Replace _ with . to normalize the format
		return strings.ReplaceAll(match, "_", ".")
	}
	return dirName
}

// CopyDir recursively copies a directory
func CopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}
		return copyFile(path, dstPath, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// GetDesktopDir returns the current user's desktop directory (cross-platform)
func GetDesktopDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	desktop := filepath.Join(home, "Desktop")
	if info, err := os.Stat(desktop); err == nil && info.IsDir() {
		return desktop, nil
	}

	// On macOS the desktop is ~/Desktop; if it does not exist, fall back to <home>/Desktop
	if _, err := os.Stat(desktop); os.IsNotExist(err) {
		// Try to create the desktop directory if the user does not have one
		if err := os.MkdirAll(desktop, 0755); err == nil {
			return desktop, nil
		}
	}

	return home, nil
}

// BackupDir copies a directory to the desktop with a timestamped name
func BackupDir(src string) (string, error) {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return "", fmt.Errorf("source directory does not exist: %s", src)
	}

	desktop, err := GetDesktopDir()
	if err != nil {
		return "", fmt.Errorf("failed to get desktop directory: %w", err)
	}

	baseName := filepath.Base(src)
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s_backup_%s", baseName, timestamp)
	backupPath := filepath.Join(desktop, backupName)

	if _, err := os.Stat(backupPath); err == nil {
		// Edge case: multiple backups in the same second; add a random suffix
		backupPath = filepath.Join(desktop, fmt.Sprintf("%s_%d", backupName, time.Now().UnixNano()%10000))
	}

	if err := CopyDir(src, backupPath); err != nil {
		return "", fmt.Errorf("failed to backup directory: %w", err)
	}

	return backupPath, nil
}
