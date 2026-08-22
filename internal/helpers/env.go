package helpers

import "strings"

// ReplacePathEnv returns a new env slice derived from env with any existing
// PATH entry removed (case-insensitive on Windows where env keys are
// case-insensitive, exact match on Unix) and the given newPath appended.
// Used everywhere we need to override PATH for a spawned SDK command —
// previously the code did `append(os.Environ(), "PATH="+...)` which left the
// parent process's PATH in front of the new value, and on Windows the spawn
// inherited both (ambiguous; cmd.exe uses the FIRST match).
func ReplacePathEnv(env []string, newPath string) []string {
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
