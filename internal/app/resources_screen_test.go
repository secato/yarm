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
	"charm.land/lipgloss/v2"

	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/fetch"
	"github.com/secato/yarm/internal/state"
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

// resourcesTestDeps builds real Deps for the resources screen: a real
// *cache.Cache backed by a temp dir and pointed at an httptest server (so
// downloads actually happen, just not against the real internet), real
// WizardData from a fixed catalog, and a real (empty, or pre-populated)
// installs.json.
func resourcesTestDeps(t *testing.T) (Deps, *httptest.Server) {
	t.Helper()
	body := zipBytes(t, map[string]string{"repo-main/Shaders/A.fx": "// effect"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := cache.New(t.TempDir(), &fetch.Client{HTTP: srv.Client(), UserAgent: "yarm/test", Retries: 1, Backoff: time.Millisecond})
	c.Now = func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }

	data := WizardData{
		Versions: []catalog.Version{{Version: "6.8.0", Latest: true}, {Version: "6.7.3"}, {Version: "6.7.2"}, {Version: "6.7.1"}},
		Packages: []catalog.Package{{ID: "standard-effects", Name: "Standard effects", DownloadURL: srv.URL + "/pkg.zip"}},
		Addons:   []catalog.Addon{{ID: "swap-chain-override-by-crosire", Name: "Swap chain override", URL64: srv.URL + "/addon64", URL32: srv.URL + "/addon32"}},
	}

	return Deps{
		WizardData: fakeWizardData{data: data},
		Cache:      c,
		StateDir:   t.TempDir(),
	}, srv
}

// loadResourcesScreen drives Init to completion, exactly as the real loop
// would before the user can press anything.
func loadResourcesScreen(t *testing.T, deps Deps) *ResourcesScreen {
	t.Helper()
	s := NewResourcesScreen(deps)
	msg := s.Init()()
	next, _ := s.Update(msg, wizardEnv())
	return next.(*ResourcesScreen)
}

func TestResourcesScreenLoadsPanesFromCatalogAndCache(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	s := loadResourcesScreen(t, deps)

	if s.loading {
		t.Fatal("loading should be false once everything has arrived")
	}
	// The fixture's whole 4-version catalog fits under the top-10 cap.
	if got := len(s.panes[paneReShadeNormal]); got != 4 {
		t.Errorf("ReShade (normal) rows = %d, want 4", got)
	}
	if got := len(s.panes[paneReShadeAddon]); got != 4 {
		t.Errorf("ReShade (addon) rows = %d, want 4", got)
	}
	if got := len(s.panes[panePackages]); got != 1 {
		t.Fatalf("package rows = %d, want 1", got)
	}
	if s.panes[panePackages][0].Cached {
		t.Error("the package should not show cached before anything downloaded it")
	}
	if got := len(s.panes[paneAddons]); got != 1 {
		t.Fatalf("addon rows = %d, want 1", got)
	}
}

func TestResourcesScreenLoadFailureReportsError(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	deps.Cache.Root = "/this/path/does/not/exist/at/all"
	s := NewResourcesScreen(deps)

	msg := s.Init()()
	res, ok := msg.(resourcesLoadedMsg)
	if !ok || res.err == nil {
		t.Fatalf("message = %#v, want a resourcesLoadedMsg carrying an error", msg)
	}

	next, cmd := s.Update(msg, wizardEnv())
	s = next.(*ResourcesScreen)
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

// A cached version outside the top-3 catalog window must stay visible and
// deletable — otherwise it would become unmanageable through this screen.
func TestResourcesScreenKeepsOlderCachedVersionVisible(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	if _, err := deps.Cache.EnsureReShade(context.Background(), "6.5.0", false, nil); err != nil {
		t.Fatalf("EnsureReShade: %v", err)
	}
	s := loadResourcesScreen(t, deps)

	found := false
	for _, r := range s.panes[paneReShadeNormal] {
		if r.reshadeVersion == "6.5.0" {
			found = true
			if !r.Cached {
				t.Error("6.5.0 (normal) should show as cached")
			}
		}
	}
	if !found {
		t.Error("a cached version outside the top 3 should still be listed")
	}
}

// Pressing d on an item not yet cached fetches it for real, then reloads
// so it shows as cached afterward.
func TestResourcesScreenDownloadFetchesPackage(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	s := loadResourcesScreen(t, deps)
	s.focus = panePackages
	s.cursors[panePackages].setCursor(0)

	_, cmd := s.Update(tea.KeyPressMsg{Code: 'd', Text: "d"}, wizardEnv())
	if cmd == nil {
		t.Fatal("pressing d should return a command")
	}
	if !s.downloading {
		t.Error("downloading should be true while the fetch runs")
	}

	msg := cmd()
	if _, ok := msg.(resourcesLoadedMsg); !ok {
		t.Fatalf("message = %T, want resourcesLoadedMsg (a successful download reloads)", msg)
	}
	next, _ := s.Update(msg, wizardEnv())
	s = next.(*ResourcesScreen)

	if s.downloading {
		t.Error("downloading should clear once the fetch finishes")
	}
	if !s.panes[panePackages][0].Cached {
		t.Error("the package should show cached after downloading")
	}
}

// Downloading an add-on that only ships one architecture-neutral build
// must not duplicate the fetch per architecture.
func TestEnsureAddonAllArchesSkipsDuplicateForArchNeutral(t *testing.T) {
	body := zipBytes(t, map[string]string{"addon.addon64": "binary"})
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := cache.New(t.TempDir(), &fetch.Client{HTTP: srv.Client(), UserAgent: "yarm/test", Retries: 1, Backoff: time.Millisecond})
	c.Now = func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }

	addon := catalog.Addon{ID: "arch-neutral", Name: "Arch Neutral", URLAny: srv.URL + "/addon.addon64"}
	if addon.ArchSpecific() {
		t.Fatal("setup: expected an architecture-neutral addon")
	}

	if err := ensureAddonAllArches(c, addon); err != nil {
		t.Fatalf("ensureAddonAllArches: %v", err)
	}
	if calls != 1 {
		t.Errorf("download calls = %d, want exactly 1 for an arch-neutral add-on", calls)
	}
}

// The delete confirm flow: x opens a confirm overlay whose action, once
// run, actually removes the entry from disk and the cache index.
func TestResourcesScreenDeleteRemovesEntry(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	if _, err := deps.Cache.EnsureReShade(context.Background(), "6.8.0", false, nil); err != nil {
		t.Fatalf("EnsureReShade: %v", err)
	}
	s := loadResourcesScreen(t, deps)
	s.focus = paneReShadeNormal
	s.cursors[paneReShadeNormal].setCursor(0) // 6.8.0, the only cached (so sorted-first) row

	_, cmd := s.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}, wizardEnv())
	if cmd == nil {
		t.Fatal("'x' should return a command opening the confirm overlay")
	}
	overlayMsg, ok := cmd().(showOverlayMsg)
	if !ok {
		t.Fatalf("message = %T, want showOverlayMsg", cmd())
	}
	confirm, ok := overlayMsg.overlay.(confirmOverlay)
	if !ok {
		t.Fatalf("overlay = %T, want confirmOverlay", overlayMsg.overlay)
	}

	result := confirm.onYes()
	if _, ok := result.(resourcesLoadedMsg); !ok {
		t.Fatalf("confirmed delete produced %T, want resourcesLoadedMsg", result)
	}
	next, _ := s.Update(result, wizardEnv())
	s = next.(*ResourcesScreen)

	for _, r := range s.panes[paneReShadeNormal] {
		if r.reshadeVersion == "6.8.0" && r.Cached {
			t.Error("6.8.0 (normal) should no longer show as cached")
		}
	}
}

