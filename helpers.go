package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"sdk_version_control/internal/sdk"
)

// replacePathEnv returns a new env slice derived from env with any existing
// PATH entry removed (case-insensitive on Windows where env keys are
// case-insensitive, exact match on Unix) and the given newPath appended.
// Used everywhere we need to override PATH for a spawned SDK command —
// previously the code did `append(os.Environ(), "PATH="+...)` which left the
// parent process's PATH in front of the new value, and on Windows the spawn
// inherited both (ambiguous; cmd.exe uses the FIRST match).
func replacePathEnv(env []string, newPath string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		key, _, _ := splitEnvVar(kv)
		if strings.EqualFold(key, "PATH") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "PATH="+newPath)
}

// splitEnvVar splits an env entry "KEY=VALUE" into key and value. Returns empty
// key for entries without '='. Pure logic — safe to unit-test cross-platform.
func splitEnvVar(kv string) (key, value string, hasEq bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return "", kv, false
}

func extractVersionFromOutput(cmd string, args []string) string {
	fullPath := resolveCommand(cmd)
	if fullPath == "" {
		return ""
	}
	// H2: Bound the version-detection command so a hung binary (e.g. waiting
	// on stdin) doesn't block the UI forever.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := createCmdContext(ctx, fullPath, args...)
	sysPath := sdk.GetSystemPath()
	if sysPath != "" {
		c.Env = replacePathEnv(os.Environ(), sysPath)
	}
	out, err := c.CombinedOutput()
	if err != nil {
		return ""
	}
	return extractVersionFromString(string(out))
}

// versionPattern matches a version string like "1.2" or "1.2.3". Hoisted to a
// package-level var so it is compiled once at init, not recompiled per call
// (L1). extractVersionFromString is on the hot path (system-detected SDK
// version scans + every imported SDK directory probe).
var versionPattern = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)

// extractVersionFromString applies the version-extraction regex to a command's
// raw output. Used by extractVersionFromOutput (system-detected SDKs) and
// detectVersionFromDir (imported SDKs). Returns "" if no version pattern found.
func extractVersionFromString(s string) string {
	return versionPattern.FindString(s)
}

func (a *App) detectVersionFromDir(sdkRoot string, f sdk.VersionFetcher) (string, error) {
	cmdName, args := f.VerifyCommand()
	sdkType := string(f.Type())

	// Search each declared binDir for the executable. SDKs like Go
	// (go/bin), Dart (dart-sdk/bin), Android (cmdline-tools/bin) ship
	// binaries in wrapper subdirs that a plain sdkRoot/bin/ check misses.
	var binPath, binDir string
	for _, bd := range f.GetBinDirs() {
		dir := sdkRoot
		if bd != "" {
			dir = filepath.Join(sdkRoot, bd)
		}
		if p := findExecutable(dir, cmdName); p != "" {
			binPath = p
			binDir = dir
			break
		}
	}
	// Fallback: try sdkRoot/bin/ (common layout for stripped SDKs)
	if binPath == "" {
		if d := filepath.Join(sdkRoot, "bin"); isDir(d) {
			if p := findExecutable(d, cmdName); p != "" {
				binPath = p
				binDir = d
			}
		}
	}
	// Final fallback: sdkRoot itself (SDKs with binDir = "")
	if binPath == "" {
		binDir = sdkRoot
		binPath = findExecutable(binDir, cmdName)
	}
	if binPath == "" {
		return "", fmt.Errorf("%s executable not found in directory", cmdName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c := createCmdContext(ctx, binPath, args...)

	sysPath := sdk.GetSystemPath()
	extraPath := binDir
	if sysPath != "" {
		extraPath = binDir + string(os.PathListSeparator) + sysPath
	}
	env := replacePathEnv(os.Environ(), extraPath)

	if sdkType == "maven" || sdkType == "gradle" {
		javaHome := a.findJavaHome()
		if javaHome == "" {
			return "", fmt.Errorf("importing %s requires JDK to be installed first, please import or install JDK first", sdkType)
		}
		env = append(env, "JAVA_HOME="+javaHome)
	}

	if sdkType == "android" {
		javaHome := a.findJavaHome()
		if javaHome != "" {
			env = append(env, "JAVA_HOME="+javaHome)
		}
	}

	c.Env = env
	out, err := c.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("executing %s timed out (10s), unable to get version", cmdName)
		}
		return "", fmt.Errorf("failed to execute %s: %s", cmdName, strings.TrimSpace(string(out)))
	}

	ver := extractVersionFromString(string(out))
	if ver == "" {
		return "", fmt.Errorf("unable to parse version from %s output", cmdName)
	}
	return ver, nil
}

func findExecutable(dir, name string) string {
	exts := []string{""}
	if runtime.GOOS == "windows" {
		exts = []string{".exe", ".cmd", ".bat", ""}
	}
	for _, ext := range exts {
		p := filepath.Join(dir, name+ext)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

func (a *App) findJavaHome() string {
	jdkDir := a.cfg.SdkDir("jdk")
	activeVersion := a.cfg.GetActiveVersion("jdk")
	if activeVersion != "" {
		jdkRoot := filepath.Join(jdkDir, activeVersion)
		if isDir(jdkRoot) {
			return jdkRoot
		}
	}
	if entries, err := os.ReadDir(jdkDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				return filepath.Join(jdkDir, e.Name())
			}
		}
	}
	if jh := os.Getenv("JAVA_HOME"); jh != "" && isDir(jh) {
		return jh
	}
	return ""
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// resolveCommand resolves a command name to its full path using the system PATH.
// Returns "" when the command is not found (matching exec.LookPath semantics).
// The SVC-managed shims directory (~/.svc/shims) is excluded: shims there are
// generated by this app and route to the active SDK version. Finding a shim
// instead of a real system binary would mislead ImportPathSdk (importing the
// shim instead of the real SDK) and GetAllSdkStatus (reporting the SVC version
// as a system PATH version).
func resolveCommand(cmd string) string {
	sysPath := sdk.GetSystemPath()
	sep := string(os.PathListSeparator)
	exts := []string{""}
	if runtime.GOOS == "windows" {
		exts = []string{"", ".exe", ".cmd", ".bat"}
	}
	shimsDir := sdk.SvcShimsDir()
	for _, dir := range strings.Split(sysPath, sep) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if sdk.IsShimsDirEntry(dir, shimsDir) {
			continue
		}
		for _, ext := range exts {
			p := filepath.Join(dir, cmd+ext)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p
			}
		}
	}
	if p, err := exec.LookPath(cmd); err == nil {
		if !sdk.IsShimsPath(p, shimsDir) {
			return p
		}
	}
	return ""
}
