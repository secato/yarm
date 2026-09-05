package steam

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

const fixtureDir = "../../../testdata/steam"

func TestParseLibraryFolders(t *testing.T) {
	paths, err := parseLibraryFolders(filepath.Join(fixtureDir, "libraryfolders.vdf"))
	if err != nil {
		t.Fatalf("parseLibraryFolders() error = %v", err)
	}

	sort.Strings(paths)
	want := []string{"/home/f/.local/share/Steam", "/mnt/games/SteamLibrary"}
	if len(paths) != len(want) {
		t.Fatalf("parseLibraryFolders() = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("paths[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}

func TestParseLibraryFoldersMissingFile(t *testing.T) {
	if _, err := parseLibraryFolders(filepath.Join(t.TempDir(), "nope.vdf")); err == nil {
		t.Error("parseLibraryFolders() with missing file: expected error, got nil")
	}
}

func TestParseAppManifest(t *testing.T) {
	m, err := parseAppManifest(filepath.Join(fixtureDir, "appmanifest_1245620.acf"))
	if err != nil {
		t.Fatalf("parseAppManifest() error = %v", err)
	}

	want := appManifest{appID: "1245620", name: "ELDEN RING", installDir: "ELDEN RING"}
	if m != want {
		t.Errorf("parseAppManifest() = %+v, want %+v", m, want)
	}
}

func TestIsSkippedApp(t *testing.T) {
	tests := []struct {
		m    appManifest
		want bool
	}{
		{appManifest{appID: "1245620", name: "ELDEN RING", installDir: "ELDEN RING"}, false},
		{appManifest{appID: "228980", name: "Steamworks Common Redistributables", installDir: "Steamworks Shared"}, true},
		{appManifest{appID: "1070560", name: "Steam Linux Runtime", installDir: "SteamLinuxRuntime"}, true},
		{appManifest{appID: "9999999", name: "Proton Experimental", installDir: "Proton - Experimental"}, true},
		{appManifest{appID: "9999998", name: "SteamVR", installDir: "SteamVR"}, true},
		{appManifest{appID: "9999997", name: "Control", installDir: "Control"}, false},
	}

	for _, tt := range tests {
		if got := isSkippedApp(tt.m); got != tt.want {
			t.Errorf("isSkippedApp(%+v) = %v, want %v", tt.m, got, tt.want)
		}
	}
}

// writeVDF renders a minimal libraryfolders.vdf listing the given library
// paths, each with one dummy app entry (Discover doesn't read "apps" itself
// — it globs appmanifest_*.acf files directly — so the content there
// doesn't need to match).
func writeVDF(t *testing.T, path string, libraryPaths []string) {
	t.Helper()
	content := "\"libraryfolders\"\n{\n"
	for i, lp := range libraryPaths {
		content += fmt.Sprintf("\t%q\n\t{\n\t\t\"path\"\t\t%q\n\t\t\"apps\"\n\t\t{\n\t\t}\n\t}\n", fmt.Sprint(i), lp)
	}
	content += "}\n"

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func writeManifest(t *testing.T, libraryRoot, appID, name, installDir string) {
	t.Helper()
	content := fmt.Sprintf(
		"\"AppState\"\n{\n\t\"appid\"\t\t%q\n\t\"name\"\t\t%q\n\t\"installdir\"\t\t%q\n}\n",
		appID, name, installDir,
	)
	path := filepath.Join(libraryRoot, "steamapps", "appmanifest_"+appID+".acf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func mkGameDir(t *testing.T, libraryRoot, installDir string) {
	t.Helper()
	dir := filepath.Join(libraryRoot, "steamapps", "common", installDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", dir, err)
	}
}

func TestDiscover(t *testing.T) {
	steamRoot := t.TempDir()    // a Steam install root (has steamapps/libraryfolders.vdf)
	extraLibrary := t.TempDir() // a second library, found only via extra_library_paths

	writeVDF(t, filepath.Join(steamRoot, "steamapps", "libraryfolders.vdf"), []string{steamRoot})

	writeManifest(t, steamRoot, "1245620", "ELDEN RING", "ELDEN RING")
	mkGameDir(t, steamRoot, "ELDEN RING")

	writeManifest(t, steamRoot, "228980", "Steamworks Common Redistributables", "Steamworks Shared")
	mkGameDir(t, steamRoot, "Steamworks Shared")

	// Manifest present but the install directory is missing on disk: must
	// be excluded (docs/plan/04-external-sources.md §4.5).
	writeManifest(t, steamRoot, "9999999", "Uninstalled Game", "Uninstalled Game")

	writeManifest(t, extraLibrary, "1928420", "Control", "Control")
	mkGameDir(t, extraLibrary, "Control")

	p := NewWithRoots([]string{steamRoot}, []string{extraLibrary})
	if got := p.Name(); got != "steam" {
		t.Errorf("Name() = %q, want %q", got, "steam")
	}

	games, err := p.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	byID := make(map[string]string, len(games)) // id -> name
	for _, g := range games {
		byID[g.ID] = g.Name
		if g.Provider != "steam" {
			t.Errorf("game %s: Provider = %q, want steam", g.ID, g.Provider)
		}
	}

	if len(games) != 2 {
		t.Fatalf("Discover() returned %d games, want 2: %+v", len(games), games)
	}
	if name, ok := byID["steam:1245620"]; !ok || name != "ELDEN RING" {
		t.Errorf("expected steam:1245620 = ELDEN RING, got %q (present=%v)", name, ok)
	}
	if name, ok := byID["steam:1928420"]; !ok || name != "Control" {
		t.Errorf("expected steam:1928420 = Control (from extra library), got %q (present=%v)", name, ok)
	}
	if _, ok := byID["steam:228980"]; ok {
		t.Error("Steamworks Common Redistributables should be skipped")
	}
	if _, ok := byID["steam:9999999"]; ok {
		t.Error("game with missing install directory should be excluded")
	}
}

func TestDiscoverNoSteamInstalled(t *testing.T) {
	p := NewWithRoots([]string{t.TempDir()}, nil)

	games, err := p.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v, want nil (Steam simply not found)", err)
	}
	if len(games) != 0 {
		t.Errorf("Discover() = %+v, want empty", games)
	}
}
