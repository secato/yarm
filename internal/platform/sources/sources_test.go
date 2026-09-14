package sources

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEntryGame(t *testing.T) {
	root := t.TempDir()

	e := Entry{Store: StoreGOG, StoreID: "1207658924", Title: "Puzzle Agent", Root: root}
	g, ok := e.Game()
	if !ok {
		t.Fatal("Game() = not ok, want ok for an absolute existing root")
	}
	want := "gog:1207658924"
	if g.ID != want {
		t.Errorf("Game().ID = %q, want %q", g.ID, want)
	}
	if g.Provider != StoreGOG {
		t.Errorf("Game().Provider = %q, want %q", g.Provider, StoreGOG)
	}
	if g.Root != root {
		t.Errorf("Game().Root = %q, want %q", g.Root, root)
	}
}

func TestEntryGameRefusesUnusableRoots(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		name string
		e    Entry
	}{
		{"empty id", Entry{Store: StoreEpic, Root: root}},
		{"relative root", Entry{Store: StoreEpic, StoreID: "a", Root: "games/SomeGame"}},
		{"missing root", Entry{Store: StoreEpic, StoreID: "a", Root: filepath.Join(root, "missing")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := tc.e.Game(); ok {
				t.Errorf("Game() = ok, want refused for %s", tc.name)
			}
		})
	}
}

func TestEntryGameCleansUntrustedText(t *testing.T) {
	root := t.TempDir()

	e := Entry{Store: StoreGOG, StoreID: "1\x1b2", Title: "Game\x1b[2mName", Root: root}
	g, ok := e.Game()
	if !ok {
		t.Fatal("Game() = not ok, want ok")
	}
	if g.ID != "gog:12" {
		t.Errorf("ID = %q, want control characters stripped", g.ID)
	}
	if g.Name != "Game[2mName" {
		t.Errorf("Name = %q, want escape sequence stripped", g.Name)
	}
}

func TestEntryGameTitleFallsBackToFolderName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Some Game Name")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	g, ok := Entry{Store: StoreEpic, StoreID: "a", Root: root}.Game()
	if !ok {
		t.Fatal("Game() = not ok, want ok")
	}
	if g.Name != "Some Game Name" {
		t.Errorf("Name = %q, want the folder's name", g.Name)
	}
}

func TestOfStore(t *testing.T) {
	entries := []Entry{
		{Store: StoreGOG, StoreID: "1"},
		{Store: StoreEpic, StoreID: "2"},
		{Store: StoreGOG, StoreID: "3"},
	}
	got := OfStore(entries, StoreGOG)
	if len(got) != 2 || got[0].StoreID != "1" || got[1].StoreID != "3" {
		t.Errorf("OfStore(gog) = %+v, want the two gog entries", got)
	}
	if got := OfStore(entries, StoreBattleNet); got != nil {
		t.Errorf("OfStore(battlenet) = %+v, want nil", got)
	}
}

func TestDedupeKeepsFirstPerStoreID(t *testing.T) {
	entries := []Entry{
		{Store: StoreGOG, StoreID: "1", Root: "/first"},
		{Store: StoreEpic, StoreID: "1", Root: "/other-store-same-id"},
		{Store: StoreGOG, StoreID: "1", Root: "/second"},
		{Store: StoreGOG, StoreID: "2", Root: "/x"},
	}
	got := Dedupe(entries)
	if len(got) != 3 {
		t.Fatalf("Dedupe() returned %d entries, want 3: %+v", len(got), got)
	}
	if got[0].Root != "/first" || got[1].Root != "/other-store-same-id" || got[2].Root != "/x" {
		t.Errorf("Dedupe() = %+v, want first-seen entries kept in order", got)
	}
}
