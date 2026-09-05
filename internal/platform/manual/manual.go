// Package manual implements a platform.Provider for user-added game
// folders (config.yaml's manual_games).
package manual

import (
	"context"
	"crypto/sha1" //nolint:gosec // used only to derive a short, stable id, not for security
	"encoding/hex"
	"path/filepath"

	"github.com/secato/yarm/internal/game"
)

// ProviderName identifies this provider in game.Game.Provider and IDs.
const ProviderName = "manual"

// Entry is one user-added game folder. It mirrors config.ManualGame without
// this package depending on the config package.
type Entry struct {
	Name string
	Path string
}

// Provider discovers games from a fixed list of user-added folders.
type Provider struct {
	entries []Entry
}

// New returns a Provider over entries.
func New(entries []Entry) *Provider {
	return &Provider{entries: entries}
}

// Name implements platform.Provider.
func (p *Provider) Name() string { return ProviderName }

// Discover implements platform.Provider. It never errors: entries are
// config-provided, and a folder that no longer exists is still returned (so
// the UI can surface it as missing) rather than silently dropped.
func (p *Provider) Discover(_ context.Context) ([]game.Game, error) {
	games := make([]game.Game, 0, len(p.entries))
	for _, e := range p.entries {
		root := filepath.Clean(e.Path)
		games = append(games, game.Game{
			ID:       ID(root),
			Name:     e.Name,
			Provider: ProviderName,
			Root:     root,
		})
	}
	return games, nil
}

// ID derives the stable "manual:<sha1(root)[:12]>" game id documented in
// docs/plan/03-data-and-storage.md §3.3.
func ID(root string) string {
	sum := sha1.Sum([]byte(root)) //nolint:gosec // id derivation, not a security use of the hash
	return ProviderName + ":" + hex.EncodeToString(sum[:])[:12]
}
