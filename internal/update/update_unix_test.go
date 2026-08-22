//go:build !windows

package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteTempScript_UnpredictableNameAndExecutable pins the /tmp hardening:
// update/rollback scripts must be created via os.CreateTemp (O_CREATE|O_EXCL,
// random suffix, never follows pre-planted symlinks) instead of the old fixed
// world-guessable names svc_updater.sh / svc_rollback.sh, and must end up
// executable (0700) so /bin/sh can run them.
func TestWriteTempScript_UnpredictableNameAndExecutable(t *testing.T) {
	const content = "#!/bin/sh\necho hello\n"

	p1, err := writeTempScript("svc_updater_*.sh", content)
	if err != nil {
		t.Fatalf("writeTempScript first call: %v", err)
	}
	defer os.Remove(p1)
	p2, err := writeTempScript("svc_updater_*.sh", content)
	if err != nil {
		t.Fatalf("writeTempScript second call: %v", err)
	}
	defer os.Remove(p2)

	if p1 == p2 {
		t.Fatalf("writeTempScript returned the same path twice (%s): names must be unpredictable", p1)
	}
	for _, p := range []string{p1, p2} {
		base := filepath.Base(p)
		if !strings.HasPrefix(base, "svc_updater_") || !strings.HasSuffix(base, ".sh") {
			t.Errorf("script path %q does not match the svc_updater_*.sh pattern", p)
		}
		// The random suffix between prefix and ".sh" must be non-empty.
		if base == "svc_updater_.sh" {
			t.Errorf("script path %q has no random suffix", p)
		}
		// Must NOT be one of the old fixed guessable names.
		if base == "svc_updater.sh" || base == "svc_rollback.sh" {
			t.Errorf("script path %q is the old fixed name", p)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %q: %v", p, err)
		}
		if info.Mode().Perm() != 0700 {
			t.Errorf("script %q perm = %v; want 0700 (owner-executable)", p, info.Mode().Perm())
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %q: %v", p, err)
		}
		if string(got) != content {
			t.Errorf("script %q content = %q; want %q", p, got, content)
		}
	}
}

