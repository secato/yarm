package app

import (
	"sync"
)

// GamesCache memoizes game discovery for the life of the process. The
// list costs on the order of 2 KB per game (~2 MB for a thousand), so
// there is no reason to expire it: navigating the UI never rescans the
// disk, a restart always starts cold, and the rescan key refills it.
// Shared by pointer — ProviderLoader is passed around by value, and every
// copy must see the same entries. The zero value is usable.
type GamesCache struct {
	mu      sync.Mutex
	entries []GameEntry
	err     error
}

// NewGamesCache returns an empty game cache.
func NewGamesCache() *GamesCache { return &GamesCache{} }

// Get returns the cached entries once discovery has run. The slice is
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
	c.entries, c.err = entries, err
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
}
