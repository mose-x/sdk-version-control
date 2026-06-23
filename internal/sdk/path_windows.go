//go:build windows

package sdk

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// getPlatformPath 从 Windows 注册表读取系统+用户 PATH
func getPlatformPath() string {
	var allPaths []string

	// 优先读取注册表（反映最新的版本切换），用户级优先于系统级
	for _, key := range []struct {
		root registry.Key
		path string
	}{
		{registry.CURRENT_USER, `Environment`},
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
	} {
		k, err := registry.OpenKey(key.root, key.path, registry.READ)
		if err != nil {
			continue
		}
		val, valType, err := k.GetStringValue("Path")
		k.Close()
		if err != nil || val == "" {
			continue
		}
		if valType == registry.EXPAND_SZ {
			expanded, err := registry.ExpandString(val)
			if err == nil {
				val = expanded
			}
		}
		parts := splitPathString(val)
		allPaths = append(allPaths, parts...)
	}

	// 进程 PATH 作为兜底
	pathEnv := os.Getenv("PATH")
	allPaths = append(allPaths, strings.Split(pathEnv, ";")...)

	// 去重
	seen := make(map[string]bool)
	var result []string
	for _, p := range allPaths {
		p = strings.TrimSpace(p)
		// 清理 Unicode 控制字符
		p = cleanUnicode(p)
		if p == "" {
			continue
		}
		lp := strings.ToLower(strings.TrimRight(p, "\\/"))
		if !seen[lp] {
			seen[lp] = true
			result = append(result, p)
		}
	}
	return strings.Join(result, ";")
}

// splitPathString 拆分 Windows PATH 字符串
// 同时处理分号分隔和空格分隔的情况
func splitPathString(s string) []string {
	// 先按分号拆分
	parts := strings.Split(s, ";")
	var result []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// 如果包含多个盘符，可能是空格分隔的，进一步拆分
		if strings.Count(part, ":\\") > 1 {
			sub := splitByDriveLetter(part)
			result = append(result, sub...)
		} else {
			result = append(result, part)
		}
	}
	return result
}

// splitByDriveLetter 按盘符拆分空格分隔的路径
// 例如 "C:\a b C:\c d" -> ["C:\a b", "C:\c d"]
func splitByDriveLetter(s string) []string {
	// 查找所有盘符位置
	var positions []int
	for i := 0; i < len(s)-2; i++ {
		if s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z' {
			if s[i+1] == ':' && s[i+2] == '\\' {
				// 确保前面是空格或开头
				if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
					positions = append(positions, i)
				}
			}
		}
	}
	if len(positions) <= 1 {
		return []string{s}
	}
	var result []string
	for i, pos := range positions {
		var end int
		if i+1 < len(positions) {
			end = positions[i+1]
		} else {
			end = len(s)
		}
		entry := strings.TrimSpace(s[pos:end])
		if entry != "" {
			result = append(result, entry)
		}
	}
	return result
}

// cleanUnicode 清理 Unicode 控制字符
func cleanUnicode(s string) string {
	return strings.Map(func(r rune) rune {
		// 删除 Unicode 双向控制字符和 BOM
		if r >= 0x202A && r <= 0x202E { return -1 }
		if r >= 0x2066 && r <= 0x2069 { return -1 }
		if r == 0xFEFF { return -1 }
		return r
	}, s)
}
