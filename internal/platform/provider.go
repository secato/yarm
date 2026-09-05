// Package platform discovers installed games via pluggable providers
// (Steam, manually added folders, and — later — other launchers).
package platform

import (
	"context"
	"errors"

	"github.com/secato/yarm/internal/game"
)

// Provider discovers games from one source (a launcher, or user-added
// folders).
type Provider interface {
	// Name identifies the provider ("steam", "manual", ...).
	Name() string
	// Discover returns every game the provider can find. A provider that
	// finds nothing (e.g. Steam not installed) returns an empty slice, not
	// an error.
	Discover(ctx context.Context) ([]game.Game, error)
}

// DiscoverAll runs every provider and concatenates their results. An error
// from one provider does not stop the others; all errors are joined.
func DiscoverAll(ctx context.Context, providers []Provider) ([]game.Game, error) {
	var (
		games []game.Game
		errs  []error
	)
	for _, p := range providers {
		found, err := p.Discover(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		games = append(games, found...)
	}
	return games, errors.Join(errs...)
}
