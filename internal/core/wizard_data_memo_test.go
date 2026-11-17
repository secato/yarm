package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
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

	inner.err, inner.data = nil, sampleWizardData()
	if _, err := loader.LoadWizardData(context.Background()); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := inner.calls.Load(); got != 2 {
		t.Errorf("the catalog was loaded %d times, want a retry after the failure", got)
	}
}

// Reset drops the memoized load so the next caller fetches fresh — the
// rescan path, after forcing the on-disk catalogs to refresh.
func TestMemoResetReloads(t *testing.T) {
	inner := &countingLoader{data: sampleWizardData()}
	loader := Memoize(inner)

	if _, err := loader.LoadWizardData(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	r, ok := loader.(interface{ Reset() })
	if !ok {
		t.Fatal("the memoized loader should be resettable")
	}
	r.Reset()
	if _, err := loader.LoadWizardData(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := inner.calls.Load(); got != 2 {
		t.Errorf("the catalog was loaded %d times, want twice", got)
	}
}
