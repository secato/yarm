package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/secato/yarm/internal/config"

	"github.com/egoist/mygo/ui"

	"github.com/secato/yarm/internal/core"
	"github.com/secato/yarm/internal/install"
)

func TestResourcesPage(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	if err := tt.Click("Resources"); err != nil {
		t.Fatal(err)
	}
	if a.view != viewResources || a.res.state != stateReady {
		t.Fatalf("view %v, state %v, err %q", a.view, a.res.state, a.res.err)
	}
	for _, want := range []string{"RESOURCES", "Clean unused", "ReShade 6.8.0", "Not downloaded", "Download"} {
		if !tt.HasText(want) {
			t.Errorf("the page lacks %q: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "resources-dark")
	for _, pane := range []string{"Shaders", "Add-ons", "RenoDX"} {
		if err := tt.Click(pane); err != nil {
			t.Fatalf("click %s: %v", pane, err)
		}
	}
	// Nothing is downloaded, so the RenoDX shortlist is empty and says how to see more.
	if !tt.HasText("Nothing here yet. Choose All to see everything on offer.") {
		t.Errorf("RenoDX pane = %q", tt.Texts())
	}
	if err := tt.Click("All"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Elden Ring HDR") {
		t.Errorf("All did not show the mod: %q", tt.Texts())
	}
	// A game's page again.
	if err := tt.Click("Elden Ring"); err != nil {
		t.Fatal(err)
	}
	if a.view != viewGame {
		t.Error("picking a game did not leave Resources")
	}
}

func TestResourcesWithoutACache(t *testing.T) {
	a := testApp(t)
	a.deps.Cache = nil
	tt := open(t, a)
	if err := tt.Click("Resources"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Couldn't read the cache") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestCustomContentPage(t *testing.T) {
	a := testApp(t)
	dir := a.deps.CustomDir
	for _, d := range []string{filepath.Join(dir, "shaders", "Film Grain", "Shaders"), filepath.Join(dir, "addons", "overlay")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"shaders/Film Grain/Shaders/g.fx", "addons/overlay/o.addon64"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(f)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tt := open(t, a)
	if err := tt.Click("Custom content"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CUSTOM CONTENT", "FOUND · 2", "Film Grain", "overlay", "Shader pack", "Add-on", dir} {
		if !tt.HasText(want) {
			t.Errorf("the page lacks %q: %q", want, tt.Texts())
		}
	}
	shot(t, tt, "custom-dark")
}

func TestCustomContentEmpty(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	click(t, tt, "Custom content")
	if !tt.HasText("FOUND · 0") || !tt.HasText("Nothing found yet. Drop a shader pack or add-on into one of the folders above.") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestSettingsApplyLive(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	if err := tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	if !a.setting.open || !tt.HasText("SETTINGS") {
		t.Fatalf("texts %q", tt.Texts())
	}
	shot(t, tt, "settings-dark")

	// Switching a provider off saves and rescans at once.
	if err := tt.Click("Search GOG"); err != nil {
		t.Fatal(err)
	}
	if a.cfg.GOG.Enabled {
		t.Error("the GOG switch did nothing")
	}
	if saved, err := loadConfig(a); err != nil || saved.GOG.Enabled {
		t.Errorf("saved GOG = %v, %v", saved.GOG.Enabled, err)
	}

	click(t, tt, "Add-on support")
	if a.cfg.Defaults.ReshadeFlavor != "addon" {
		t.Errorf("flavor default = %q", a.cfg.Defaults.ReshadeFlavor)
	}
	click(t, tt, "Normal")
	if a.cfg.Defaults.ReshadeFlavor != "normal" {
		t.Errorf("flavor default = %q", a.cfg.Defaults.ReshadeFlavor)
	}

	// A library path comes from the system's folder dialog.
	a.pick = func(string) string { return "/mnt/games/SteamLibrary" }
	click(t, tt, "Add folder")
	if len(a.cfg.Steam.ExtraLibraryPaths) != 1 {
		t.Fatalf("paths = %v", a.cfg.Steam.ExtraLibraryPaths)
	}
	tt.Frame()
	if !tt.HasText("/mnt/games/SteamLibrary") {
		t.Error("the added path is not shown")
	}
	click(t, tt, "Remove /mnt/games/SteamLibrary")
	if len(a.cfg.Steam.ExtraLibraryPaths) != 0 {
		t.Errorf("paths = %v", a.cfg.Steam.ExtraLibraryPaths)
	}

	// The cache location needs a restart, and says so.
	a.pick = func(string) string { return "/mnt/cache" }
	click(t, tt, "Change…")
	if a.cfg.CacheDir != "/mnt/cache" {
		t.Errorf("cache dir = %q", a.cfg.CacheDir)
	}

	// A canceled dialog changes nothing.
	a.pick = func(string) string { return "" }
	click(t, tt, "Change…")
	if a.cfg.CacheDir != "/mnt/cache" {
		t.Error("canceling changed the cache dir")
	}
}

func TestSettingsRejectAnInvalidConfig(t *testing.T) {
	a := testApp(t)
	next := a.cfg
	next.Defaults.ReshadeFlavor = "bogus"
	if a.saveConfig(next, false) {
		t.Error("an invalid configuration was saved")
	}
	if a.cfg.Defaults.ReshadeFlavor == "bogus" {
		t.Error("an invalid configuration was taken in")
	}
}

func TestAddGameEndToEnd(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	dir := t.TempDir()
	// The scanner counts a PE file, which needs real headers.
	if err := os.WriteFile(filepath.Join(dir, "game.exe"), minimalPE(), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openAddGame()
	tt.Frame()
	a.pick = func(string) string { return dir }
	click(t, tt, "Browse…")
	tt.Frame()
	if !a.add.check.OK() {
		t.Skipf("the stub is not recognized as an executable here: %+v", a.add.check)
	}
	if err := tt.Click("Add game"); err != nil {
		t.Fatal(err)
	}
	if a.add.open || len(a.cfg.ManualGames) != 1 {
		t.Errorf("open %v, manual games %v", a.add.open, a.cfg.ManualGames)
	}
}

func TestAdoptFlow(t *testing.T) {
	a := testApp(t)
	e := a.games
	_ = e
	// A game with an untracked ReShade in its folder.
	g := a.mustEntryLoaded(t)
	g.Groups[0].Unmanaged = &install.AdoptCandidate{DLLName: "dxgi.dll"}
	for i := range a.games {
		if a.games[i].ID == g.ID {
			a.games[i] = g
		}
	}
	a.gamesLoad = stateReady
	a.dataLoad, a.data = stateReady, sampleData()
	a.selected = g.ID
	tt := ui.NewTester(a.View, 1180, 860)
	if !tt.HasText("Adopt existing install") {
		t.Fatalf("no adopt button: %q", tt.Texts())
	}
	click(t, tt, "Adopt existing install")
	if !a.adopting || !tt.HasText("ADOPT THE EXISTING RESHADE?") {
		t.Fatalf("adopting %v: %q", a.adopting, tt.Texts())
	}
	shot(t, tt, "adopt-dark")
	a.adopt(g, g.Groups)
	if ad := a.deps.Adopter.(*fakeAdopter); ad.n != 1 {
		t.Errorf("adopted %d times", ad.n)
	}
}

func (a *App) mustEntryLoaded(t *testing.T) core.GameEntry {
	t.Helper()
	// Baldur's Gate 3 has a single, uninstalled folder.
	for _, g := range sampleGames() {
		if g.ID == "gog:1" {
			a.games = sampleGames()
			return g
		}
	}
	t.Fatal("no fixture")
	return core.GameEntry{}
}

func TestForeignFileSendsApplyToReview(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Bin", "dxgi.dll"), []byte("another injector"), 0o644); err != nil {
		t.Fatal(err)
	}
	games := sampleGames()
	for i := range games {
		if games[i].ID == "steam:1145360" {
			games[i].Root = root
			games[i].Exes[0].Path = "Bin/Hades2.exe"
			games[i] = games[i].WithGroups()
		}
	}
	a.loader = fakeLoader{games: games}
	tt := open(t, a)
	click(t, tt, "Hades II")
	click(t, tt, "Install")
	if a.tab != tabReview || len(a.inst().reqs) != 0 {
		t.Fatalf("tab %d, installs %d: the apply should wait for a look at the review", a.tab, len(a.inst().reqs))
	}
	if !tt.HasText("Another file holds dxgi.dll") {
		t.Errorf("no warning: %q", tt.Texts())
	}
	shot(t, tt, "foreign-dark")
	click(t, tt, "Install")
	if len(a.inst().reqs) != 1 || !a.inst().reqs[0].Overwrite {
		t.Errorf("the second Install should go ahead: %+v", a.inst().reqs)
	}
}

func TestCtrlEnterApplies(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	click(t, tt, "Hades II")
	tt.Key(ui.Cmd, ui.KeyEnter)
	if len(a.inst().reqs) != 1 {
		t.Errorf("Ctrl+Enter did not apply: %d installs", len(a.inst().reqs))
	}
}

func TestCtrlRRescansAndSlashSearches(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	n := 0
	a.loader = countingLoader{n: &n, games: sampleGames()}
	tt.Key(ui.Cmd, ui.KeyR)
	if n != 1 {
		t.Errorf("rescans = %d", n)
	}
	tt.Key(0, ui.KeySlash)
}

func TestSwitchingGamesIsBlockedWhileApplying(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	a.dock = dock{running: true}
	click(t, tt, "Hades II")
	if a.selected == "steam:1145360" {
		t.Error("the page changed mid-apply")
	}
}

func TestRenoDXTabAndAddonFlavorNotices(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	click(t, tt, "Hades II")
	click(t, tt, "Add-ons")
	if !tt.HasText("Add-ons need the Add-on support flavor") {
		t.Errorf("no notice: %q", tt.Texts())
	}
	click(t, tt, "Use Add-on support")
	if a.session().Flavor != install.FlavorAddon {
		t.Error("the button did not switch the flavor")
	}
	click(t, tt, "RenoDX")
	if !tt.HasText("Elden Ring HDR") {
		t.Errorf("RenoDX list: %q", tt.Texts())
	}
	shot(t, tt, "renodx-dark")
	click(t, tt, "Add-ons")
	shot(t, tt, "addons-dark")
}

func TestThemeEnv(t *testing.T) {
	t.Setenv("YARM_THEME", "Light")
	if ThemeFromEnv() != "light" {
		t.Errorf("ThemeFromEnv() = %q", ThemeFromEnv())
	}
	t.Setenv("YARM_THEME", "blue")
	if ThemeFromEnv() != "" {
		t.Errorf("ThemeFromEnv() = %q", ThemeFromEnv())
	}
}

type countingLoader struct {
	n     *int
	games []core.GameEntry
}

func (c countingLoader) LoadGames(ctx context.Context) ([]core.GameEntry, error) {
	*c.n++
	return c.games, nil
}

func loadConfig(a *App) (config.Config, error) { return config.Load(a.deps.ConfigDir) }

func minimalPE() []byte { return []byte("MZ") }

type resettableData struct {
	fakeData
	resets int
}

func (r *resettableData) Reset() { r.resets++ }

type countingRefresher struct{ n int }

func (c *countingRefresher) RefreshCatalog(context.Context) error { c.n++; return nil }

// Ctrl+R pulls the catalogs again and drops the memoized copy of the old
// ones, or the rescan would show exactly what it showed before.
func TestRescanRefreshesTheCatalog(t *testing.T) {
	a := testApp(t)
	data := &resettableData{fakeData: fakeData{data: sampleData()}}
	ref := &countingRefresher{}
	a.deps.WizardData, a.deps.CatalogRefresh = data, ref
	tt := open(t, a)
	if ref.n != 0 || data.resets != 0 {
		t.Fatalf("the first load refreshed: %d, %d", ref.n, data.resets)
	}
	tt.Key(ui.Cmd, ui.KeyR)
	if ref.n != 1 || data.resets != 1 {
		t.Errorf("refreshes = %d, resets = %d, want 1 and 1", ref.n, data.resets)
	}
}

// Typing a slash into a tab's search box must reach the box, not pull focus
// to the sidebar's search.
func TestSlashTypedIntoASearchFieldStaysThere(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	click(t, tt, "Shaders")
	tt.Frame()
	click(t, tt, "Search shader packs")
	tt.TypeKey(0, ui.KeySlash, "/")
	tt.Frame()
	if a.shaderQ != "/" {
		t.Errorf("shader search = %q, want the slash typed into it", a.shaderQ)
	}
}
