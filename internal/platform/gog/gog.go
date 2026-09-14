// Package gog implements a platform.Provider that discovers games from
// GOG. On Windows, every GOG installer — Galaxy or the offline kind —
// writes a per-game registry key, so the registry is the source there.
// On Linux, where GOG has no client at all, the games are managed by
// launchers, so the sources are Heroic and Lutris.
package gog

import (
	"context"
	"log/slog"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/platform/sources"
	"github.com/secato/yarm/internal/platform/sources/heroic"
	"github.com/secato/yarm/internal/platform/sources/lutris"
)

// ProviderName identifies this provider in game.Game.Provider and IDs.
const ProviderName = sources.StoreGOG

// Provider discovers GOG games.
type Provider struct {
	// reg enumerates the per-game registry keys; nil where there is no
	// registry to read (every OS but Windows).
	reg registrySource
	// heroicDirs are Heroic config directories to read.
	heroicDirs []string
	// lutrisDBs are Lutris pga.db paths to read.
	lutrisDBs []string
}

// New returns a Provider using this OS's defaults: the registry where
// one exists, and the standard Heroic and Lutris locations.
func New() *Provider {
	return NewWith(defaultRegistry(), heroic.DefaultConfigDirs(), lutris.DefaultDBPaths())
}

// NewWith reads exactly the sources given. Exported for tests, which
// point it at fixture directories and fake registries; production code
// should use New.
func NewWith(reg registrySource, heroicDirs, lutrisDBs []string) *Provider {
	return &Provider{reg: reg, heroicDirs: heroicDirs, lutrisDBs: lutrisDBs}
}

// Name implements platform.Provider.
func (p *Provider) Name() string { return ProviderName }

// Discover implements platform.Provider. A source that is not present
// (GOG not installed, Heroic not installed, and so on) is the normal
// case on any given machine and yields nothing; a source that is
// present but unreadable is logged and skipped rather than failing the
// whole discovery, the same deal the Steam provider gives its roots.
func (p *Provider) Discover(_ context.Context) ([]game.Game, error) {
	// Order is deliberate: the native source is authoritative, and
	// Dedupe keeps the first entry per game id, so a game installed
	// both ways lists the registry's install.
	var entries []sources.Entry

	if p.reg != nil {
		found, err := p.reg.games()
		if err != nil {
			slog.Debug("gog: registry unreadable", "error", err)
		}
		for _, g := range found {
			// A subkey with dependsOn is DLC installed under the base
			// game's folder, not a second game.
			if g.dependsOn != "" {
				continue
			}
			entries = append(entries, sources.Entry{
				Store:   ProviderName,
				StoreID: g.id,
				Title:   g.name,
				Root:    g.path,
			})
		}
	}

	for _, dir := range p.heroicDirs {
		found, err := heroic.Read(dir)
		entries = append(entries, sources.OfStore(found, ProviderName)...)
		if err != nil {
			slog.Debug("gog: heroic unreadable", "dir", dir, "error", err)
		}
	}

	for _, db := range p.lutrisDBs {
		found, err := lutris.Games(db)
		entries = append(entries, sources.OfStore(found, ProviderName)...)
		if err != nil {
			slog.Debug("gog: lutris unreadable", "db", db, "error", err)
		}
	}

	return sources.Games(entries), nil
}

// registrySource enumerates GOG's per-game registry keys. An interface
// so the discovery logic is testable without Windows; the real
// implementation is registry_windows.go.
type registrySource interface {
	games() ([]gogGame, error)
}

// gogGame is one per-game registry subkey's values.
type gogGame struct {
	id        string
	name      string
	path      string
	dependsOn string
}
