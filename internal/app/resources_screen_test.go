package app

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/fetch"
	"github.com/secato/yarm/internal/game"
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
	reg.Record("steam:1", state.Game{Name: "X", Provider: "steam", Root: testRoot("/games/x")}, state.Install{
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
	s.Update(tea.KeyPressMsg{Code: tea.KeyLeft}, wizardEnv())
	if s.focus != paneAddons {
		t.Errorf("left from the first pane should wrap to the last, got %v", s.focus)
	}
	s.Update(tea.KeyPressMsg{Code: tea.KeyRight}, wizardEnv())
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

// The row colors carry the state the caption names: green for cached,
// blue for in use (overriding green — an install can reference content
// whose cache entry is gone), dim for not downloaded, accent for custom.
func TestRenderRowColorsFollowTheLegend(t *testing.T) {
	env := Env{Styles: NewStyles(true), Width: 100, Height: 30}
	s := &ResourcesScreen{}

	open := func(s lipgloss.Style) string {
		r := s.Render("x")
		return r[:strings.Index(r, "x")]
	}
	cachedLine := s.renderRow(resourceRow{Name: "P", Cached: true}, false, 40, env)
	if !strings.Contains(cachedLine, open(env.Styles.Good)) {
		t.Errorf("a cached row should render green:\n%s", cachedLine)
	}
	inUse := s.renderRow(resourceRow{Name: "P", Cached: true, InUse: true}, false, 40, env)
	if !strings.Contains(inUse, open(env.Styles.Info)) {
		t.Errorf("an in-use row should render blue:\n%s", inUse)
	}
	// In use wins over cached, and applies even without a cache entry —
	// the install's files are the evidence that matters.
	inUseOnly := s.renderRow(resourceRow{Name: "P", InUse: true}, false, 40, env)
	if !strings.Contains(inUseOnly, open(env.Styles.Info)) {
		t.Errorf("an in-use row without a cache entry should still render blue:\n%s", inUseOnly)
	}
	plain := s.renderRow(resourceRow{Name: "P"}, false, 40, env)
	if !strings.Contains(plain, open(env.Styles.Faint)) {
		t.Errorf("a not-downloaded row should render dim:\n%s", plain)
	}
}

// The caption names what the colors mean, once, above the panes.
func TestResourcesScreenShowsTheLegend(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	s := NewResourcesScreen(deps)
	drainCmd(s.Init())
	s.loading = false // the legend renders regardless of load state
	if s.loadErr != "" && s.total == 0 {
		s.loadErr = ""
	}

	body := s.View(Env{Styles: NewStyles(true), Width: 120, Height: 30})
	for _, want := range []string{"green downloaded", "blue in use", "not downloaded"} {
		if !strings.Contains(body, want) {
			t.Errorf("the legend should say %q:\n%s", want, body)
		}
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

// withPackages returns deps whose catalog also holds the given packages.
func withPackages(t *testing.T, deps Deps, pkgs ...catalog.Package) Deps {
	t.Helper()
	d := deps.WizardData.(fakeWizardData).data
	d.Packages = append(d.Packages, pkgs...)
	deps.WizardData = fakeWizardData{data: d}
	return deps
}

// The browser shows the same curated shortlist the wizard does — 43
// packages is a catalog, not a list anybody reads — with `a` widening it.
func TestResourcesScreenShowsCuratedShortlistUntilShowAll(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	deps = withPackages(t, deps,
		catalog.Package{ID: "sweetfx-by-ceejay-dk", Name: "SweetFX by CeeJay.dk"},
		catalog.Package{ID: "crt-royale-reshade-by-akgunter", Name: "CRT-Royale-ReShade by akgunter"},
	)
	s := loadResourcesScreen(t, deps)

	names := visibleNames(s, panePackages)
	if slices.Contains(names, "CRT-Royale-ReShade by akgunter") {
		t.Errorf("an off-shortlist package should be hidden until 'a': %v", names)
	}
	if !slices.Contains(names, "SweetFX by CeeJay.dk") {
		t.Errorf("a shortlisted package should be shown: %v", names)
	}

	body := s.View(wizardEnv())
	if !strings.Contains(body, "of 3") {
		t.Errorf("a filtered pane should say how much it is hiding:\n%s", body)
	}

	next, _ := s.Update(tea.KeyPressMsg{Code: 'a', Text: "a"}, wizardEnv())
	s = next.(*ResourcesScreen)
	if !slices.Contains(visibleNames(s, panePackages), "CRT-Royale-ReShade by akgunter") {
		t.Error("'a' should reveal the whole catalog")
	}
}

// Something already downloaded must stay listed however obscure, or the
// browser could not show — or delete — what is actually on disk.
func TestResourcesScreenAlwaysShowsCachedAndInUseRows(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	deps = withPackages(t, deps,
		catalog.Package{ID: "crt-royale-reshade-by-akgunter", Name: "CRT-Royale-ReShade by akgunter"},
	)
	s := loadResourcesScreen(t, deps)

	// Fake the two reasons a row is sticky, one at a time.
	for _, tc := range []struct {
		name string
		mark func(*resourceRow)
	}{
		{"cached", func(r *resourceRow) { r.Cached = true }},
		{"in use", func(r *resourceRow) { r.InUse = true }},
	} {
		rows := slices.Clone(s.panes[panePackages])
		for i := range rows {
			if rows[i].ID == "crt-royale-reshade-by-akgunter" {
				tc.mark(&rows[i])
			}
		}
		s.panes[panePackages] = rows
		if !slices.Contains(visibleNames(s, panePackages), "CRT-Royale-ReShade by akgunter") {
			t.Errorf("a %s package must stay visible whatever the shortlist says", tc.name)
		}
	}
}

// The panes are ~20 columns wide, which is a name and nothing else, so
// the catalog's description of the focused row goes under them.
func TestResourcesScreenDescribesTheFocusedRow(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	deps = withPackages(t, deps, catalog.Package{
		ID: "sweetfx-by-ceejay-dk", Name: "SweetFX by CeeJay.dk",
		Description:   "The original SweetFX shader collection (LumaSharpen, SMAA, ...)",
		RepositoryURL: "https://github.com/CeeJayDK/SweetFX",
	})
	s := loadResourcesScreen(t, deps)
	s.focus = panePackages
	s.cursors[panePackages].setCursor(1)

	body := s.View(wizardEnv())
	if !strings.Contains(body, "The original SweetFX shader collection") {
		t.Errorf("the focused row's description should be shown:\n%s", body)
	}
	if !strings.Contains(body, "https://github.com/CeeJayDK/SweetFX") {
		t.Errorf("the detail block should name where it comes from:\n%s", body)
	}
}

// A manual-only add-on says why it cannot be downloaded here, the same way
// the wizard does.
func TestResourcesScreenExplainsAManualOnlyAddon(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	d := deps.WizardData.(fakeWizardData).data
	d.Addons = append(d.Addons, catalog.Addon{
		ID: "renodx-by-shortfuse", Name: "RenoDX by ShortFuse",
		RepositoryURL: "https://github.com/clshortfuse/renodx",
	})
	deps.WizardData = fakeWizardData{data: d}
	s := loadResourcesScreen(t, deps)
	s.focus = paneAddons
	s.cursors[paneAddons].setCursor(1)

	body := s.View(wizardEnv())
	if !strings.Contains(body, "manual install only: https://github.com/clshortfuse/renodx") {
		t.Errorf("a manual-only add-on should say so and name its repository:\n%s", body)
	}
}

// visibleNames lists what a pane currently shows.
func visibleNames(s *ResourcesScreen, p resourcePane) []string {
	var out []string
	for _, r := range s.visible(p) {
		out = append(out, r.Name)
	}
	return out
}

// pressClear sends X and returns the confirm overlay it must open.
func pressClear(t *testing.T, s *ResourcesScreen) confirmOverlay {
	t.Helper()
	_, cmd := s.Update(tea.KeyPressMsg{Code: 'X', Text: "X"}, wizardEnv())
	if cmd == nil {
		t.Fatal("'X' should return a command")
	}
	overlayMsg, ok := cmd().(showOverlayMsg)
	if !ok {
		t.Fatalf("message = %T, want showOverlayMsg", cmd())
	}
	confirm, ok := overlayMsg.overlay.(confirmOverlay)
	if !ok {
		t.Fatalf("overlay = %T, want confirmOverlay", overlayMsg.overlay)
	}
	return confirm
}

// X empties the whole cache, not just the row under the cursor.
func TestResourcesScreenClearCacheRemovesEverythingCached(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	if _, err := deps.Cache.EnsureReShade(context.Background(), "6.8.0", false, nil); err != nil {
		t.Fatalf("EnsureReShade: %v", err)
	}
	if _, err := deps.Cache.EnsurePackage(context.Background(), deps.WizardData.(fakeWizardData).data.Packages[0], nil); err != nil {
		t.Fatalf("EnsurePackage: %v", err)
	}
	s := loadResourcesScreen(t, deps)
	// The cursor sits on a ReShade row; the package must go too.
	s.focus = paneReShadeNormal

	result := pressClear(t, s).onYes()
	if _, ok := result.(resourcesLoadedMsg); !ok {
		t.Fatalf("confirmed clear produced %T, want resourcesLoadedMsg", result)
	}
	next, _ := s.Update(result, wizardEnv())
	s = next.(*ResourcesScreen)

	for p := resourcePane(0); p < paneCount; p++ {
		for _, r := range s.panes[p] {
			if r.Cached && !r.Custom {
				t.Errorf("%q still shows as cached after clearing", r.Name)
			}
		}
	}
	if items, size, _ := s.cachedTotals(); items != 0 || size != 0 {
		t.Errorf("cachedTotals() = %d items, %d bytes, want 0, 0", items, size)
	}
}

// Nothing cached is not a confirmation worth asking for.
func TestResourcesScreenClearCacheSaysSoWhenThereIsNothingToClear(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	s := loadResourcesScreen(t, deps)

	_, cmd := s.Update(tea.KeyPressMsg{Code: 'X', Text: "X"}, wizardEnv())
	if cmd == nil {
		t.Fatal("'X' should still say something when the cache is empty")
	}
	if _, ok := cmd().(showOverlayMsg); ok {
		t.Fatal("an empty cache should not open a confirm dialog")
	}
	msg, ok := cmd().(statusMsg)
	if !ok {
		t.Fatalf("message = %T, want statusMsg", cmd())
	}
	if !strings.Contains(msg.text, "nothing cached") {
		t.Errorf("status = %q, want it to say there is nothing cached", msg.text)
	}
}

// The count and size go in the question, so the decision can be made
// without reading the paragraph under it.
func TestClearCacheQuestionCountsWhatItWillRemove(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	if _, err := deps.Cache.EnsureReShade(context.Background(), "6.8.0", false, nil); err != nil {
		t.Fatalf("EnsureReShade: %v", err)
	}
	s := loadResourcesScreen(t, deps)

	confirm := pressClear(t, s)
	if !strings.Contains(confirm.question, "1 item(s)") {
		t.Errorf("question = %q, want the item count in it", confirm.question)
	}
	if !strings.Contains(confirm.detail, "Custom content") {
		t.Errorf("detail = %q, want it to say custom content is untouched", confirm.detail)
	}
}

// Clearing something a recorded install was made from is safe but worth
// saying out loud, the same way deleting one such row is.
func TestClearCacheWarnsAboutEntriesAnInstallUses(t *testing.T) {
	deps, _ := resourcesTestDeps(t)
	if _, err := deps.Cache.EnsureReShade(context.Background(), "6.8.0", false, nil); err != nil {
		t.Fatalf("EnsureReShade: %v", err)
	}
	reg := state.Registry{Schema: state.SchemaVersion, Games: map[string]state.Game{}}
	reg.Record("steam:1", state.Game{Name: "X", Provider: "steam", Root: testRoot("/games/x")}, state.Install{
		Exe:     "x.exe",
		ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "normal"},
	})
	if err := state.Save(deps.StateDir, reg); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	s := loadResourcesScreen(t, deps)
	if _, _, inUse := s.cachedTotals(); inUse != 1 {
		t.Fatalf("setup: cachedTotals() reports %d in use, want 1", inUse)
	}
	if detail := pressClear(t, s).detail; !strings.Contains(detail, "back a recorded install") {
		t.Errorf("detail = %q, want a warning about installs using it", detail)
	}
}

// renodxDeps adds two RenoDX mods to the resources fixture, one of which
// is cacheable from the test server.
func renodxDeps(t *testing.T) (Deps, []catalog.RenoMod) {
	t.Helper()
	deps, srv := resourcesTestDeps(t)
	mods := []catalog.RenoMod{
		{
			ID: "cp2077", Title: "Cyberpunk 2077", Status: "stable",
			Maintainers: []string{"ShortFuse"},
			Artifacts: []catalog.RenoArtifact{{
				Name: "renodx-cp2077.addon64", Arch: game.ArchX64, URL: srv.URL + "/r.addon64",
			}},
		},
		{
			ID: "wobbly", Title: "Wobbly Life", Status: "beta",
			Artifacts: []catalog.RenoArtifact{{
				Name: "renodx-wobbly.addon64", Arch: game.ArchX64, URL: srv.URL + "/r.addon64",
			}},
		},
	}
	d := deps.WizardData.(fakeWizardData).data
	d.RenoDX = mods
	deps.WizardData = fakeWizardData{data: d}
	return deps, mods
}

// RenoDX mods share the add-ons pane rather than getting a fifth: a fifth
// pane would push the four-pane floor from 88 columns to 110. There are
// ~200 of them, so the shortlist keeps the pane readable — only the ones
// already downloaded or in use show until `a` widens it.
func TestResourcesScreenListsRenoDXMods(t *testing.T) {
	deps, mods := renodxDeps(t)
	if _, err := deps.Cache.EnsureRenoDX(context.Background(), mods[0], game.ArchX64, nil); err != nil {
		t.Fatalf("EnsureRenoDX: %v", err)
	}

	s := loadResourcesScreen(t, deps)

	shortlisted := visibleNames(s, paneAddons)
	if !slices.Contains(shortlisted, "RenoDX: Cyberpunk 2077") {
		t.Errorf("a cached RenoDX mod should show in the shortlist: %v", shortlisted)
	}
	if slices.Contains(shortlisted, "RenoDX: Wobbly Life") {
		t.Errorf("an uncached RenoDX mod should stay behind show-all: %v", shortlisted)
	}

	s.showAll = true
	all := visibleNames(s, paneAddons)
	if !slices.Contains(all, "RenoDX: Wobbly Life") {
		t.Errorf("show-all should reveal every RenoDX mod: %v", all)
	}
}

func TestResourcesScreenDownloadsARenoDXMod(t *testing.T) {
	deps, _ := renodxDeps(t)
	s := loadResourcesScreen(t, deps)
	s.focus = paneAddons
	// Through the binding, not the field: toggling show-all re-syncs every
	// pane's cursor count, and setting the field alone leaves the cursor
	// clamped to the shortlisted length.
	next, _ := s.Update(tea.KeyPressMsg{Code: 'a', Text: "a"}, wizardEnv())
	s = next.(*ResourcesScreen)
	s.cursors[paneAddons].setCursor(indexOfVisible(t, s, paneAddons, "RenoDX: Cyberpunk 2077"))

	_, cmd := s.Update(tea.KeyPressMsg{Code: 'd', Text: "d"}, wizardEnv())
	if cmd == nil {
		t.Fatal("'d' should start a download")
	}
	if _, ok := cmd().(resourcesLoadedMsg); !ok {
		t.Fatalf("download produced %T, want a reload", cmd())
	}
	if !deps.Cache.HasRenoDX("cp2077") {
		t.Error("the mod was not cached")
	}
}

func TestResourcesScreenDeletesARenoDXMod(t *testing.T) {
	deps, mods := renodxDeps(t)
	if _, err := deps.Cache.EnsureRenoDX(context.Background(), mods[0], game.ArchX64, nil); err != nil {
		t.Fatalf("EnsureRenoDX: %v", err)
	}
	s := loadResourcesScreen(t, deps)
	s.focus = paneAddons
	s.cursors[paneAddons].setCursor(indexOfVisible(t, s, paneAddons, "RenoDX: Cyberpunk 2077"))

	_, cmd := s.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}, wizardEnv())
	if cmd == nil {
		t.Fatal("'x' should open the confirm dialog")
	}
	overlayMsg, ok := cmd().(showOverlayMsg)
	if !ok {
		t.Fatalf("message = %T, want showOverlayMsg", cmd())
	}
	confirm := overlayMsg.overlay.(confirmOverlay)
	if _, ok := confirm.onYes().(resourcesLoadedMsg); !ok {
		t.Fatal("confirming should reload the panes")
	}
	if deps.Cache.HasRenoDX("cp2077") {
		t.Error("the mod is still cached after deleting it")
	}
}

// A RenoDX mod is recorded in its own field rather than in the add-on
// list, so "in use" has to look there.
func TestResourcesScreenDetectsARenoDXModInUse(t *testing.T) {
	deps, mods := renodxDeps(t)
	if _, err := deps.Cache.EnsureRenoDX(context.Background(), mods[0], game.ArchX64, nil); err != nil {
		t.Fatalf("EnsureRenoDX: %v", err)
	}
	reg := state.Registry{Schema: state.SchemaVersion, Games: map[string]state.Game{}}
	reg.Record("steam:1", state.Game{Name: "X", Provider: "steam", Root: testRoot("/games/x")}, state.Install{
		Exe: "x.exe", ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "addon"}, RenoDX: "cp2077",
	})
	if err := state.Save(deps.StateDir, reg); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	s := loadResourcesScreen(t, deps)
	for _, r := range s.visible(paneAddons) {
		if r.ID == "cp2077" {
			if !r.InUse {
				t.Error("a mod a recorded install uses should show as in use")
			}
			return
		}
	}
	t.Errorf("no cp2077 row: %v", visibleNames(s, paneAddons))
}

// indexOfVisible finds a row by name in what a pane currently shows.
func indexOfVisible(t *testing.T, s *ResourcesScreen, p resourcePane, name string) int {
	t.Helper()
	for i, r := range s.visible(p) {
		if r.Name == name {
			return i
		}
	}
	t.Fatalf("no row %q in %v", name, visibleNames(s, p))
	return -1
}
