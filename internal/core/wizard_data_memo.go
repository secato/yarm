package core

import (
	"context"
	"sync"
)

// Opening a game used to sit on "loading catalog data…" every single time,
// even with a warm disk cache, because LoadWizardData does real work
// on every call: it re-reads and re-parses three catalog files, rescans
// the custom-content directory, and — the expensive part — asks
// reshade.me which version is the latest, which is deliberately not cached
// on disk (a stale "latest" marker is worse than none).
//
// None of that changes while yarm is running, so it is worth doing exactly
// once. memoWizardData holds the first successful result for the rest of
// the session; the window starts that load at startup, alongside the game
// scan, so by the time a game is picked the answer is already in memory and
// its lists are filled in.
type memoWizardData struct {
	inner WizardDataLoader

	mu   sync.Mutex
	once *sync.Once
	data WizardData
	err  error
}

// Memoize returns a loader that calls inner at most once per successful
// load and hands every later caller the same data.
func Memoize(inner WizardDataLoader) WizardDataLoader {
	if inner == nil {
		return nil
	}
	return &memoWizardData{inner: inner, once: new(sync.Once)}
}

// LoadWizardData implements WizardDataLoader.
//
// Concurrent callers share one load rather than racing to make the same
// requests: the window's startup load and a game opened before it
// finishes are exactly that case. A failed load is not remembered — it is
// usually the network being briefly away, and the next caller that asks
// should get a fresh attempt rather than the old error.
func (m *memoWizardData) LoadWizardData(ctx context.Context) (WizardData, error) {
	m.mu.Lock()
	once := m.once
	m.mu.Unlock()

	once.Do(func() {
		data, err := m.inner.LoadWizardData(ctx)
		m.mu.Lock()
		defer m.mu.Unlock()
		m.data, m.err = data, err
		if err != nil {
			m.once = new(sync.Once) // let the next caller retry
		}
	})

	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data, m.err
}

// Reset drops the memoized result so the next caller loads fresh — the
// rescan path, after forcing the on-disk catalogs to refresh. A load
// already in flight keeps writing to the old once, which Reset replaces:
// either order leaves the memo holding one complete load, never a mix.
func (m *memoWizardData) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data, m.err = WizardData{}, nil
	m.once = new(sync.Once)
}
