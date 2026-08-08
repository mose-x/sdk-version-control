package shimmanager

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"sdk_version_control/internal/config"
	"sdk_version_control/internal/shim"
)

// newTestManager returns a Manager backed by a temp SVC dir (shims dir +
// shims.json under t.TempDir()) with a fake svc-shim binary in place so
// createShimFor can hardlink to it.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.SetHomeDir(dir) // RcFilePath derives from homeDir, not svcDir.
	cfg.SetSvcDir(dir)  // ShimsDir / ShimsConfigPath derive from svcDir.
	m := New(cfg)
	shimsDir := cfg.ShimsDir()
	if err := os.MkdirAll(shimsDir, 0755); err != nil {
		t.Fatal(err)
	}
	shimName := "svc-shim"
	if runtime.GOOS == "windows" {
		shimName = "svc-shim.exe"
	}
	if err := os.WriteFile(filepath.Join(shimsDir, shimName), []byte("fake-svc-shim"), 0755); err != nil {
		t.Fatal(err)
	}
	return m
}

func shimExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// --- ensurePython3Alias (pure logic) ---

func TestEnsurePython3Alias_addsWhenMissing(t *testing.T) {
	m := &Manager{}
	cfg := shim.ShimConfig{
		SdkTypes: map[string]shim.SdkShimEntry{"python": {}},
		Commands: map[string]string{"python": "python"},
	}
	if !m.ensurePython3Alias(&cfg) {
		t.Fatal("ensurePython3Alias = false; want true (should add python3)")
	}
	if got := cfg.Commands["python3"]; got != "python" {
		t.Fatalf(`Commands["python3"] = %q; want "python"`, got)
	}
}

func TestEnsurePython3Alias_noOpWhenPresent(t *testing.T) {
	m := &Manager{}
	cfg := shim.ShimConfig{
		SdkTypes: map[string]shim.SdkShimEntry{"python": {}},
		Commands: map[string]string{"python": "python", "python3": "python"},
	}
	if m.ensurePython3Alias(&cfg) {
		t.Fatal("ensurePython3Alias = true; want false (python3 already present)")
	}
}

func TestEnsurePython3Alias_noOpWhenNoPython(t *testing.T) {
	m := &Manager{}
	cfg := shim.ShimConfig{
		SdkTypes: map[string]shim.SdkShimEntry{"go": {}},
		Commands: map[string]string{"go": "go"},
	}
	if m.ensurePython3Alias(&cfg) {
		t.Fatal("ensurePython3Alias = true; want false (Python not in SdkTypes)")
	}
}

// --- ensurePython3Shim (filesystem: registers alias + creates shim) ---

func TestEnsurePython3Shim_addsAliasAndCreatesShim(t *testing.T) {
	m := newTestManager(t)
	seed := shim.ShimConfig{
		SdkTypes: map[string]shim.SdkShimEntry{"python": {BinDirs: []string{"python"}}},
		Commands: map[string]string{"python": "python"},
	}
	if err := m.saveShimConfig(m.cfg.ShimsConfigPath(), seed); err != nil {
		t.Fatal(err)
	}
	m.ensurePython3Shim()
	loaded := m.loadShimConfig()
	if got := loaded.Commands["python3"]; got != "python" {
		t.Fatalf(`shims.json Commands["python3"] = %q; want "python"`, got)
	}
	if _, err := os.Stat(filepath.Join(m.cfg.ShimsDir(), "python3"+shimExt())); err != nil {
		t.Fatalf("python3 shim file not created: %v", err)
	}
}

// --- createShimsForDirs (python3 alias on install/switch) ---

func TestCreateShimsForDirs_python3Alias(t *testing.T) {
	m := newTestManager(t)
	versionDir := t.TempDir()
	binName := "python" + shimExt()
	if err := os.WriteFile(filepath.Join(versionDir, binName), []byte("fake-python"), 0755); err != nil {
		t.Fatal(err)
	}
	created, err := m.createShimsForDirs(versionDir, []string{""}, "python")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range created {
		if c == "python3" {
			found = true
		}
	}
	if !found {
		t.Errorf("createShimsForDirs result = %v; want to contain python3", created)
	}
	if _, err := os.Stat(filepath.Join(m.cfg.ShimsDir(), "python3"+shimExt())); err != nil {
		t.Errorf("python3 shim not created: %v", err)
	}
}

// --- RemoveSdk (cleans python3) ---

