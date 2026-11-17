package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/fetch"
	"github.com/secato/yarm/internal/state"
)

// resourceDeps is a Deps over an empty cache and registry in temp dirs.
type resourceDeps struct {
	Deps
	cacheRoot string
}

func newResourceDeps(t *testing.T, data WizardData) resourceDeps {
	t.Helper()
	root := t.TempDir()
	c := cache.New(filepath.Join(root, "cache"), fetch.New("yarm-test"))
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	d := fakeDeps()
	d.WizardData = fakeWizardData{data: data}
	d.Cache = c
	d.CacheStatus = c
	d.StateDir = filepath.Join(root, "state")
	return resourceDeps{Deps: d, cacheRoot: c.Root}
}

// seed puts a cache entry on disk the way a download would have left it: a
// directory holding a file, and a row in the index.
func (r resourceDeps) seed(t *testing.T, kind cache.Kind, id, rel string, size int) {
	t.Helper()
	dir := filepath.Join(r.cacheRoot, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := cache.Index{Schema: cache.SchemaVersion, Entries: map[string]cache.Entry{}}
	if raw, err := os.ReadFile(filepath.Join(r.cacheRoot, cache.IndexFile)); err == nil {
		_ = json.Unmarshal(raw, &idx)
	}
	idx.Entries[id] = cache.Entry{
		ID: id, Kind: kind, Path: rel, Name: id,
		DownloadedAt: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC),
	}
	raw, _ := json.Marshal(idx)
	if err := os.WriteFile(filepath.Join(r.cacheRoot, cache.IndexFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// recordInstall writes an install using the given packs to installs.json.
func (r resourceDeps) recordInstall(t *testing.T, packages ...string) {
	t.Helper()
	if err := os.MkdirAll(r.StateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg, err := state.Load(r.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	reg.Record("steam:1", state.Game{Name: "G", Provider: "steam", Root: testRoot("/games/g")}, state.Install{
		Exe:      "g.exe",
		ReShade:  state.ReShadeInfo{Version: "6.8.0", Flavor: "normal", Arch: "x64", API: "d3d11", DLL: "dxgi.dll"},
		Packages: packages,
	})
	if err := state.Save(r.StateDir, reg); err != nil {
		t.Fatal(err)
	}
}

func resourceData() WizardData {
	d := sampleWizardData()
	d.Packages = append(d.Packages, catalog.Package{ID: "immerse-by-marty-mcfly", Name: "iMMERSE", DownloadURL: "https://example.invalid/i.zip"})
	d.Packages = append(d.Packages, catalog.Package{ID: "obscure-pack-nobody-curated", Name: "Obscure"})
	d.CustomShaders = []catalog.Custom{{ID: "custom:shaders:film", Name: "Film Grain", Kind: catalog.CustomShaders}}
	return d
}

func TestLoadResourcesFlagsCachedAndInUse(t *testing.T) {
	r := newResourceDeps(t, resourceData())
	r.seed(t, cache.KindPackage, "package:sweetfx-by-ceejay-dk:20261028-abc1234", "packages/sweetfx-by-ceejay-dk/20261028-abc1234", 2048)
	r.seed(t, cache.KindPackage, "package:immerse-by-marty-mcfly:20261028-def5678", "packages/immerse-by-marty-mcfly/20261028-def5678", 1024)
	r.recordInstall(t, "sweetfx-by-ceejay-dk")

	res, err := LoadResources(context.Background(), r.Deps)
	if err != nil {
		t.Fatal(err)
	}

	byID := map[string]ResourceRow{}
	for _, row := range res.Panes[PaneShaders] {
		byID[row.ID] = row
	}
	if sw := byID["sweetfx-by-ceejay-dk"]; !sw.Cached || sw.UsedBy != 1 || sw.Size == 0 || sw.Sub != "2026-10-28" {
		t.Errorf("sweetfx = %+v", sw)
	}
	if im := byID["immerse-by-marty-mcfly"]; !im.Cached || im.InUse() {
		t.Errorf("immerse = %+v", im)
	}
	if c := byID["custom:shaders:film"]; !c.Custom || !c.Cached || c.Sub != "Your own content" {
		t.Errorf("custom = %+v", c)
	}

	items, inUse, size := res.Items()
	if items != 2 || inUse != 1 || size == 0 {
		t.Errorf("Items() = %d, %d, %d; custom content is not in the cache", items, inUse, size)
	}
	if res.Total == 0 {
		t.Error("Total() is zero with two entries on disk")
	}

	// ReShade rows: both builds of each version, ReShade never shortlisted.
	if got := len(res.Panes[PaneReShade]); got != 4 {
		t.Errorf("ReShade rows = %d, want a normal and an add-on row for each of two versions", got)
	}
	if got := len(res.Visible(PaneReShade, false)); got != 4 {
		t.Errorf("ReShade visible = %d", got)
	}
}

func TestVisibleShortlistsButKeepsWhatYouHave(t *testing.T) {
	r := newResourceDeps(t, resourceData())
	r.seed(t, cache.KindPackage, "package:immerse-by-marty-mcfly:20261028-def5678", "packages/immerse-by-marty-mcfly/20261028-def5678", 10)

	res, err := LoadResources(context.Background(), r.Deps)
	if err != nil {
		t.Fatal(err)
	}
	short, all := res.Visible(PaneShaders, false), res.Visible(PaneShaders, true)
	if len(short) >= len(all) {
		t.Errorf("the shortlist (%d) hides nothing of %d", len(short), len(all))
	}
	for _, row := range short {
		if row.ID == "obscure-pack-nobody-curated" {
			t.Error("an uncurated pack nobody has is on the shortlist")
		}
	}
	found := false
	for _, row := range short {
		if row.ID == "immerse-by-marty-mcfly" {
			found = true
		}
	}
	if !found {
		t.Error("a downloaded pack fell off the shortlist")
	}
	// RenoDX has no curated list: only what you have, and nothing is cached.
	if got := res.Visible(PaneRenoDX, false); len(got) != 0 {
		t.Errorf("RenoDX shortlist = %d rows, want none cached", len(got))
	}
	if got := res.Visible(PaneRenoDX, true); len(got) != len(res.Panes[PaneRenoDX]) || len(got) == 0 {
		t.Errorf("RenoDX all = %d", len(got))
	}
}

func TestDeleteResourceRefusesWhatIsInUse(t *testing.T) {
	r := newResourceDeps(t, resourceData())
	r.seed(t, cache.KindPackage, "package:sweetfx-by-ceejay-dk:20261028-abc1234", "packages/sweetfx-by-ceejay-dk/20261028-abc1234", 10)
	r.seed(t, cache.KindPackage, "package:immerse-by-marty-mcfly:20261028-def5678", "packages/immerse-by-marty-mcfly/20261028-def5678", 10)
	r.recordInstall(t, "sweetfx-by-ceejay-dk")

	res, _ := LoadResources(context.Background(), r.Deps)
	rows := map[string]ResourceRow{}
	for _, row := range res.Panes[PaneShaders] {
		rows[row.ID] = row
	}

	if err := DeleteResource(r.Deps, rows["sweetfx-by-ceejay-dk"]); err == nil || !strings.Contains(err.Error(), "used by an install") {
		t.Errorf("deleting what is in use: err = %v", err)
	}
	if !r.Cache.HasPackage("sweetfx-by-ceejay-dk") {
		t.Error("a pack in use was removed")
	}
	if err := DeleteResource(r.Deps, rows["immerse-by-marty-mcfly"]); err != nil {
		t.Fatal(err)
	}
	if r.Cache.HasPackage("immerse-by-marty-mcfly") {
		t.Error("an unused pack was kept")
	}
	// Nothing to do for custom content, or for what is not there.
	if err := DeleteResource(r.Deps, rows["custom:shaders:film"]); err != nil {
		t.Errorf("custom delete: %v", err)
	}
}

func TestCleanUnusedKeepsInstallsInputs(t *testing.T) {
	r := newResourceDeps(t, resourceData())
	r.seed(t, cache.KindPackage, "package:sweetfx-by-ceejay-dk:20261028-abc1234", "packages/sweetfx-by-ceejay-dk/20261028-abc1234", 4096)
	r.seed(t, cache.KindPackage, "package:immerse-by-marty-mcfly:20261028-def5678", "packages/immerse-by-marty-mcfly/20261028-def5678", 8192)
	r.recordInstall(t, "sweetfx-by-ceejay-dk")

	freed, kept, err := CleanUnused(r.Deps)
	if err != nil {
		t.Fatal(err)
	}
	if freed < 8192 || kept != 1 {
		t.Errorf("freed %d, kept %d", freed, kept)
	}
	if !r.Cache.HasPackage("sweetfx-by-ceejay-dk") || r.Cache.HasPackage("immerse-by-marty-mcfly") {
		t.Error("the sweep kept the wrong things")
	}
	// A second sweep has nothing to do.
	if freed, _, err := CleanUnused(r.Deps); err != nil || freed != 0 {
		t.Errorf("second sweep freed %d, err %v", freed, err)
	}
}

func TestDownloadAndRefreshSkipWhatTheyShouldNot(t *testing.T) {
	r := newResourceDeps(t, resourceData())
	ctx := context.Background()
	for _, row := range []ResourceRow{
		{Cached: true, Downloadable: true},
		{Custom: true},
		{Downloadable: false},
	} {
		if err := DownloadResource(ctx, r.Deps, row); err != nil {
			t.Errorf("DownloadResource(%+v) = %v, want a no-op", row, err)
		}
	}
	if err := RefreshResource(ctx, r.Deps, ResourceRow{pane: PaneAddons, Cached: true}); err != nil {
		t.Errorf("refreshing an add-on: %v", err)
	}
	if (ResourceRow{pane: PaneAddons, Cached: true}).CanRefresh() || !(ResourceRow{pane: PaneShaders, Cached: true}).CanRefresh() {
		t.Error("CanRefresh is wrong")
	}
}

func TestEnsureAllArchesWithNothingInstallable(t *testing.T) {
	r := newResourceDeps(t, resourceData())
	ctx := context.Background()
	if err := ensureRenoDXAllArches(ctx, r.Cache, catalog.RenoMod{ID: "x"}); err == nil {
		t.Error("a RenoDX mod with no artifacts should be an error")
	}
	if err := ensureAddonAllArches(ctx, r.Cache, catalog.Addon{ID: "y"}); err == nil {
		t.Error("an add-on with no source should be an error")
	}
	if got := renodxDescription(catalog.RenoMod{Status: "beta", Maintainers: []string{"A", "B"}}); !strings.Contains(got, "beta") || !strings.Contains(got, "by A, B") {
		t.Errorf("renodxDescription = %q", got)
	}
}

func TestPaneLabels(t *testing.T) {
	want := []string{"ReShade", "Shaders", "Add-ons", "RenoDX"}
	for p := Pane(0); p < PaneCount; p++ {
		if p.Label() != want[p] {
			t.Errorf("Pane(%d).Label() = %q, want %q", p, p.Label(), want[p])
		}
	}
	if Pane(99).Label() != "?" {
		t.Error("an unknown pane should say so")
	}
}
