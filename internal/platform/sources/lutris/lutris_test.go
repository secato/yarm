package lutris

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/secato/yarm/internal/platform/sources/lutris/lutristest"
)

func TestGames(t *testing.T) {
	path := lutristest.Create(t,
		lutristest.Row{Name: "Puzzle Agent", Slug: "puzzle-agent", Service: "gog", ServiceID: "1207658924", Directory: "/games/gog/PuzzleAgent", Installed: true},
		lutristest.Row{Name: "Cypress Tree", Slug: "cypress-tree", Service: "egs", ServiceID: "Cypress", Directory: "/games/epic/Cypress", Installed: true},
		lutristest.Row{Name: "Uninstalled GOG", Slug: "gone", Service: "gog", ServiceID: "42", Directory: "/games/gog/Gone", Installed: false},
		lutristest.Row{Name: "No directory", Slug: "empty", Service: "gog", ServiceID: "43", Directory: "", Installed: true},
		lutristest.Row{Name: "No service id", Slug: "nosid", Service: "gog", ServiceID: "", Directory: "/games/gog/NoSid", Installed: true},
		lutristest.Row{Name: "Other store", Slug: "humble", Service: "humblebundle", ServiceID: "99", Directory: "/games/humble/x", Installed: true},
	)

	entries, err := Games(path)
	if err != nil {
		t.Fatalf("Games() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Games() returned %d entries, want 2: %+v", len(entries), entries)
	}
	if entries[0].Store != "gog" || entries[0].StoreID != "1207658924" || entries[0].Title != "Puzzle Agent" || entries[0].Root != "/games/gog/PuzzleAgent" {
		t.Errorf("gog entry = %+v", entries[0])
	}
	if entries[1].Store != "epic" || entries[1].StoreID != "Cypress" {
		t.Errorf("egs should be reported as the epic store: %+v", entries[1])
	}
}

func TestGamesMissingDBIsNotAnError(t *testing.T) {
	entries, err := Games(filepath.Join(t.TempDir(), "pga.db"))
	if err != nil {
		t.Fatalf("Games() error = %v, want nil for a missing database", err)
	}
	if entries != nil {
		t.Errorf("Games() = %+v, want nil", entries)
	}
}

func TestGamesNotADatabaseIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pga.db")
	if err := os.WriteFile(path, []byte("definitely not sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Games(path); err == nil {
		t.Error("Games() error = nil, want an error for a file that is not a database")
	}
}

func TestBattleNetPrefixes(t *testing.T) {
	path := lutristest.Create(t,
		lutristest.Row{Name: "Battle.net", Slug: "battlenet", Directory: "/home/user/Games/battle.net/drive_c/Program Files (x86)/Battle.net", Installed: true},
		lutristest.Row{Name: "Battle.net again", Slug: "battle.net", Directory: "/home/user/Games/battle.net/drive_c/Program Files (x86)/Battle.net", Installed: true},
		lutristest.Row{Name: "Battle.net client gone", Slug: "battlenet", Directory: "/home/user/Games/old/drive_c/Program Files (x86)/Battle.net", Installed: false},
		lutristest.Row{Name: "No prefix", Slug: "battlenet", Directory: "/home/user/Games/flat", Installed: true},
	)

	prefixes, err := BattleNetPrefixes(path)
	if err != nil {
		t.Fatalf("BattleNetPrefixes() error = %v", err)
	}
	if len(prefixes) != 1 || prefixes[0] != "/home/user/Games/battle.net" {
		t.Errorf("BattleNetPrefixes() = %+v, want the one installed client's prefix, deduped", prefixes)
	}
}

func TestPrefixOf(t *testing.T) {
	cases := []struct {
		directory string
		prefix    string
		ok        bool
	}{
		{"/home/u/Games/bn/drive_c/Program Files (x86)/Battle.net", "/home/u/Games/bn", true},
		{"/home/u/Games/bn/drive_c", "/home/u/Games/bn", true},
		{"drive_c/Program Files", "", false},
		{"/home/u/Games/flat", "", false},
	}
	for _, tc := range cases {
		got, ok := prefixOf(tc.directory)
		if got != tc.prefix || ok != tc.ok {
			t.Errorf("prefixOf(%q) = (%q, %v), want (%q, %v)", tc.directory, got, ok, tc.prefix, tc.ok)
		}
	}
}

func TestDSNEscapesPathCharacters(t *testing.T) {
	// A path with a space and a '?' must survive the URI round trip;
	// Games on it then proves the driver opens what dsn built.
	path := filepath.Join(t.TempDir(), "lutris data?dir", "pga.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE games (id INTEGER PRIMARY KEY, name TEXT, sortname TEXT, slug TEXT, installer_slug TEXT, parent_slug TEXT, platform TEXT, runner TEXT, executable TEXT, directory TEXT, updated DATETIME, lastplayed INTEGER, installed INTEGER, installed_at INTEGER, year INTEGER, configpath TEXT, has_custom_banner INTEGER, has_custom_icon INTEGER, has_custom_coverart_big INTEGER, playtime REAL, service TEXT, service_id TEXT, discord_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	if _, err := Games(path); err != nil {
		t.Errorf("Games() error = %v, want the escaped URI to open the db", err)
	}
}

func TestDefaultDBPaths(t *testing.T) {
	for _, p := range DefaultDBPaths() {
		if !filepath.IsAbs(p) {
			t.Errorf("DefaultDBPaths() = %q, want absolute paths", p)
		}
	}
}
