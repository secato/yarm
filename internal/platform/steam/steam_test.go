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
	m, err := parseAppManifest(filepath.Join(fixtureDir, "appmanifest_700110.acf"))
	if err != nil {
		t.Fatalf("parseAppManifest() error = %v", err)
	}

	want := appManifest{appID: "700110", name: "Ember Hollow", installDir: "Ember Hollow"}
	if m != want {
		t.Errorf("parseAppManifest() = %+v, want %+v", m, want)
	}
}

func TestIsSkippedApp(t *testing.T) {
	tests := []struct {
		m    appManifest
		want bool
	}{
		{appManifest{appID: "700110", name: "Ember Hollow", installDir: "Ember Hollow"}, false},
		{appManifest{appID: "228980", name: "Steamworks Common Redistributables", installDir: "Steamworks Shared"}, true},
		{appManifest{appID: "1070560", name: "Steam Linux Runtime", installDir: "SteamLinuxRuntime"}, true},
		{appManifest{appID: "9999999", name: "Proton Experimental", installDir: "Proton - Experimental"}, true},
		{appManifest{appID: "9999998", name: "SteamVR", installDir: "SteamVR"}, true},
		{appManifest{appID: "9999997", name: "Vantage Point", installDir: "Vantage Point"}, false},
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

	writeManifest(t, steamRoot, "700110", "Ember Hollow", "Ember Hollow")
	mkGameDir(t, steamRoot, "Ember Hollow")

	writeManifest(t, steamRoot, "228980", "Steamworks Common Redistributables", "Steamworks Shared")
	mkGameDir(t, steamRoot, "Steamworks Shared")

	// Manifest present but the install directory is missing on disk: must
	// be excluded.
	writeManifest(t, steamRoot, "9999999", "Uninstalled Game", "Uninstalled Game")

	writeManifest(t, extraLibrary, "700220", "Vantage Point", "Vantage Point")
	mkGameDir(t, extraLibrary, "Vantage Point")

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
	if name, ok := byID["steam:700110"]; !ok || name != "Ember Hollow" {
		t.Errorf("expected steam:700110 = Ember Hollow, got %q (present=%v)", name, ok)
	}
	if name, ok := byID["steam:700220"]; !ok || name != "Vantage Point" {
		t.Errorf("expected steam:700220 = Vantage Point (from extra library), got %q (present=%v)", name, ok)
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

// installdir is a string out of a file yarm does not own, and it decides
// the directory yarm later writes DLLs into — so it may name a folder
// under the library's common/ and nothing else.
func TestSafeInstallDir(t *testing.T) {
	const common = "/games/steamapps/common"

	tests := []struct {
		name       string
		installDir string
		want       string
	}{
		{"plain folder", "Ember Hollow", filepath.Join(common, "Ember Hollow")},
		{"nested", "Game/Bin", filepath.Join(common, "Game", "Bin")},
		{"parent", "../../../escape", ""},
		{"parent deeper in", "Game/../../escape", ""},
		{"absolute", "/etc", ""},
		{"windows absolute", `C:\Windows`, ""},
		{"unc", `\\server\share`, ""},
		{"backslash parent", `..\..\escape`, ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := safeInstallDir(common, tt.installDir)
			if tt.want == "" {
				if ok {
					t.Errorf("safeInstallDir(%q) = %q, want refusal", tt.installDir, got)
				}
				return
			}
			if !ok {
				t.Fatalf("safeInstallDir(%q) refused, want %q", tt.installDir, tt.want)
			}
			if got != tt.want {
				t.Errorf("safeInstallDir(%q) = %q, want %q", tt.installDir, got, tt.want)
			}
		})
	}
}

// The same thing end to end: a manifest that points outside the library
// yields no game at all, even though the directory it names exists.
func TestScanLibraryRefusesEscapingInstallDir(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "library")
	steamapps := filepath.Join(lib, "steamapps")
	if err := os.MkdirAll(filepath.Join(steamapps, "common"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// The directory the escaping manifest points at: present, so only the
	// path check can be what refuses it.
	if err := os.MkdirAll(filepath.Join(root, "escape"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(steamapps, "common", "Ember Hollow"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	manifest := func(appID, name, installDir string) {
		acf := fmt.Sprintf("\"AppState\"\n{\n\t\"appid\"\t\t%q\n\t\"name\"\t\t%q\n\t\"installdir\"\t\t%q\n}\n",
			appID, name, installDir)
		path := filepath.Join(steamapps, "appmanifest_"+appID+".acf")
		if err := os.WriteFile(path, []byte(acf), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	manifest("1", "Escaper", "../../../escape")
	manifest("2", "Ember Hollow", "Ember Hollow")

	games := scanLibrary(lib)
	if len(games) != 1 {
		t.Fatalf("scanLibrary() found %d games, want only the legitimate one: %+v", len(games), games)
	}
	if games[0].Name != "Ember Hollow" {
		t.Errorf("scanLibrary() = %q, want Ember Hollow", games[0].Name)
	}
}

// A relative library path would resolve against yarm's working directory
// rather than anywhere Steam pointed it.
func TestParseLibraryFoldersRefusesRelativePaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "libraryfolders.vdf")
	vdf := "\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\"../../elsewhere\"\n\t}\n}\n"
	if err := os.WriteFile(path, []byte(vdf), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	paths, err := parseLibraryFolders(path)
	if err != nil {
		t.Fatalf("parseLibraryFolders() error = %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("parseLibraryFolders() = %v, want none", paths)
	}
}
