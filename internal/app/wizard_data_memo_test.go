package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// countingLoader records how often it was asked to load.
type countingLoader struct {
	calls atomic.Int64
	data  WizardData
	err   error
}

func (l *countingLoader) LoadWizardData(context.Context) (WizardData, error) {
	l.calls.Add(1)
	return l.data, l.err
}

// Opening the wizard twice used to mean two full catalog loads — three
// file parses, a directory scan, and an uncached request to reshade.me for
// the "latest" marker — for data that cannot change while yarm runs.
func TestCatalogIsLoadedOncePerSession(t *testing.T) {
	inner := &countingLoader{data: sampleWizardData()}
	loader := Memoize(inner)

	for range 5 {
		data, err := loader.LoadWizardData(context.Background())
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if len(data.Packages) != len(inner.data.Packages) {
			t.Fatalf("got %d packages, want %d", len(data.Packages), len(inner.data.Packages))
		}
	}
	if got := inner.calls.Load(); got != 1 {
		t.Errorf("the catalog was loaded %d times, want once", got)
	}
}

// The games screen's preload and a wizard opened before it finishes are
// the same load, not two racing ones.
func TestConcurrentLoadsShareOneFetch(t *testing.T) {
	inner := &countingLoader{data: sampleWizardData()}
	loader := Memoize(inner)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := loader.LoadWizardData(context.Background()); err != nil {
				t.Errorf("load: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := inner.calls.Load(); got != 1 {
		t.Errorf("the catalog was loaded %d times, want once", got)
	}
}

// A failure is usually the network being briefly away, so it must not be
// remembered: the next screen to ask gets a fresh attempt.
func TestAFailedLoadIsRetried(t *testing.T) {
	inner := &countingLoader{err: errors.New("no network")}
	loader := Memoize(inner)

	if _, err := loader.LoadWizardData(context.Background()); err == nil {
		t.Fatal("want the load error")
	}
	if _, ok := loader.(preloadedWizardData).loaded(); ok {
		t.Error("a failed load should not count as preloaded")
	}

	inner.err, inner.data = nil, sampleWizardData()
	if _, err := loader.LoadWizardData(context.Background()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := inner.calls.Load(); got != 2 {
		t.Errorf("the catalog was loaded %d times, want a retry after the failure", got)
	}
}

// A load that produced nothing at all is not something to hand the wizard
// as if it were an answer.
func TestAnEmptyLoadIsNotTreatedAsPreloaded(t *testing.T) {
	loader := Memoize(&countingLoader{data: WizardData{}})
	if _, err := loader.LoadWizardData(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := loader.(preloadedWizardData).loaded(); ok {
		t.Error("an empty catalog should not count as preloaded")
	}
}

// The games screen warms the catalog at startup, alongside the game scan,
// so picking a game does not start the wait.
func TestGamesScreenPreloadsTheCatalog(t *testing.T) {
	inner := &countingLoader{data: sampleWizardData()}
	deps := Deps{WizardData: Memoize(inner)}
	s := NewGamesScreen(fakeLoader{entries: sampleEntries()}, deps, false)

	cmd := s.Init()
	if cmd == nil {
		t.Fatal("Init returned no command")
	}
	drainCmd(cmd)

	if got := inner.calls.Load(); got != 1 {
		t.Errorf("the catalog was loaded %d times during startup, want once", got)
	}
}

// With the catalog already in memory, the wizard must open showing its
// lists — not a frame of "loading catalog data…" for data nobody is
// waiting on.
func TestWizardOpensWithoutLoadingWhenTheCatalogIsWarm(t *testing.T) {
	loader := Memoize(&countingLoader{data: sampleWizardData()})
	if _, err := loader.LoadWizardData(context.Background()); err != nil {
		t.Fatalf("preload: %v", err)
	}

	entry := sampleEntries()[0]
	w := NewWizardScreen(entry, entry.Exes[0], singleGroup(entry.Exes[0]), Deps{WizardData: loader})
	w.Init()

	if w.loading {
		t.Error("the wizard is still in its loading state with a warm catalog")
	}
	env := Env{Styles: NewStyles(true), Width: 100, Height: 30}
	if body := w.View(env); strings.Contains(body, "loading catalog data") {
		t.Errorf("the wizard painted its loading state:\n%s", body)
	}
	if len(w.data.Versions) == 0 {
		t.Error("the wizard has no versions, so the warm data never reached it")
	}
}

// drainCmd runs a command (and any batched children) for its side
// effects, which is all the preload has.
func drainCmd(cmd tea.Cmd) {
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var wg sync.WaitGroup
		for _, c := range batch {
			wg.Add(1)
			go func() { defer wg.Done(); c() }()
		}
		wg.Wait()
	}
}
