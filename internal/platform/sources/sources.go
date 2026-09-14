// Package sources holds the shared entry type for launcher-metadata
// readers. Heroic, Lutris and Legendary manage Windows-game stores on
// platforms where those stores have no client of their own; the reader
// subpackages turn their metadata files into one common Entry so the
// store providers can consume them uniformly.
package sources

import (
	"os"
	"path/filepath"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/safetext"
)

// Store names, used both as Entry.Store and as the platform.Provider
// name of the store provider that consumes the entries — a gog entry
// becomes a game with Provider "gog" and ID "gog:<id>".
const (
	StoreGOG       = "gog"
	StoreEpic      = "epic"
	StoreBattleNet = "battlenet"
)

// Entry is one installed game as a launcher or store reported it.
type Entry struct {
	// Store is the store the game belongs to (StoreGOG, ...).
	Store string
	// StoreID is the store's own identifier for the game: GOG's numeric
	// product id, Epic's AppName, Battle.net's product uid. It is stable
	// across platforms and launchers, so the same game discovered via
	// the Windows registry, Heroic and Lutris is one game, not three.
	StoreID string
	// Title is the display name. Empty is allowed; Game falls back to
	// the install folder's name.
	Title string
	// Root is the absolute path to the game's install directory as the
	// source reported it — a host path even when the game lives inside
	// a wine prefix.
	Root string
}

// Game converts e into a game.Game for the provider named by e.Store,
// refusing anything that cannot safely become a game: an empty id, or
// a root that is not absolute or does not exist on disk (uninstalled
// games linger in several of these metadata files, and a root yarm
// cannot see is not one it can plan writes into). Names and ids out of
// files yarm does not own are stripped of control characters on the
// way in — the same rule the Steam provider applies to .acf fields.
func (e Entry) Game() (game.Game, bool) {
	if e.StoreID == "" || !filepath.IsAbs(e.Root) {
		return game.Game{}, false
	}
	if _, err := os.Stat(e.Root); err != nil {
		return game.Game{}, false
	}
	name := safetext.Clean(e.Title)
	if name == "" {
		name = filepath.Base(filepath.Clean(e.Root))
	}
	return game.Game{
		ID:       e.Store + ":" + safetext.Clean(e.StoreID),
		Name:     name,
		Provider: e.Store,
		Root:     filepath.Clean(e.Root),
	}, true
}

// OfStore returns only the entries belonging to store.
func OfStore(entries []Entry, store string) []Entry {
	var out []Entry
	for _, e := range entries {
		if e.Store == store {
			out = append(out, e)
		}
	}
	return out
}

// Games dedupes entries and converts each survivor into a game.Game,
// dropping any that Entry.Game refuses. It is the common tail of every
// store provider's Discover: gather Entry values from each source in
// priority order, then call Games once.
func Games(entries []Entry) []game.Game {
	var games []game.Game
	for _, e := range Dedupe(entries) {
		if g, ok := e.Game(); ok {
			games = append(games, g)
		}
	}
	return games
}

// Dedupe keeps only the first entry per store id. A game installed
// through two launchers is one game, not two rows that would both key
// installs.json by the same id; the fixed per-source order inside each
// provider decides which install wins.
func Dedupe(entries []Entry) []Entry {
	seen := make(map[string]bool, len(entries))
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		key := e.Store + ":" + e.StoreID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	return out
}
