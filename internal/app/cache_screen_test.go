package app

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/fetch"
)

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// newTestCacheWithPackage builds a real *cache.Cache (backed by a temp
// dir, pointed at an httptest server) holding one cached package entry —
// enough to exercise listing, sorting, deleting and refreshing without
// reaching the network.
func newTestCacheWithPackage(t *testing.T) (*cache.Cache, *httptest.Server) {
	t.Helper()

	body := zipBytes(t, map[string]string{
		"repo-main/Shaders/A.fx": "// effect v1",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := cache.New(t.TempDir(), &fetch.Client{HTTP: srv.Client(), UserAgent: "yarm/test", Retries: 1, Backoff: time.Millisecond})
	c.Now = func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }

	pkg := catalog.Package{ID: "standard-effects", Name: "Standard effects", DownloadURL: srv.URL + "/slim.zip"}
	if _, err := c.EnsurePackage(context.Background(), pkg, nil); err != nil {
		t.Fatalf("EnsurePackage(): %v", err)
	}
	return c, srv
}

// loadCacheScreen drives Init to completion, exactly as the real loop
// would before the user can press anything.
func loadCacheScreen(t *testing.T, c *cache.Cache) *CacheScreen {
	t.Helper()
	s := NewCacheScreen(c)
	msg := s.Init()()
	next, _ := s.Update(msg, wizardEnv())
	return next.(*CacheScreen)
}

func TestCacheScreenLoadsEntries(t *testing.T) {
	c, _ := newTestCacheWithPackage(t)
	s := loadCacheScreen(t, c)

	if s.loading {
		t.Fatal("loading should be false once the listing has arrived")
	}
	if len(s.entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(s.entries))
	}
	if s.entries[0].Name != "Standard effects" {
		t.Errorf("entry name = %q", s.entries[0].Name)
	}
	if s.total <= 0 {
		t.Error("total should reflect the cached package's real size")
	}
}

// A load failure (a corrupt index, here simulated by pointing at a path
// that cannot be listed) must not leave the screen stuck "loading…"
// forever.
func TestCacheScreenLoadFailureClearsLoading(t *testing.T) {
	c, _ := newTestCacheWithPackage(t)
	s := NewCacheScreen(c)

	// A cache.Cache whose Total() fails: point Root somewhere DirSize
	// cannot walk. Simplest reliable way: a path that does not exist.
	c.Root = "/this/path/does/not/exist/at/all"

	msg := s.Init()()
	res, ok := msg.(cacheResultMsg)
	if !ok || res.err == nil {
		t.Fatalf("message = %#v, want a cacheResultMsg carrying an error", msg)
	}

	next, cmd := s.Update(msg, wizardEnv())
	s = next.(*CacheScreen)

	if s.loading {
		t.Error("loading should be false even after a failed load")
	}
	if cmd == nil {
		t.Fatal("a load failure should still produce a command reporting it")
	}
	if _, ok := cmd().(errorMsg); !ok {
		t.Error("the follow-up command should report the error to the shell")
	}
}

// "s" cycles size -> date -> name -> back to size, per §6.1.
func TestCacheScreenSortCycle(t *testing.T) {
	c, _ := newTestCacheWithPackage(t)
	s := loadCacheScreen(t, c)

	order := []cache.SortBy{cache.SortBySize, cache.SortByDate, cache.SortByName, cache.SortBySize}
	if got := s.sortBy(); got != order[0] {
		t.Fatalf("initial sort = %q, want %q", got, order[0])
	}
	for i := 1; i < len(order); i++ {
		next, cmd := s.Update(cacheSortKey(), wizardEnv())
		s = next.(*CacheScreen)
		if cmd != nil {
			s2, _ := s.Update(cmd(), wizardEnv())
			s = s2.(*CacheScreen)
		}
		if got := s.sortBy(); got != order[i] {
			t.Errorf("after %d presses, sort = %q, want %q", i, got, order[i])
		}
	}
}

