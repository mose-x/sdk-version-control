package pathmgr

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// detectSdkTypeFromPath 根据路径中的关键字推断 SDK 类型
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

// detectSdkTypeByBin 检查目录中是否存在特征性可执行文件
func detectSdkTypeByBin(dir string) string {
	dirs := []string{dir, filepath.Join(dir, "bin")}
	// 特征性可执行文件（跨平台）
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
			// 检查无扩展名（Unix）和有扩展名（Windows）
			for _, ext := range []string{"", ".exe", ".cmd", ".bat"} {
				if _, err := os.Stat(filepath.Join(d, c.bin+ext)); err == nil {
					return c.sdkType
				}
			}
		}
	}
	return detectSdkTypeFromPath(dir)
}

// DetectSdkRoot 从 bin 目录向上查找 SDK 根目录
func DetectSdkRoot(binDir string, sdkType string) string {
	binDir = filepath.Clean(binDir)
	candidate := binDir
	if strings.ToLower(filepath.Base(candidate)) == "bin" {
		candidate = filepath.Dir(candidate)
	}

	// 验证根目录
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
		// Node.js 根目录有 node 可执行文件
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

// DeduplicateEntries 按 SDK 根目录去重，同一个 SDK 安装只保留一条记录
// 如果存在重复条目，优先保留 IsManaged=true 的标记
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

// normalizeJdkRoot 将 JRE 路径规范化为 JDK 根路径
// 例如 jdk-1.8.0/jre -> jdk-1.8.0
func normalizeJdkRoot(root string) string {
	// 如果根目录名为 "jre"，检查父目录是否是 JDK 根
	if strings.ToLower(filepath.Base(root)) == "jre" {
		parent := filepath.Dir(root)
		// 检查父目录是否有 bin/javac，确认是 JDK 根
		for _, ext := range []string{"", ".exe"} {
			if _, err := os.Stat(filepath.Join(parent, "bin", "javac"+ext)); err == nil {
				return parent
			}
		}
		// 父目录有 release 文件也确认是 JDK 根
		if _, err := os.Stat(filepath.Join(parent, "release")); err == nil {
			return parent
		}
	}
	return root
}

// ExtractVersion 从目录名中提取纯版本号
// 例如: jdk-1.8.412 -> 1.8.412, node-v18.14.0-win-x64 -> 18.14.0, go1.25.11 -> 1.25.11
func ExtractVersion(dirName string) string {
	// 匹配版本号模式: 数字.数字[.数字][_数字][-rc1 等]
	re := regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?(?:[._]\d+)?(?:[-.](?:rc|alpha|beta)\d*)?)`)
	match := re.FindString(dirName)
	if match != "" {
		// 将 _ 替换为 . 以统一格式
		return strings.ReplaceAll(match, "_", ".")
	}
	return dirName
}

// CopyDir 递归复制目录
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
