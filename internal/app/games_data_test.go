package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/secato/yarm/internal/game"
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

// ReShade intercepts by directory, not by executable: two executables
// sharing one folder must produce a single group with one ReShade status,
// not one finding per executable.
func TestGroupByFolderSharesOneReShadeStatusPerDirectory(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "Game")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for name, body := range map[string]string{"dxgi.dll": "dll body", "ReShade.ini": "[GENERAL]\n"} {
		if err := os.WriteFile(filepath.Join(gameDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	exes := []Executable{
		{Executable: game.Executable{Path: "Game/eldenring.exe"}},
		{Executable: game.Executable{Path: "Game/start_protected_game.exe"}},
	}
	groups := groupByFolder(dir, exes)

	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1 (both exes share Game/)", len(groups))
	}
	if groups[0].Unmanaged == nil {
		t.Fatal("the shared folder should be flagged unmanaged")
	}
	if len(groups[0].Exes) != 2 {
		t.Errorf("group has %d exes, want 2", len(groups[0].Exes))
	}
}

// The detail screen must say what's found once per folder, not once per
// executable — the bug this whole grouping model exists to fix.
func TestGameDetailShowsOneUnmanagedStatusNotPerExecutable(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "Game")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for name, body := range map[string]string{"dxgi.dll": "dll body", "ReShade.ini": "[GENERAL]\n"} {
		if err := os.WriteFile(filepath.Join(gameDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	exes := []Executable{
		{Executable: game.Executable{Path: "Game/eldenring.exe", Arch: game.ArchX64, API: game.APID3D12}},
		{Executable: game.Executable{Path: "Game/start_protected_game.exe", Arch: game.ArchX64}},
	}
	entry := GameEntry{
		Game:   game.Game{ID: "manual:x", Name: "ELDEN RING", Root: dir},
		Exes:   exes,
		Groups: groupByFolder(dir, exes),
	}

	gd := NewGameDetailScreen(entry, Deps{})
	env := Env{Styles: NewStyles(true), Width: 100, Height: 30}
	gd.resize(env)
	body := gd.View(env)

	if n := strings.Count(body, "found, untracked"); n != 1 {
		t.Errorf("body mentions \"found, untracked\" %d time(s), want exactly 1:\n%s", n, body)
	}
	if !strings.Contains(body, "start_protected_game.exe") {
		t.Error("both executables should still be listed, just without a repeated status")
	}
}
