package epic

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/platform/sources/lutris/lutristest"
)

func mkdirGame(t *testing.T, parts ...string) string {
	t.Helper()
	root := filepath.Join(append([]string{t.TempDir()}, parts...)...)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeManifests(t *testing.T, items map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range items {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const gameItem = `{
	"FormatVersion": 0,
	"bIsIncompleteInstall": false,
	"bIsApplication": true,
	"DisplayName": "Cypress Tree",
	"AppName": "Cypress",
	"InstallLocation": "INSTALL",
	"LaunchExecutable": "Cypress.exe",
	"MainGameAppName": "Cypress"
}`

func TestDiscoverFromManifests(t *testing.T) {
	install := mkdirGame(t, "Epic Games", "Cypress")

	manifests := writeManifests(t, map[string]string{
		"Cypress.item": withInstall(gameItem, install),
		// DLC: the base game's AppName in MainGameAppName.
		"DLC.item": withInstall(`{
			"bIsApplication": true,
			"DisplayName": "Cypress DLC",
			"AppName": "CypressDLC",
			"InstallLocation": "INSTALL",
			"MainGameAppName": "Cypress"
		}`, install),
		// An incomplete install is not a game yet.
		"Partial.item": withInstall(`{
			"bIsIncompleteInstall": true,
			"bIsApplication": true,
			"DisplayName": "Partial",
			"AppName": "Partial",
			"InstallLocation": "INSTALL",
			"MainGameAppName": "Partial"
		}`, install),
		// A launcher tool, not a game.
		"Tool.item": withInstall(`{
			"bIsApplication": false,
			"DisplayName": "UE Tool",
			"AppName": "UETool",
			"InstallLocation": "INSTALL",
			"MainGameAppName": "UETool"
		}`, install),
	})

	games, err := NewWith([]string{manifests}, nil, nil).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("Discover() returned %d games, want 1 (DLC, partial and tool skipped): %+v", len(games), games)
	}
	if games[0].ID != "epic:Cypress" || games[0].Name != "Cypress Tree" || games[0].Root != install || games[0].Provider != "epic" {
		t.Errorf("game = %+v", games[0])
	}
}

// writeHeroicEpicDir builds a heroic config directory whose embedded
// legendary client reports one installed Epic game.
func writeHeroicEpicDir(t *testing.T, appName, title, installPath string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "legendaryConfig", "legendary"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
		"` + appName + `": {"app_name": "` + appName + `", "title": "` + title + `", "install_path": "` + installPath + `", "is_dlc": false}
	}`
	if err := os.WriteFile(filepath.Join(dir, "legendaryConfig", "legendary", "installed.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDiscoverFromLinuxSources(t *testing.T) {
	heroicCypress := mkdirGame(t, "heroic", "Cypress")
	heroicRedguard := mkdirGame(t, "heroic", "Redguard")
	lutrisGame := mkdirGame(t, "lutris", "Somer")

	heroicDirs := []string{
		writeHeroicEpicDir(t, "Cypress", "Cypress Tree", heroicCypress),
		writeHeroicEpicDir(t, "Redguard", "Redguard", heroicRedguard),
	}

	lutrisDBPath := lutristest.Create(t, lutristest.Row{
		Name: "Somer", Slug: "somer", Service: "egs", ServiceID: "Somer", Directory: lutrisGame, Installed: true,
	})

	games, err := NewWith(nil, heroicDirs, []string{lutrisDBPath}).Discover(context.Background())
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
	for _, id := range []string{"epic:Cypress", "epic:Redguard", "epic:Somer"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("missing game %s in %+v", id, byID)
		}
	}
}

func TestDiscoverPrefersManifestsOnDuplicateIDs(t *testing.T) {
	manifestInstall := mkdirGame(t, "native", "Cypress")
	heroicInstall := mkdirGame(t, "heroic", "CypressOther")

	manifests := writeManifests(t, map[string]string{
		"Cypress.item": withInstall(gameItem, manifestInstall),
	})
	heroicDir := writeHeroicEpicDir(t, "Cypress", "Cypress Tree", heroicInstall)

	games, err := NewWith([]string{manifests}, []string{heroicDir}, nil).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("Discover() returned %d games, want 1 for one game listed twice: %+v", len(games), games)
	}
	if games[0].Root != manifestInstall {
		t.Errorf("Root = %q, want the manifest install to win", games[0].Root)
	}
}

func TestDiscoverWithNothingInstalled(t *testing.T) {
	games, err := NewWith(nil, []string{t.TempDir()}, []string{filepath.Join(t.TempDir(), "pga.db")}).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if games != nil {
		t.Errorf("Discover() = %+v, want nil with nothing installed", games)
	}
}

func TestCorruptManifestIsAnErrorButNotAFailure(t *testing.T) {
	// scanManifests surfaces the corrupt file as an error alongside the
	// good game's entry, and Discover takes the entry.
	install := mkdirGame(t, "Epic Games", "Cypress")
	manifests := writeManifests(t, map[string]string{
		"Cypress.item": withInstall(gameItem, install),
		"Broken.item":  `{"DisplayName": `,
	})

	entries, err := scanManifests(manifests)
	if err == nil {
		t.Error("scanManifests() error = nil, want the corrupt manifest reported")
	}
	if len(entries) != 1 {
		t.Errorf("scanManifests() = %d entries, want the good game: %+v", len(entries), entries)
	}
}

func TestName(t *testing.T) {
	if got := (&Provider{}).Name(); got != "epic" {
		t.Errorf("Name() = %q, want epic", got)
	}
}

func withInstall(body, install string) string {
	return strings.Replace(body, "INSTALL", install, 1)
}
