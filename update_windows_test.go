//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestSha256OfFile_roundTrip pins the helper that ApplyUpdate relies on for
// pre-copy hashing: same bytes -> same non-empty digest; different bytes ->
// different digest. ApplyUpdate feeds the digest into buildUpdateScript's
// post-copy certutil check, so a regression here silently disables rollback.
func TestSha256OfFile_roundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake_update.exe")
	payload := []byte("svc-update-payload-v1")
	if err := os.WriteFile(path, payload, 0644); err != nil {
		t.Fatal(err)
	}
	h1, err := sha256OfFile(path)
	if err != nil {
		t.Fatalf("sha256OfFile first call: %v", err)
	}
	if h1 == "" {
		t.Fatal("sha256OfFile returned empty digest")
	}
	h2, err := sha256OfFile(path)
	if err != nil {
		t.Fatalf("sha256OfFile second call: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("sha256OfFile not deterministic: %s then %s", h1, h2)
	}

	// Different bytes -> different digest.
	other := filepath.Join(dir, "other.exe")
	if err := os.WriteFile(other, []byte("different-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	h3, err := sha256OfFile(other)
	if err != nil {
		t.Fatalf("sha256OfFile on other: %v", err)
	}
	if h1 == h3 {
		t.Fatalf("sha256OfFile collision between distinct payloads: %s", h1)
	}
}

// TestBuildUpdateScript_containsHashAndCertutil pins BUG D's fix: the .bat
// written by ApplyUpdate must (a) embed the pre-copy SHA256, (b) invoke
// certutil -hashfile on the replaced exe to verify the post-copy bytes, and
// (c) roll back to the .bak on mismatch. Without these, a partial copy
// leaves a corrupt binary in place of the working app with no auto-recovery.
func TestBuildUpdateScript_containsHashAndCertutil(t *testing.T) {
	const hash = "deadbeefcafef00dba5eba11cafebabedeadbeefcafef00dba5eba11cafebabe"
	const pid = 4242
	const currentExe = `C:\svc\svc.exe`
	const bak = `C:\svc\svc.exe.bak`
	const newExe = `C:\tmp\svc_update_new.exe`
	script := buildUpdateScript(pid, currentExe, bak, newExe, hash)

	checks := []struct {
		name string
		want string
	}{
		{"embeds expected hash", hash},
		{"invokes certutil", `certutil -hashfile "` + currentExe + `" SHA256`},
		{"compares actual vs expected", `if /i not "%actual%"=="%expected%"`},
		{"uses move for replacement", `move /Y "` + newExe + `" "` + currentExe + `"`},
		{"has copy fallback", `copy /Y "` + newExe + `" "` + currentExe + `"`},
		{"rolls back to bak", `copy /Y "` + bak + `" "` + currentExe + `"`},
		{"exits non-zero on mismatch", "exit /b 1"},
		// M11: exact PID match. The new findstr /B /C:"<pid> " matches the
		// PID at the beginning of a line with a trailing space (so PID 4242
		// does not match a row for PID 42421 or 14242).
		{"uses findstr exact PID match", `findstr /B /C:"` + strconv.Itoa(pid) + ` " >NUL`},
	}
	for _, c := range checks {
		if !strings.Contains(script, c.want) {
			t.Errorf("buildUpdateScript missing %q\n--- script ---\n%s", c.name, script)
		}
	}
	// M11: the old substring `find "<pid>"` pattern must NOT be present — it
	// matched any PID containing the same digits (e.g. 4242 vs 42421).
	if strings.Contains(script, `find "`+strconv.Itoa(pid)+`" >NUL`) {
		t.Errorf("buildUpdateScript still uses the old substring `find %%d` pattern (M11 not fixed)\n--- script ---\n%s", script)
	}
	// The loop var must use the batch `%%i` form (a single `%%` in the file is
	// a stray percent that breaks the for-loop); a Go `%%%%i` template becomes
	// `%%i` in the output. Assert the rendered script has exactly `%%i` and
	// not the broken `%i` form anywhere except inside the `delims=` clause.
	if !strings.Contains(script, "%%i") {
		t.Errorf("buildUpdateScript missing batch loop var %%i\n--- script ---\n%s", script)
	}
}
