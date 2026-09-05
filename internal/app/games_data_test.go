package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/secato/yarm/internal/platform"
	"github.com/secato/yarm/internal/platform/manual"
)

// A game folder that cannot even be scanned (most commonly a permissions
// problem) must surface why, not just look like an empty game — the
// plan's "friendly errors: permission denied on game dir" case.
func TestLoadGamesSurfacesScanError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}

	dir := t.TempDir()
	locked := filepath.Join(dir, "LockedGame")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	loader := ProviderLoader{
		Providers: []platform.Provider{manual.New([]manual.Entry{{Name: "Locked Game", Path: locked}})},
		StateDir:  t.TempDir(),
	}

	entries, err := loader.LoadGames(context.Background())
	if err != nil {
		t.Fatalf("LoadGames() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	e := entries[0]
	if e.ScanErr == nil {
		t.Fatal("ScanErr should be set for a folder that cannot be read")
	}
	if len(e.Exes) != 0 {
		t.Errorf("Exes = %v, want none", e.Exes)
	}
	if e.NativeBuild {
		t.Error("a scan error should not also trigger a native-build probe")
	}
}

// The games list and the detail views must both show this distinctly
// from an ordinary empty or native-build game.
func TestScanErrorRendersDistinctlyFromNativeBuild(t *testing.T) {
	entry := GameEntry{ScanErr: os.ErrPermission}
	gd := NewGameDetailScreen(entry, Deps{})
	body := gd.View(Env{Styles: NewStyles(true), Width: 100, Height: 30})

	if !strings.Contains(body, "Could not scan") || !strings.Contains(body, "Permission denied") {
		t.Errorf("detail view should explain the scan error:\n%s", body)
	}
}
