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

func extractVersionFromOutput(cmd string, args []string) string {
	fullPath := resolveCommand(cmd)
	c := createCmd(fullPath, args...)
	sysPath := sdk.GetSystemPath()
	if sysPath != "" {
		c.Env = append(os.Environ(), "PATH="+sysPath)
	}
	out, err := c.CombinedOutput()
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)
	return re.FindString(string(out))
}

func (a *App) detectVersionFromDir(sdkRoot string, f sdk.VersionFetcher) (string, error) {
	cmdName, args := f.VerifyCommand()
	sdkType := string(f.Type())

	binDir := sdkRoot
	if d := filepath.Join(sdkRoot, "bin"); isDir(d) {
		binDir = d
	}

	binPath := findExecutable(binDir, cmdName)
	if binPath == "" {
		return "", fmt.Errorf("%s executable not found in directory", cmdName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c := createCmdContext(ctx, binPath, args...)

	env := os.Environ()
	sysPath := sdk.GetSystemPath()
	extraPath := binDir
	if sysPath != "" {
		extraPath = binDir + string(os.PathListSeparator) + sysPath
	}
	env = append(env, "PATH="+extraPath)

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

	re := regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)
	ver := re.FindString(string(out))
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

func resolveCommand(cmd string) string {
	sysPath := sdk.GetSystemPath()
	sep := ";"
	if runtime.GOOS != "windows" {
		sep = ":"
	}
	exts := []string{""}
	if runtime.GOOS == "windows" {
		exts = []string{"", ".exe", ".cmd", ".bat"}
	}
	for _, dir := range strings.Split(sysPath, sep) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
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
		return p
	}
	return cmd
}