// Refresh only applies to package rows.
func TestResourcesScreenRefreshOnlyForPackages(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	s := loadResourcesScreen(t, deps)
	s.focus = paneReShadeNormal

	_, cmd := s.startRefresh()
	if cmd != nil {
		t.Error("refreshing a non-package pane should be a no-op")
	}
	if s.refreshing {
		t.Error("refreshing should not be set for a no-op refresh")
	}
}

func TestResourcesScreenRefreshPackage(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	if _, err := deps.Cache.EnsurePackage(context.Background(), deps.WizardData.(fakeWizardData).data.Packages[0], nil); err != nil {
		t.Fatalf("EnsurePackage: %v", err)
	}
	s := loadResourcesScreen(t, deps)
	s.focus = panePackages
	s.cursors[panePackages].setCursor(0)

	_, cmd := s.startRefresh()
	if cmd == nil {
		t.Fatal("refreshing a cached package should return a command")
	}
	if !s.refreshing {
		t.Error("refreshing should be true while the job runs")
	}

	msg := cmd()
	if _, ok := msg.(resourcesLoadedMsg); !ok {
		t.Fatalf("message = %T, want resourcesLoadedMsg", msg)
	}
	next, _ := s.Update(msg, wizardEnv())
	s = next.(*ResourcesScreen)
	if s.refreshing {
		t.Error("refreshing should clear once the job finishes")
	}
}

// A cached item referenced by any recorded install must show "in use", so
// deleting it warns the user first.
func TestResourcesScreenDetectsInUse(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	reg := state.Registry{Schema: state.SchemaVersion, Games: map[string]state.Game{}}
	reg.Record("steam:1", state.Game{Name: "X", Provider: "steam", Root: "/games/x"}, state.Install{
		Exe:      "x.exe",
		ReShade:  state.ReShadeInfo{Version: "6.8.0", Flavor: "normal"},
		Packages: []string{"standard-effects"},
	})
	if err := state.Save(deps.StateDir, reg); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	s := loadResourcesScreen(t, deps)

	foundReShade := false
	for _, r := range s.panes[paneReShadeNormal] {
		if r.reshadeVersion == "6.8.0" {
			foundReShade = true
			if !r.InUse {
				t.Error("6.8.0 (normal) should show as in use")
			}
		}
	}
	if !foundReShade {
		t.Fatal("setup: expected a 6.8.0 (normal) row")
	}
	if !s.panes[panePackages][0].InUse {
		t.Error("standard-effects should show as in use")
	}
}

