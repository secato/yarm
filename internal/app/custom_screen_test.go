package app

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCustomFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func loadCustomScreen(t *testing.T, dir string) *CustomScreen {
	t.Helper()
	s := NewCustomScreen(dir)
	msg := s.Init()()
	next, _ := s.Update(msg, wizardEnv())
	return next.(*CustomScreen)
}

func TestCustomScreenListsShadersAndAddons(t *testing.T) {
	dir := t.TempDir()
	writeCustomFile(t, dir, "shaders/My Pack/Shaders/Cool.fx", "// fx")
	writeCustomFile(t, dir, "addons/My Addon/thing.addon64", "bin")

	s := loadCustomScreen(t, dir)
	if s.loading {
		t.Fatal("loading should be false once the scan has completed")
	}
	if len(s.shaders) != 1 || s.shaders[0].Name != "My Pack" {
		t.Fatalf("shaders = %v", s.shaders)
	}
	if len(s.addons) != 1 || s.addons[0].Name != "My Addon" {
		t.Fatalf("addons = %v", s.addons)
	}
}

// A missing custom-content folder is normal (most users never create
// one) and must not be reported as an error.
func TestCustomScreenMissingDirIsNotAnError(t *testing.T) {
	s := loadCustomScreen(t, filepath.Join(t.TempDir(), "nope"))
	if s.loading {
		t.Fatal("loading should be false")
	}
	if len(s.shaders) != 0 || len(s.addons) != 0 {
		t.Errorf("expected nothing found, got shaders=%v addons=%v", s.shaders, s.addons)
	}
}

// A genuine scan failure must still clear loading and surface the error,
// not leave "scanning…" on screen forever.
func TestCustomScreenScanFailureClearsLoading(t *testing.T) {
	// A file where a directory is expected: os.ReadDir on it fails with
	// something other than "not exist".
	dir := t.TempDir()
	blocker := filepath.Join(dir, "shaders")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := NewCustomScreen(dir)
	msg := s.Init()()
	cm, ok := msg.(customLoadedMsg)
	if !ok || cm.err == nil {
		t.Fatalf("message = %#v, want a customLoadedMsg carrying an error", msg)
	}

	next, cmd := s.Update(msg, wizardEnv())
	s = next.(*CustomScreen)
	if s.loading {
		t.Error("loading should be false even after a failed scan")
	}
	if cmd == nil {
		t.Fatal("a scan failure should still produce a command reporting it")
	}
	if _, ok := cmd().(errorMsg); !ok {
		t.Error("the follow-up command should report the error to the shell")
	}
}

// The cursor must be able to reach every real entry and skip the section
// headers, same convention as the wizard's multi-select lists.
func TestCustomScreenCursorSkipsHeaders(t *testing.T) {
	dir := t.TempDir()
	writeCustomFile(t, dir, "shaders/Pack/Shaders/A.fx", "// fx")
	writeCustomFile(t, dir, "addons/Addon/a.addon64", "bin")

	s := loadCustomScreen(t, dir)
	// rows: [Shaders header, Pack, Add-ons header, Addon]
	if len(s.rows) != 4 {
		t.Fatalf("rows = %v, want 4", s.rows)
	}
	if s.rows[0].header != true || s.rows[2].header != true {
		t.Fatalf("expected section headers at 0 and 2: %+v", s.rows)
	}

	s.moveCursor(1) // header -> Pack
	if s.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (Pack)", s.cursor)
	}
}

// "o" puts the highlighted entry's real path in the status bar.
func TestCustomScreenPrintPath(t *testing.T) {
	dir := t.TempDir()
	writeCustomFile(t, dir, "shaders/Pack/Shaders/A.fx", "// fx")

	s := loadCustomScreen(t, dir)
	s.cursor = 1 // the "Pack" entry, past the Shaders header

	cmd := s.printSelectedPath()
	if cmd == nil {
		t.Fatal("printSelectedPath() should return a command")
	}
	msg, ok := cmd().(statusMsg)
	if !ok {
		t.Fatalf("message = %T, want statusMsg", cmd())
	}
	want := filepath.Join(dir, "shaders", "Pack")
	if msg.text != want {
		t.Errorf("status = %q, want %q", msg.text, want)
	}
}
