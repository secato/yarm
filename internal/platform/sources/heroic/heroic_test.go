package heroic

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfigDir builds a heroic config directory with the given files
// (paths relative to the config root) for a test.
func writeConfigDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const gogLibrary = `{
	"games": [
		{"app_name": "1207658924", "title": "Puzzle Agent", "is_installed": true},
		{"app_name": "1435829617", "title": "Redguard", "is_installed": false}
	]
}`

const legendaryInstalled = `{
	"Cypress": {
		"app_name": "Cypress",
		"title": "Cypress Tree",
		"install_path": "/games/epic/Cypress",
		"is_dlc": false
	}
}`

func TestRead(t *testing.T) {
	dir := writeConfigDir(t, map[string]string{
		// The modern shape: the install list wrapped in an object, with
		// titles living in the store cache.
		"gog_store/installed.json": `{"installed": [
			{"appName": "1207658924", "install_path": "/games/gog/PuzzleAgent", "is_dlc": false},
			{"appName": "1435829617", "install_path": "/games/gog/Redguard", "is_dlc": false},
			{"appName": "1207659999", "install_path": "/games/gog/PuzzleAgent/dlc1", "is_dlc": true}
		]}`,
		"store_cache/gog_library.json":             gogLibrary,
		"legendaryConfig/legendary/installed.json": legendaryInstalled,
	})

	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	var gog, epic int
	for _, e := range entries {
		switch e.Store {
		case "gog":
			gog++
			if e.StoreID == "1207658924" && (e.Title != "Puzzle Agent" || e.Root != "/games/gog/PuzzleAgent") {
				t.Errorf("gog entry = %+v, want title from the library cache and the install path", e)
			}
		case "epic":
			epic++
			if e.StoreID != "Cypress" || e.Root != "/games/epic/Cypress" {
				t.Errorf("epic entry = %+v, want the legendary-installed game", e)
			}
		default:
			t.Errorf("entry = %+v, unexpected store", e)
		}
	}
	if gog != 2 {
		t.Errorf("gog entries = %d, want 2 (DLC skipped)", gog)
	}
	if epic != 1 {
		t.Errorf("epic entries = %d, want 1", epic)
	}
}

func TestReadBareArrayInstalls(t *testing.T) {
	// Older Heroic wrote the install list as a bare array.
	dir := writeConfigDir(t, map[string]string{
		"gog_store/installed.json": `[
			{"appName": "1207658924", "install_path": "/games/gog/PuzzleAgent", "is_dlc": false}
		]`,
	})

	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(entries) != 1 || entries[0].StoreID != "1207658924" {
		t.Errorf("Read() = %+v, want the one bare-array game", entries)
	}
}

func TestReadLegacyTitleCacheLocation(t *testing.T) {
	dir := writeConfigDir(t, map[string]string{
		"gog_store/installed.json": `{"installed": [
			{"appName": "1207658924", "install_path": "/games/gog/PuzzleAgent", "is_dlc": false}
		]}`,
		"store/gog_library.json": gogLibrary,
	})

	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Title != "Puzzle Agent" {
		t.Errorf("Read() = %+v, want the title from the legacy library location", entries)
	}
}

func TestReadMissingDirectory(t *testing.T) {
	entries, err := Read(filepath.Join(t.TempDir(), "heroic"))
	if err != nil {
		t.Fatalf("Read() error = %v, want nil for a missing directory", err)
	}
	if entries != nil {
		t.Errorf("Read() = %+v, want nil", entries)
	}
}

func TestReadCorruptInstallsFileIsAnError(t *testing.T) {
	dir := writeConfigDir(t, map[string]string{
		"gog_store/installed.json": `{"installed": [`,
	})

	if _, err := Read(dir); err == nil {
		t.Error("Read() error = nil, want an error for a corrupt installed list")
	}
}

func TestReadSkipsStaleAndDLCEpicEntries(t *testing.T) {
	dir := writeConfigDir(t, map[string]string{
		"legendaryConfig/legendary/installed.json": `{
			"Cypress": {"app_name": "Cypress", "title": "Cypress Tree", "install_path": "/games/epic/Cypress", "is_dlc": false},
			"CypressDLC": {"app_name": "CypressDLC", "title": "Cypress DLC", "install_path": "/games/epic/Cypress", "is_dlc": true},
			"Stale": {"app_name": "Stale", "title": "Removed Game", "install_path": "", "is_dlc": false}
		}`,
	})

	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Read() returned %d entries, want 1 (epic DLC and stale skipped): %+v", len(entries), entries)
	}
	if entries[0].Store != "epic" || entries[0].StoreID != "Cypress" || entries[0].Root != "/games/epic/Cypress" {
		t.Errorf("epic entry = %+v", entries[0])
	}
}

func TestReadCorruptEpicInstallsFileIsAnError(t *testing.T) {
	dir := writeConfigDir(t, map[string]string{
		"legendaryConfig/legendary/installed.json": `{"Cypress": {"app_name":`,
	})

	if _, err := Read(dir); err == nil {
		t.Error("Read() error = nil, want an error for a corrupt epic installed list")
	}
}

func TestReadCorruptTitleCacheCostsOnlyTitles(t *testing.T) {
	dir := writeConfigDir(t, map[string]string{
		"gog_store/installed.json": `{"installed": [
			{"appName": "1207658924", "install_path": "/games/gog/PuzzleAgent", "is_dlc": false}
		]}`,
		"store_cache/gog_library.json": `{"games": [`,
	})

	entries, err := Read(dir)
	if err != nil {
		t.Fatalf("Read() error = %v, want nil: a corrupt library cache should not cost the games", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Read() = %+v, want the game without its title", entries)
	}
	if entries[0].Title != "" {
		t.Errorf("Title = %q, want empty so the game falls back to its folder name", entries[0].Title)
	}
}

func TestDefaultConfigDirs(t *testing.T) {
	for _, dir := range DefaultConfigDirs() {
		if !filepath.IsAbs(dir) {
			t.Errorf("DefaultConfigDirs() = %q, want absolute paths", dir)
		}
	}
}
