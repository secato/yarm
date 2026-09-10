package app

import (
	"sync"
	"time"
)

// gamesCacheTTL is how long game discovery is trusted in memory. Game
// folders change outside yarm — a Steam install or uninstall lands
// whenever — so this is minutes, not forever: long enough that poking
// around the wizard never rescans, short enough that a missing game
// appears on its own. Anything sooner is one R away, and a restart
// always starts cold.
const gamesCacheTTL = 10 * time.Minute

// GamesCache memoizes game discovery for gamesCacheTTL. In memory only:
// restarting the app rescans, and the rescan key refills it. Shared by
// pointer — ProviderLoader is passed around by value, and every copy
// must see the same entries. The zero value is usable.
type GamesCache struct {
	mu      sync.Mutex
	entries []GameEntry
	err     error
	at      time.Time
	// Now is overridable for tests.
	Now func() time.Time
}

// NewGamesCache returns an empty game cache.
func NewGamesCache() *GamesCache { return &GamesCache{} }

// Get returns the cached entries when they are still fresh. The slice is
// a copy: screens never mutate entries in place, and sharing the backing
// array with a future Set would make that a load-bearing assumption.
func (c *GamesCache) Get() ([]GameEntry, error, bool) {
	if c == nil {
		return nil, nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil && c.err == nil {
		return nil, nil, false
	}
	if c.now().Sub(c.at) >= gamesCacheTTL {
		return nil, nil, false
	}
	return append([]GameEntry(nil), c.entries...), c.err, true
}

// Set stores a discovery result. Errors are stored too: a failed scan
// reads back as the same failure until it expires or something clears
// it, rather than retrying on every screen load.
func (c *GamesCache) Set(entries []GameEntry, err error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries, c.err, c.at = entries, err, c.now()
}

// Clear drops whatever is cached. Nil-safe, so callers that only
// sometimes have a cache never have to check.
func (c *GamesCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries, c.err = nil, nil
	c.at = time.Time{}
}

func (c *GamesCache) now() time.Time {
	if c == nil || c.Now == nil {
		return time.Now()
	}
	return c.Now()
}
