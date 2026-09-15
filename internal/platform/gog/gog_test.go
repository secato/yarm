package gog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/platform/sources/lutris/lutristest"
)

// jsonEscape returns s as it would appear inside a JSON string, without the
// surrounding quotes, so a Windows path's backslashes don't corrupt the
// hand-built JSON test fixtures below.
func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

// fakeRegistry stands in for the Windows registry in tests.
type fakeRegistry struct {
	rows []gogGame
	err  error
}

func (f fakeRegistry) games() ([]gogGame, error) { return f.rows, f.err }

// writeHeroicDir builds a heroic config directory with one installed
// GOG game, optionally skipping the DLC flag.
func writeHeroicDir(t *testing.T, appID, installPath string, isDLC bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "gog_store"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"installed": [{"appName": "` + appID + `", "install_path": "` + jsonEscape(installPath) + `", "is_dlc": ` + boolText(isDLC) + `}]}`
	if err := os.WriteFile(filepath.Join(dir, "gog_store", "installed.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// mkdirGame creates a game install directory under a temp root and
// returns its absolute path, so Entry.Game's existence check passes.
func mkdirGame(t *testing.T, parts ...string) string {
	t.Helper()
	root := filepath.Join(append([]string{t.TempDir()}, parts...)...)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDiscoverFromAllSources(t *testing.T) {
	registryGame := mkdirGame(t, "reg", "PuzzleAgent")
	heroicGame := mkdirGame(t, "heroic", "Redguard")
	lutrisGame := mkdirGame(t, "lutris", "Somer")

	reg := fakeRegistry{rows: []gogGame{
		{id: "1207658924", name: "Puzzle Agent", path: registryGame},
		{id: "42", name: "Some DLC", path: registryGame, dependsOn: "1207658924"},
		{id: "43", name: "Gone", path: filepath.Join(registryGame, "uninstalled")},
	}}
	heroicDir := writeHeroicDir(t, "1435829617", heroicGame, false)

	p := NewWith(reg, []string{heroicDir}, []string{lutrisDB(t, "Somer", "9667", lutrisGame)})

	games, err := p.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(games) != 3 {
		t.Fatalf("Discover() returned %d games, want 3: %+v", len(games), games)
	}
	byID := make(map[string]game.Game)
	for _, g := range games {
		byID[g.ID] = g
	}
	for _, id := range []string{"gog:1207658924", "gog:1435829617", "gog:9667"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("missing game %s in %+v", id, byID)
		}
	}
	if g := byID["gog:1207658924"]; g.Name != "Puzzle Agent" || g.Provider != "gog" || g.Root != registryGame {
		t.Errorf("registry game = %+v", g)
	}
}

func TestDiscoverPrefersNativeSourceOnDuplicateIDs(t *testing.T) {
	registryGame := mkdirGame(t, "reg", "PuzzleAgent")
	heroicGame := mkdirGame(t, "heroic", "PuzzleAgentOther")

	reg := fakeRegistry{rows: []gogGame{{id: "1207658924", name: "Puzzle Agent", path: registryGame}}}
	heroicDir := writeHeroicDir(t, "1207658924", heroicGame, false)

	games, err := NewWith(reg, []string{heroicDir}, nil).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("Discover() returned %d games, want 1 for one game installed twice: %+v", len(games), games)
	}
	if games[0].Root != registryGame {
		t.Errorf("Root = %q, want the registry install to win over the launcher one", games[0].Root)
	}
}

func TestDiscoverWithoutRegistry(t *testing.T) {
	heroicGame := mkdirGame(t, "heroic", "Redguard")
	heroicDir := writeHeroicDir(t, "1435829617", heroicGame, false)

	games, err := NewWith(nil, []string{heroicDir}, nil).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(games) != 1 || games[0].ID != "gog:1435829617" {
		t.Fatalf("Discover() = %+v, want the heroic-installed game", games)
	}
}

func TestDiscoverWithNothingInstalled(t *testing.T) {
	games, err := NewWith(fakeRegistry{}, []string{t.TempDir()}, []string{filepath.Join(t.TempDir(), "pga.db")}).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if games != nil {
		t.Errorf("Discover() = %+v, want nil with nothing installed", games)
	}
}

func TestDiscoverSkipsDLCAndUninstalled(t *testing.T) {
	base := mkdirGame(t, "gog", "PuzzleAgent")
	dlcDir := mkdirGame(t, "gog", "PuzzleAgent", "dlc")
	reg := fakeRegistry{rows: []gogGame{
		{id: "1207658924", name: "Puzzle Agent", path: base},
		{id: "1207659999", name: "Puzzle Agent DLC", path: dlcDir, dependsOn: "1207658924"},
	}}
	heroicDir := writeHeroicDir(t, "1207658888", dlcDir, true)

	games, err := NewWith(reg, []string{heroicDir}, nil).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(games) != 1 || games[0].ID != "gog:1207658924" {
		t.Errorf("Discover() = %+v, want only the base game (DLC skipped from both sources)", games)
	}
}

func TestName(t *testing.T) {
	if got := (&Provider{}).Name(); got != "gog" {
		t.Errorf("Name() = %q, want gog", got)
	}
}

// lutrisDB builds a pga.db with one installed GOG row.
func lutrisDB(t *testing.T, name, serviceID, directory string) string {
	t.Helper()
	return lutristest.Create(t, lutristest.Row{
		Name: name, Service: "gog", ServiceID: serviceID, Directory: directory, Installed: true,
	})
}
