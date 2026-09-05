package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/secato/yarm/internal/game"
)

type fakeProvider struct {
	name  string
	games []game.Game
	err   error
}

func (f fakeProvider) Name() string { return f.name }

func (f fakeProvider) Discover(context.Context) ([]game.Game, error) {
	return f.games, f.err
}

func TestDiscoverAll(t *testing.T) {
	boom := errors.New("boom")
	providers := []Provider{
		fakeProvider{name: "a", games: []game.Game{{ID: "a:1"}, {ID: "a:2"}}},
		fakeProvider{name: "b", err: boom},
		fakeProvider{name: "c", games: []game.Game{{ID: "c:1"}}},
	}

	games, err := DiscoverAll(context.Background(), providers)

	if !errors.Is(err, boom) {
		t.Errorf("DiscoverAll() error = %v, want it to wrap %v", err, boom)
	}
	if len(games) != 3 {
		t.Fatalf("DiscoverAll() returned %d games, want 3 (errors from one provider shouldn't drop the others): %+v", len(games), games)
	}
}

func TestDiscoverAllNoErrors(t *testing.T) {
	providers := []Provider{
		fakeProvider{name: "a", games: []game.Game{{ID: "a:1"}}},
	}

	games, err := DiscoverAll(context.Background(), providers)
	if err != nil {
		t.Fatalf("DiscoverAll() error = %v, want nil", err)
	}
	if len(games) != 1 {
		t.Fatalf("DiscoverAll() = %+v, want 1 game", games)
	}
}