// Left/right must move focus between panes, wrapping at both ends.
func TestResourcesScreenPaneFocusWraps(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	s := loadResourcesScreen(t, deps)

	if s.focus != paneReShadeNormal {
		t.Fatalf("initial focus = %v, want paneReShadeNormal", s.focus)
	}
	s.Update(tea.KeyPressMsg{Code: 'h', Text: "h"}, wizardEnv())
	if s.focus != paneAddons {
		t.Errorf("left from the first pane should wrap to the last, got %v", s.focus)
	}
	s.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}, wizardEnv())
	if s.focus != paneReShadeNormal {
		t.Errorf("right from the last pane should wrap to the first, got %v", s.focus)
	}
}

func TestScrollWindowKeepsCursorVisible(t *testing.T) {
	start, end := scrollWindow(20, 15, 5)
	if 15 < start || 15 >= end {
		t.Errorf("cursor 15 not within [%d, %d)", start, end)
	}
	if end-start != 5 {
		t.Errorf("window size = %d, want 5", end-start)
	}

	start, end = scrollWindow(3, 1, 10)
	if start != 0 || end != 3 {
		t.Errorf("a list shorter than the window should not be windowed, got [%d, %d)", start, end)
	}
}

func TestResourcesScreenRendersHeaderAndPanes(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	s := loadResourcesScreen(t, deps)

	body := s.View(Env{Styles: NewStyles(true), Width: 120, Height: 30})
	for _, want := range []string{"ReShade", "Shaders", "Add-ons", "total:"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

// At 60 columns — a `tmux split-window -h` on an 80-column terminal — four
// panes at their 20-column floor would need 80 columns and overflow. The
// screen must fold to a single, full-width pane with a tab strip instead
// of wrapping or overlapping.
func TestResourcesScreenFoldsToSinglePaneWhenNarrow(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	s := loadResourcesScreen(t, deps)

	body := s.View(Env{Styles: NewStyles(true), Width: 60, Height: 30})
	for _, line := range strings.Split(body, "\n") {
		if lipgloss.Width(line) > 60 {
			t.Errorf("line exceeds 60 columns (%d):\n%q", lipgloss.Width(line), line)
		}
	}
	// The tab strip must still name every pane, so the other three stay
	// reachable rather than disappearing.
	for _, want := range []string{"ReShade (normal)", "ReShade (addon)", "Shaders", "Add-ons"} {
		if !strings.Contains(body, want) {
			t.Errorf("narrow body missing pane label %q:\n%s", want, body)
		}
	}
}

// A row that has not been downloaded gets no status tag at all — the
// dimmed color already says "not downloaded"; repeating it in text was
// just noise. A cached, custom or in-use row still gets a tag, since
// those facts are not something the color alone conveys.
func TestRenderRowOmitsRedundantNotDownloadedTag(t *testing.T) {
	env := Env{Styles: NewStyles(true), Width: 100, Height: 30}

	notCached := resourceRow{Name: "SomePackage"}
	line := (&ResourcesScreen{}).renderRow(notCached, false, 40, env)
	if strings.Contains(line, "not downloaded") {
		t.Errorf("an uncached row should not print a redundant tag:\n%s", line)
	}

	cached := resourceRow{Name: "SomePackage", Cached: true, Size: 1024}
	line = (&ResourcesScreen{}).renderRow(cached, false, 40, env)
	if !strings.Contains(line, "1.0 KiB") {
		t.Errorf("a cached row should still show its size:\n%s", line)
	}
}

func TestSortCachedFirstPreservesOrderWithinGroups(t *testing.T) {
	rows := []resourceRow{
		{ID: "a", Cached: false},
		{ID: "b", Cached: true},
		{ID: "c", Cached: false},
		{ID: "d", Cached: true},
	}
	sortCachedFirst(rows)

	want := []string{"b", "d", "a", "c"}
	for i, id := range want {
		if rows[i].ID != id {
			t.Errorf("rows[%d].ID = %q, want %q (order = %v)", i, rows[i].ID, id, rowIDs(rows))
		}
	}
}

func rowIDs(rows []resourceRow) []string {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

// Loading must put cached (and custom) rows first in every pane.
func TestResourcesScreenSortsCachedItemsFirst(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	if _, err := deps.Cache.EnsureReShade(context.Background(), "6.7.2", true, nil); err != nil {
		t.Fatalf("EnsureReShade: %v", err)
	}
	s := loadResourcesScreen(t, deps)

	rows := s.panes[paneReShadeAddon]
	if !rows[0].Cached {
		t.Errorf("the first ReShade (addon) row should be the cached one, got %+v", rows[0])
	}
	if rows[0].reshadeVersion != "6.7.2" {
		t.Errorf("expected 6.7.2 first, got %s", rows[0].reshadeVersion)
	}
}
