package app

import (
	"context"
	"sync"
)

// Opening the wizard used to sit on "loading catalog data…" every single
// time, even with a warm disk cache, because LoadWizardData does real work
// on every call: it re-reads and re-parses three catalog files, rescans
// the custom-content directory, and — the expensive part — asks
// reshade.me which version is the latest, which is deliberately not cached
// on disk (a stale "latest" marker is worse than none).
//
// None of that changes while yarm is running, so it is worth doing exactly
// once. memoWizardData holds the first successful result for the rest of
// the session; PreloadWizardData starts that load at startup, alongside
// the game scan, so by the time a game is picked the answer is already in
// memory and the wizard opens with its lists filled in.
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
// requests: the games screen's preload and a wizard opened before it
// finishes are exactly that case. A failed load is not remembered — it is
// usually the network being briefly away, and the next screen that asks
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

// preloadedWizardData is a loader that can answer without doing any work,
// because it has already loaded. The wizard uses it to skip its loading
// state entirely; a loader that cannot do this simply does not implement
// it, and the wizard loads as before.
type preloadedWizardData interface {
	loaded() (WizardData, bool)
}

// loaded implements preloadedWizardData.
func (m *memoWizardData) loaded() (WizardData, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data, m.err == nil && !m.data.empty()
}
