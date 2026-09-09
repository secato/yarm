package manual

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDiscover(t *testing.T) {
	// Built with filepath, because the provider stores filepath.Clean of
	// what it was given: on Windows "/games/foo" cleans to "\games\foo",
	// and the id is derived from that cleaned form. A hardcoded POSIX
	// fixture asserts the wrong thing there.
	sep := string(filepath.Separator)
	foo := filepath.Join(sep, "games", "foo")
	bar := filepath.Join(sep, "games", "bar")

	p := New([]Entry{
		{Name: "My GOG game", Path: foo},
		{Name: "Another one", Path: bar + sep}, // trailing separator
	})

	if got := p.Name(); got != "manual" {
		t.Errorf("Name() = %q, want %q", got, "manual")
	}

	games, err := p.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(games) != 2 {
		t.Fatalf("Discover() returned %d games, want 2", len(games))
	}

	g := games[0]
	if g.Name != "My GOG game" || g.Root != foo || g.Provider != "manual" {
		t.Errorf("games[0] = %+v, want Root %q", g, foo)
	}
	if g.ID != ID(foo) {
		t.Errorf("games[0].ID = %q, want %q", g.ID, ID(foo))
	}

	// Trailing separator is cleaned, so the id is stable regardless of how
	// the path was typed.
	if games[1].Root != bar {
		t.Errorf("games[1].Root = %q, want %q (cleaned)", games[1].Root, bar)
	}
	if games[1].ID != ID(bar) {
		t.Errorf("games[1].ID = %q, want the id of the cleaned path %q", games[1].ID, bar)
	}
}

func TestIDIsStableAndUnique(t *testing.T) {
	a := ID("/games/foo")
	b := ID("/games/foo")
	c := ID("/games/bar")

	if a != b {
		t.Errorf("ID() is not stable: %q != %q", a, b)
	}
	if a == c {
		t.Errorf("ID() collided for different paths: %q", a)
	}
	if len(a) != len("manual:")+12 {
		t.Errorf("ID() = %q, want manual: + 12 hex chars", a)
	}
}