func TestRemoveSdk_cleansPython3(t *testing.T) {
	m := newTestManager(t)
	shimsDir := m.cfg.ShimsDir()
	seed := shim.ShimConfig{
		SdkTypes: map[string]shim.SdkShimEntry{"python": {BinDirs: []string{"python"}}},
		Commands: map[string]string{"python": "python", "python3": "python"},
	}
	if err := m.saveShimConfig(m.cfg.ShimsConfigPath(), seed); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"python", "python3"} {
		if err := os.WriteFile(filepath.Join(shimsDir, name+shimExt()), []byte("x"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.RemoveSdk("python", nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"python", "python3"} {
		if _, err := os.Stat(filepath.Join(shimsDir, name+shimExt())); !os.IsNotExist(err) {
			t.Errorf("%s%s still exists after RemoveSdk; want removed", name, shimExt())
		}
	}
	loaded := m.loadShimConfig()
	if _, ok := loaded.Commands["python"]; ok {
		t.Error(`shims.json Commands["python"] still present after RemoveSdk`)
	}
	if _, ok := loaded.Commands["python3"]; ok {
		t.Error(`shims.json Commands["python3"] still present after RemoveSdk`)
	}
	if _, ok := loaded.SdkTypes["python"]; ok {
		t.Error(`shims.json SdkTypes["python"] still present after RemoveSdk`)
	}
}

// --- rebuildCommandShims (rebuilds existing command shims incl. python3) ---

func TestRebuildCommandShims_rebuildsPython3(t *testing.T) {
	m := newTestManager(t)
	shimsDir := m.cfg.ShimsDir()
	seed := shim.ShimConfig{
		SdkTypes: map[string]shim.SdkShimEntry{"python": {BinDirs: []string{"python"}}},
		Commands: map[string]string{"python": "python", "python3": "python"},
	}
	if err := m.saveShimConfig(m.cfg.ShimsConfigPath(), seed); err != nil {
		t.Fatal(err)
	}
	m.rebuildCommandShims()
	for _, name := range []string{"python", "python3"} {
		if _, err := os.Stat(filepath.Join(shimsDir, name+shimExt())); err != nil {
			t.Errorf("%s shim not rebuilt by rebuildCommandShims: %v", name, err)
		}
	}
}

// --- createShimFor (BUG F: removes other variants) ---

// TestCreateShimFor_removesOtherVariants pins BUG F: creating a .exe shim
// must also clean up any stale .cmd/.bat wrappers (and the bare file) so the
// old variant doesn't linger and shadow the new one. The previous code only
// removed the target variant (os.Remove(cmdName+ext)), so switching
// extensions left a stale sibling on disk.
func TestCreateShimFor_removesOtherVariants(t *testing.T) {
	m := newTestManager(t)
	shimsDir := m.cfg.ShimsDir()
	name := "node"
	ext := shimExt() // ".exe" on Windows, "" on Unix

	// Pre-create every variant that removeShim should purge as stale.
	// On Windows: bare + .exe + .cmd + .bat. On Unix: bare only (removeShim
	// is bare-only on Unix; .exe/.cmd/.bat are Windows-only cleanup).
	staleVariants := []string{name}
	if runtime.GOOS == "windows" {
		staleVariants = append(staleVariants, name+".exe", name+".cmd", name+".bat")
	}
	for _, v := range staleVariants {
		if err := os.WriteFile(filepath.Join(shimsDir, v), []byte("stale-"+v), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := m.createShimFor(name, ext); err != nil {
		t.Fatal(err)
	}

	// The new shim must exist + contain the svc-shim bytes (hardlink or copy
	// of svc-shim, which newTestManager wrote as "fake-svc-shim").
	target := filepath.Join(shimsDir, name+ext)
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("new shim %s not created: %v", target, err)
	}
	if string(got) != "fake-svc-shim" {
		t.Errorf("new shim content = %q; want %q (svc-shim bytes)", string(got), "fake-svc-shim")
	}

	// All OTHER pre-created variants must be gone.
	for _, v := range staleVariants {
		if v == name+ext {
			continue
		}
		if _, err := os.Stat(filepath.Join(shimsDir, v)); !os.IsNotExist(err) {
			t.Errorf("stale variant %s still exists after createShimFor; want removed", v)
		}
	}
}

// --- filesEqual (BUG G: content comparison helper) ---

// TestFilesEqual pins BUG G's helper: same bytes -> true; different bytes or
// different size -> false; missing file -> false. ensureShimBinary uses this
// to catch same-size-different-content binaries that the size+modtime
// heuristic would miss.
func TestFilesEqual(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shim.bin")
	content := []byte("hello shim bytes")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	if !filesEqual(path, content) {
		t.Error("filesEqual(path, content) = false; want true (identical bytes)")
	}
	if filesEqual(path, []byte("different content")) {
		t.Error("filesEqual(path, different) = true; want false (different content)")
	}
	// Different size: a prefix of the content is NOT equal (size check is
	// implicit in bytes.Equal, which returns false when lengths differ).
	if filesEqual(path, content[:len(content)-1]) {
		t.Error("filesEqual(path, content[:n-1]) = true; want false (different size)")
	}
	// Missing file -> false (read error is treated as not-equal).
	if filesEqual(filepath.Join(dir, "nope.bin"), content) {
		t.Error("filesEqual(missing, content) = true; want false (read error)")
	}
}
