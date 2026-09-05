package manual

import (
	"context"
	"testing"
)

func TestDiscover(t *testing.T) {
	p := New([]Entry{
		{Name: "My GOG game", Path: "/games/foo"},
		{Name: "Another one", Path: "/games/bar/"},
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
	if g.Name != "My GOG game" || g.Root != "/games/foo" || g.Provider != "manual" {
		t.Errorf("games[0] = %+v", g)
	}
	if g.ID != ID("/games/foo") {
		t.Errorf("games[0].ID = %q, want %q", g.ID, ID("/games/foo"))
	}

	// Trailing slash is cleaned, so the id is stable regardless of how the
	// path was typed.
	if games[1].Root != "/games/bar" {
		t.Errorf("games[1].Root = %q, want %q (cleaned)", games[1].Root, "/games/bar")
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