// The delete confirm flow: "d" opens a confirm overlay whose action, once
// run, actually removes the entry and its files.
func TestCacheScreenDeleteRemovesEntry(t *testing.T) {
	c, _ := newTestCacheWithPackage(t)
	s := loadCacheScreen(t, c)
	id := s.entries[0].ID

	_, cmd := s.Update(cacheDeleteKey(), wizardEnv())
	if cmd == nil {
		t.Fatal("'d' should return a command opening the confirm overlay")
	}
	msg := cmd()
	overlayMsg, ok := msg.(showOverlayMsg)
	if !ok {
		t.Fatalf("message = %T, want showOverlayMsg", msg)
	}
	confirm, ok := overlayMsg.overlay.(confirmOverlay)
	if !ok {
		t.Fatalf("overlay = %T, want confirmOverlay", overlayMsg.overlay)
	}

	// Run the confirmed action (what pressing "y" would trigger) and feed
	// its result back to the screen.
	result := confirm.onYes()
	next, _ := s.Update(result, wizardEnv())
	s = next.(*CacheScreen)

	if len(s.entries) != 0 {
		t.Errorf("entries after delete = %v, want none", s.entries)
	}
	if _, err := c.List(cache.SortByName, false); err != nil {
		t.Fatalf("List() after delete: %v", err)
	}
	entries, _ := c.List(cache.SortByName, false)
	for _, e := range entries {
		if e.ID == id {
			t.Error("the deleted entry is still in the cache index")
		}
	}
}

// Refresh only applies to package entries — trying it on anything else,
// or while one is already running, must be a no-op rather than a panic
// or a wrong download.
func TestCacheScreenRefreshOnlyForPackages(t *testing.T) {
	c, _ := newTestCacheWithPackage(t)
	s := loadCacheScreen(t, c)

	s.entries[0].Kind = cache.KindReShade // pretend it's not a package
	_, cmd := s.startRefresh()
	if cmd != nil {
		t.Error("refreshing a non-package entry should be a no-op")
	}
	if s.refreshing {
		t.Error("refreshing should not be set for a no-op refresh")
	}
}

func TestCacheScreenRefreshPackage(t *testing.T) {
	c, srv := newTestCacheWithPackage(t)
	s := loadCacheScreen(t, c)

	_, cmd := s.startRefresh()
	if cmd == nil {
		t.Fatal("refreshing a package entry should return a command")
	}
	if !s.refreshing {
		t.Error("refreshing should be true while the job runs")
	}

	msg := cmd()
	next, _ := s.Update(msg, wizardEnv())
	s = next.(*CacheScreen)

	if s.refreshing {
		t.Error("refreshing should clear once the job finishes")
	}
	if len(s.entries) != 1 {
		t.Fatalf("entries after refresh = %v, want exactly 1", s.entries)
	}
	_ = srv
}

// A refresh failure (a source that has vanished) must clear refreshing
// rather than leaving the header stuck saying "refreshing…" forever.
func TestCacheScreenRefreshFailureClearsFlag(t *testing.T) {
	c, srv := newTestCacheWithPackage(t)
	s := loadCacheScreen(t, c)
	srv.Close() // the package's source is now unreachable

	_, cmd := s.startRefresh()
	if cmd == nil {
		t.Fatal("want a command")
	}
	msg := cmd()
	res, ok := msg.(cacheResultMsg)
	if !ok || res.err == nil {
		t.Fatalf("message = %#v, want a cacheResultMsg carrying an error", msg)
	}

	next, followUp := s.Update(msg, wizardEnv())
	s = next.(*CacheScreen)
	if s.refreshing {
		t.Error("a failed refresh must still clear the refreshing flag")
	}
	if followUp == nil {
		t.Fatal("a refresh failure should still produce a command reporting it")
	}
}

func TestFreeSpaceHeaderRenders(t *testing.T) {
	c, _ := newTestCacheWithPackage(t)
	s := loadCacheScreen(t, c)
	s.resize(Env{Styles: NewStyles(true), Width: 100, Height: 30})

	body := s.View(Env{Styles: NewStyles(true), Width: 100, Height: 30})
	if s.freeErr == nil && !strings.Contains(body, "free:") {
		t.Errorf("the header should show free disk space:\n%s", body)
	}
}

func cacheSortKey() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: 's', Text: "s"} }
func cacheDeleteKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'd', Text: "d"} }