// TestWriteTempScript_RejectsSymlinkOccupiedName verifies the O_EXCL guarantee
// indirectly: CreateTemp never reuses an existing path, so even if an attacker
// pre-creates a file at a guessed name, the script lands elsewhere.
func TestWriteTempScript_NeverCollidesWithExistingFile(t *testing.T) {
	// Occupy many candidate names is impractical; instead assert the returned
	// path never equals a pre-existing file we control.
	pre := filepath.Join(os.TempDir(), "svc_updater_preexisting.sh")
	if err := os.WriteFile(pre, []byte("attacker"), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(pre)

	p, err := writeTempScript("svc_updater_*.sh", "#!/bin/sh\n")
	if err != nil {
		t.Fatalf("writeTempScript: %v", err)
	}
	defer os.Remove(p)
	if p == pre {
		t.Fatalf("writeTempScript reused the pre-existing path %s", pre)
	}
	got, err := os.ReadFile(pre)
	if err != nil || string(got) != "attacker" {
		t.Fatalf("pre-existing file was clobbered: content=%q err=%v", got, err)
	}
}

// TestBuildUnixUpdateScript pins the shell logic ApplyUpdate relies on: wait
// for the app to exit by exact PID, back up via mv, replace via mv, verify
// the post-copy SHA256 against the embedded pre-copy hash, and restore the
// backup on mismatch.
func TestBuildUnixUpdateScript(t *testing.T) {
	const hash = "deadbeefcafef00dba5eba11cafebabedeadbeefcafef00dba5eba11cafebabe"
	const pid = 4242
	const currentExe = "/opt/svc/svc"
	const bak = "/opt/svc/svc.bak"
	const newExe = "/tmp/svc_update_payload_123456"
	script := buildUnixUpdateScript(pid, currentExe, bak, newExe, hash)

	checks := []struct {
		name string
		want string
	}{
		{"embeds expected hash", "expected_hash='" + hash + "'"},
		{"waits on exact PID", "while kill -0 4242 2>/dev/null; do"},
		{"has wait timeout", `if [ "$timeout" -le 0 ]; then`},
		{"cleans stale backups before backup", `echo "Cleaning stale backups..."`},
		{"skips the current bak when cleaning", `[ "$oldbak" = '/opt/svc/svc.bak' ] && continue`},
		{"removes old-named bak files", `rm -f "$oldbak"`},
		{"backs up via mv", "mv -f '/opt/svc/svc' '/opt/svc/svc.bak'"},
		{"replaces via mv of payload", "mv -f '/tmp/svc_update_payload_123456' '/opt/svc/svc'"},
		{"makes result executable", "chmod +x '/opt/svc/svc'"},
		{"verifies sha256 post-copy", "sha256sum '/opt/svc/svc'"},
		{"compares hashes", `if [ "$actual_hash" != "$expected_hash" ]; then`},
		{"restores backup on mismatch", "mv -f '/opt/svc/svc.bak' '/opt/svc/svc'"},
		{"relaunches new binary", "nohup '/opt/svc/svc' > /dev/null 2>&1 &"},
		{"self-deletes", `rm -f "$0"`},
	}
	for _, c := range checks {
		if !strings.Contains(script, c.want) {
			t.Errorf("buildUnixUpdateScript missing %q\n--- script ---\n%s", c.name, script)
		}
	}
}

// TestBuildUnixUpdateScript_QuotesHostilePaths ensures paths with spaces and
// shell metacharacters are single-quoted so the script cannot be broken or
// injected via the install path.
func TestBuildUnixUpdateScript_QuotesHostilePaths(t *testing.T) {
	exe := `/opt/svc dir/with"space$/svc`
	script := buildUnixUpdateScript(1, exe, exe+".bak", "/tmp/payload", "abc")
	want := shellQuote(exe)
	if !strings.Contains(script, want) {
		t.Errorf("buildUnixUpdateScript does not shell-quote hostile path; want substring %q\n--- script ---\n%s", want, script)
	}
}

// TestBuildUnixRollbackScript pins the rollback shell logic: wait by exact
// PID, restore .bak over the current binary, chmod +x, relaunch.
func TestBuildUnixRollbackScript(t *testing.T) {
	const pid = 4242
	const currentExe = "/opt/svc/svc"
	const bak = "/opt/svc/svc.bak"
	script := buildUnixRollbackScript(pid, bak, currentExe)

	checks := []struct {
		name string
		want string
	}{
		{"waits on exact PID", "while kill -0 4242 2>/dev/null; do"},
		{"restores bak via mv", "mv -f '/opt/svc/svc.bak' '/opt/svc/svc'"},
		{"makes result executable", "chmod +x '/opt/svc/svc'"},
		{"relaunches restored binary", "nohup '/opt/svc/svc' > /dev/null 2>&1 &"},
		{"self-deletes", `rm -f "$0"`},
	}
	for _, c := range checks {
		if !strings.Contains(script, c.want) {
			t.Errorf("buildUnixRollbackScript missing %q\n--- script ---\n%s", c.name, script)
		}
	}
}

// TestSecureUpdatePayload verifies the downloaded payload is moved off the
// fixed guessable name to an unpredictable CreateTemp-based path with its
// content intact.
func TestSecureUpdatePayload(t *testing.T) {
	fixed := filepath.Join(t.TempDir(), "svc_update_new")
	payload := []byte("update-payload-bytes")
	if err := os.WriteFile(fixed, payload, 0644); err != nil {
		t.Fatal(err)
	}

	secure, err := secureUpdatePayload(fixed)
	if err != nil {
		t.Fatalf("secureUpdatePayload: %v", err)
	}
	defer os.Remove(secure)

	base := filepath.Base(secure)
	if !strings.HasPrefix(base, "svc_update_payload_") {
		t.Errorf("secure payload path %q does not use the svc_update_payload_* pattern", secure)
	}
	if base == "svc_update_payload_" {
		t.Errorf("secure payload path %q has no random suffix", secure)
	}
	if secure == fixed {
		t.Fatalf("payload still at the fixed guessable path %s", fixed)
	}
	if _, err := os.Stat(fixed); !os.IsNotExist(err) {
		t.Errorf("fixed payload path still exists after securing (stat err = %v)", err)
	}
	got, err := os.ReadFile(secure)
	if err != nil {
		t.Fatalf("read secured payload: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("secured payload content = %q; want %q", got, payload)
	}
}

// TestSecureUpdatePayload_MissingFile covers the error path: securing a
// payload that was never downloaded must fail cleanly.
func TestSecureUpdatePayload_MissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does_not_exist")
	secure, err := secureUpdatePayload(missing)
	if err == nil {
		os.Remove(secure)
		t.Fatal("secureUpdatePayload should fail when the payload does not exist")
	}
}
