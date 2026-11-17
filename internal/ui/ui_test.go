package ui

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/config"
	"github.com/secato/yarm/internal/core"
	"github.com/secato/yarm/internal/fetch"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

type fakeLoader struct {
	games []core.GameEntry
	err   error
}

func (f fakeLoader) LoadGames(context.Context) ([]core.GameEntry, error) { return f.games, f.err }

type fakeData struct {
	data core.WizardData
	err  error
}

func (f fakeData) LoadWizardData(context.Context) (core.WizardData, error) { return f.data, f.err }

// fakeInstaller records what it was asked to install and can be told to
// block until canceled, the shape a real download blocks in.
type fakeInstaller struct {
	reqs    []install.Request
	err     error
	updates []core.ProgressUpdate
	block   chan struct{}
	onStart func()
}

func (f *fakeInstaller) Install(ctx context.Context, req install.Request, send func(core.ProgressUpdate)) (install.Result, error) {
	f.reqs = append(f.reqs, req)
	if f.onStart != nil {
		f.onStart()
	}
	for _, u := range f.updates {
		send(u)
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return install.Result{}, ctx.Err()
		}
	}
	return install.Result{Written: []string{"Game/dxgi.dll", "Game/ReShade.ini"}, Bytes: 4 << 20}, f.err
}

type fakeUninstaller struct{ reqs []install.UninstallRequest }

func (f *fakeUninstaller) Uninstall(r install.UninstallRequest) (install.UninstallResult, error) {
	f.reqs = append(f.reqs, r)
	return install.UninstallResult{Removed: []string{"a", "b"}}, nil
}

type fakeAdopter struct{ n int }

func (f *fakeAdopter) Adopt(game.Game, game.Executable, install.AdoptCandidate) (state.Install, error) {
	f.n++
	return state.Install{}, nil
}

func sampleData() core.WizardData {
	return core.WizardData{
		Versions: []catalog.Version{{Version: "6.8.0", Latest: true}, {Version: "6.7.3"}},
		Packages: []catalog.Package{
			{ID: "standard-effects", Name: "Standard effects", Required: true, Description: "ReShade's own shaders."},
			{ID: "sweetfx-by-ceejay-dk", Name: "SweetFX", Description: "Classic tonemap, vibrance and sharpen effects."},
			{ID: "immerse-by-marty-mcfly", Name: "iMMERSE", Description: "Ambient occlusion and sharpening."},
		},
		Addons: []catalog.Addon{
			{ID: "swap-chain-override-by-crosire", Name: "Swap chain override", URL64: "https://example.invalid/a.addon64"},
			{ID: "renodx-by-shortfuse", Name: "RenoDX by ShortFuse"},
		},
		RenoDX: []catalog.RenoMod{{
			ID: "eldenring", Title: "Elden Ring HDR", Status: "stable", SteamAppID: 1245620,
			Description: "Proper HDR tonemapping with peak brightness controls.",
			Artifacts: []catalog.RenoArtifact{{
				Name: "renodx-eldenring.addon64", Arch: game.ArchX64, Size: 1 << 20,
				URL: catalog.RenoDXAssetBase + "renodx-eldenring.addon64",
			}},
		}},
	}
}

func sampleGames() []core.GameEntry {
	installed := state.Install{
		Exe:      "Game/eldenring.exe",
		ReShade:  state.ReShadeInfo{Version: "6.8.0", Flavor: "addon", Arch: "x64", API: "d3d12", DLL: "dxgi.dll"},
		Packages: []string{"standard-effects", "sweetfx-by-ceejay-dk"},
	}
	mk := func(id, name, provider, root string, exes ...core.Executable) core.GameEntry {
		return core.GameEntry{Game: game.Game{ID: id, Name: name, Provider: provider, Root: root}, Exes: exes}.WithGroups()
	}
	x64 := func(path string, api game.API, in *state.Install) core.Executable {
		return core.Executable{Executable: game.Executable{Path: path, Arch: game.ArchX64, API: api}, Installed: in}
	}
	return []core.GameEntry{
		mk("steam:1245620", "Elden Ring", "steam", "/games/ELDEN RING",
			x64("Game/eldenring.exe", game.APID3D12, &installed), x64("Game/Launcher/start.exe", game.APIUnknown, nil)),
		mk("steam:1145360", "Hades II", "steam", "/games/Hades II", x64("Hades2.exe", game.APID3D11, nil)),
		mk("gog:1", "Baldur's Gate 3", "gog", "/games/BG3", x64("bg3.exe", game.APIVulkan, nil)),
		{Game: game.Game{ID: "steam:7", Name: "Ridgeline", Provider: "steam", Root: "/games/ridge"}, NativeBuild: true},
		mk("manual:1", "My Emulated Game", "manual", "/games/emulated", x64("emu.exe", game.APID3D9, nil)),
	}
}

// testApp builds an App with no window: background work runs inline, so a
// click and its consequences are over by the time Click returns.
func testApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	inst := &fakeInstaller{}
	c := cache.New(filepath.Join(dir, "cache"), fetch.New("yarm-test"))
	cfgDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Defaults.Packages = []string{"sweetfx-by-ceejay-dk"}
	games := sampleGames()
	a := New(Options{
		NewLoader: func(config.Config) core.GamesLoader { return fakeLoader{games: games} },
		Deps: core.Deps{
			WizardData:  fakeData{data: sampleData()},
			Installer:   inst,
			Uninstaller: &fakeUninstaller{},
			Adopter:     &fakeAdopter{},
			CacheStatus: c,
			Defaults:    cfg.Defaults,
			Cache:       c,
			StateDir:    filepath.Join(dir, "state"),
			CustomDir:   filepath.Join(dir, "custom"),
			Config:      cfg,
			ConfigDir:   cfgDir,
		},
	})
	a.spawn = func(f func()) { f() }
	a.onMain = func(f func()) { f() }
	a.pick = func(string) string { return "" }
	a.themeMode = "dark"
	return a
}

func (a *App) inst() *fakeInstaller { return a.deps.Installer.(*fakeInstaller) }

// open loads the games the way Start does, then draws a first frame.
// click presses the first control with this text or label and fails the test
// if there is none, so a renamed button cannot turn a test into a no-op.
func click(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	if err := tt.Click(name); err != nil {
		t.Fatalf("click %q: %v", name, err)
	}
}

func open(t *testing.T, a *App) *ui.Tester {
	t.Helper()
	a.rescan(false)
	return ui.NewTester(a.View, 1180, 860)
}

func shot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("YARM_SHOTS")
	if dir == "" {
		return
	}
	// Switches slide and dialogs fade in: let them finish before the picture.
	tt.Advance(time.Second)
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestListShowsGamesInGroups(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)

	for _, want := range []string{"Elden Ring", "Hades II", "Baldur's Gate 3", "My Emulated Game", "Steam", "GOG", "Manual"} {
		if !tt.HasText(want) {
			t.Errorf("the list lacks %q", want)
		}
	}
	for _, want := range []string{"INSTALLED · 1", "NOT INSTALLED · 3", "ADDED BY YOU · 1"} {
		if !tt.HasText(want) {
			t.Errorf("the list lacks the group %q; texts: %q", want, tt.Texts())
		}
	}
	if a.selected != "steam:1245620" {
		t.Errorf("selected = %q, want the first installed game", a.selected)
	}
	shot(t, tt, "game-dark")
}

func TestSearchFiltersTheList(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	a.query = "hades"
	tt.Frame()
	if !tt.HasText("Hades II") || tt.HasText("Baldur's Gate 3") {
		t.Errorf("search hades shows %q", tt.Texts())
	}
	a.query = "zzz"
	tt.Frame()
	if !tt.HasText("No game matches") {
		t.Error("an empty search should say so")
	}
}

func TestSelectingAGameShowsItsPage(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	if err := tt.Click("Hades II"); err != nil {
		t.Fatal(err)
	}
	if a.selected != "steam:1145360" {
		t.Fatalf("selected %q", a.selected)
	}
	for _, want := range []string{"HADES II", "Folders", "ReShade", "Shaders", "Add-ons", "RenoDX", "Review", "Install"} {
		if !tt.HasText(want) {
			t.Errorf("the page lacks %q", want)
		}
	}
}

func TestNativeBuildHasNoTabs(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	click(t, tt, "Ridgeline")
	if !tt.HasText("This is a native Linux build") || tt.HasText("Shaders") {
		t.Errorf("native page = %q", tt.Texts())
	}
}

func TestInstallFlow(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	click(t, tt, "Hades II")
	click(t, tt, "Shaders")
	if err := tt.Click("iMMERSE"); err != nil {
		t.Fatal(err)
	}
	shot(t, tt, "shaders-dark")

	if err := tt.Click("Install"); err != nil {
		t.Fatal(err)
	}
	if got := len(a.inst().reqs); got != 1 {
		t.Fatalf("installer called %d times", got)
	}
	req := a.inst().reqs[0]
	if req.Version != "6.8.0" || req.DLLName != "d3d11.dll" && req.DLLName != "dxgi.dll" {
		t.Errorf("request = %+v", req)
	}
	if !contains(req.Packages, "immerse-by-marty-mcfly") || !contains(req.Packages, "standard-effects") {
		t.Errorf("packages = %v", req.Packages)
	}
	if a.dock.report == nil || a.dock.report.Failed {
		t.Fatalf("dock = %+v", a.dock)
	}
	if !tt.HasText("Done") || !tt.HasText("Installed") {
		t.Errorf("the result is not shown: %q", tt.Texts())
	}
	shot(t, tt, "result-dark")
	click(t, tt, "Done")
	if a.dock.report != nil {
		t.Error("Done did not close the result")
	}
}

func TestFailedInstallIsReported(t *testing.T) {
	a := testApp(t)
	a.inst().err = errors.New("permission denied")
	tt := open(t, a)
	click(t, tt, "Hades II")
	click(t, tt, "Install")
	if a.dock.report == nil || !a.dock.report.Failed {
		t.Fatalf("report = %+v", a.dock.report)
	}
	if !tt.HasText("Finished with a problem") {
		t.Errorf("texts %q", tt.Texts())
	}
}

// While an apply runs the dock shows its step and a Cancel button; pressing
// it stops the run and the result says what was rolled back.
func TestRunningDockShowsProgress(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	click(t, tt, "Hades II")
	a.dock = dock{running: true, title: "Applying to Hades II", step: "Downloading iMMERSE", done: 12, total: 40}
	tt.Frame()
	if !tt.HasText("Downloading iMMERSE") || !tt.HasText("Cancel") {
		t.Errorf("running dock = %q", tt.Texts())
	}
	shot(t, tt, "running-dark")
}

func TestCancelStopsTheRun(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	click(t, tt, "Hades II")
	// The installer blocks the way a download does, and cancels itself the
	// moment it starts, as a click on Cancel would from the UI thread.
	a.inst().block = make(chan struct{})
	a.inst().onStart = func() { a.dock.cancel() }
	click(t, tt, "Install")
	rep := a.dock.report
	if rep == nil || !rep.Canceled || rep.Summary() != "Canceled before it finished" {
		t.Fatalf("report = %+v", rep)
	}
	if !tt.HasText("Canceled before it finished") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestUninstallConfirmsFirst(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	if err := tt.Click("Uninstall"); err != nil {
		t.Fatal(err)
	}
	if !a.uninstalling || !tt.HasText("UNINSTALL FROM ELDEN RING?") {
		t.Fatalf("uninstalling = %v, texts %q", a.uninstalling, tt.Texts())
	}
	shot(t, tt, "uninstall-dark")
	un := a.deps.Uninstaller.(*fakeUninstaller)
	if len(un.reqs) != 0 {
		t.Fatal("uninstall ran before it was confirmed")
	}
	// the dialog's own button is the second "Uninstall" on screen
	a.uninstalling = false
	a.runOps(a.mustEntry("steam:1245620"), core.UninstallOps(a.mustEntry("steam:1245620"), core.InstalledGroups(a.mustEntry("steam:1245620"))), "Uninstalling from Elden Ring")
	if len(un.reqs) != 1 || un.reqs[0].Exe != "Game/eldenring.exe" {
		t.Errorf("uninstall requests = %+v", un.reqs)
	}
}

func (a *App) mustEntry(id string) core.GameEntry {
	e, _ := a.entry(id)
	return e
}

func TestEditShowsChangesAndDiscards(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	if tt.HasText("Apply changes") {
		t.Fatal("an untouched edit offers Apply")
	}
	if !tt.HasText("Up to date") {
		t.Errorf("an untouched edit says %q", tt.Texts())
	}
	s := a.session()
	s.TogglePackage("immerse-by-marty-mcfly")
	tt.Frame()
	if !tt.HasText("Apply changes") || !tt.HasText("Discard changes") {
		t.Fatalf("a changed edit shows %q", tt.Texts())
	}
	click(t, tt, "Discard changes")
	if a.session().Dirty() {
		t.Error("Discard changes kept the changes")
	}
}

func TestTabsRender(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	for i, name := range []string{"Folders", "ReShade", "Shaders", "Add-ons", "RenoDX", "Review"} {
		if err := tt.Click(name); err != nil {
			t.Fatalf("click %s: %v", name, err)
		}
		if a.tab != i {
			t.Errorf("tab = %d after clicking %s, want %d", a.tab, name, i)
		}
		shot(t, tt, "tab-"+name)
	}
}

func TestThemeToggleSaves(t *testing.T) {
	a := testApp(t)
	a.themeMode = ""
	tt := open(t, a)
	click(t, tt, "Light")
	if a.cfg.Theme != "light" || pal.dark {
		t.Errorf("theme = %q, dark %v", a.cfg.Theme, pal.dark)
	}
	shot(t, tt, "game-light")
	saved, err := config.Load(a.deps.ConfigDir)
	if err != nil || saved.Theme != "light" {
		t.Errorf("saved = %q, %v", saved.Theme, err)
	}
	click(t, tt, "Dark")
	if a.cfg.Theme != "dark" || !pal.dark {
		t.Errorf("theme = %q", a.cfg.Theme)
	}
}

func TestEmptyStateOffersAddingAGame(t *testing.T) {
	a := testApp(t)
	a.loader = fakeLoader{}
	a.opts.NewLoader = func(config.Config) core.GamesLoader { return fakeLoader{} }
	tt := open(t, a)
	if !tt.HasText("NO GAMES FOUND") || !tt.HasText("Add a game folder") {
		t.Fatalf("empty = %q", tt.Texts())
	}
	shot(t, tt, "empty-dark")
	click(t, tt, "Add a game folder")
	if !a.add.open {
		t.Error("the button did not open the dialog")
	}
}

func TestOfflineBanner(t *testing.T) {
	a := testApp(t)
	a.deps.WizardData = fakeData{data: sampleData(), err: errors.New("dial tcp: lookup api.github.com: no such host")}
	tt := open(t, a)
	if !tt.HasText("You're offline.") {
		t.Errorf("no banner: %q", tt.Texts())
	}
	click(t, tt, "Show details")
	if !a.offlineOpen {
		t.Error("Show details did nothing")
	}
}

func TestAddGameDialog(t *testing.T) {
	a := testApp(t)
	tt := open(t, a)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "game.exe"), peStub(), 0o644); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()
	if !tt.HasText("ADD A GAME") {
		t.Fatalf("no dialog: %q", tt.Texts())
	}
	a.setAddPath("/no/such/folder")
	tt.Frame()
	if !tt.HasText("No such folder.") {
		t.Errorf("no problem shown: %q", tt.Texts())
	}
	shot(t, tt, "addgame-dark")
	a.setAddPath(dir)
	if a.add.name != filepath.Base(dir) {
		t.Errorf("name = %q", a.add.name)
	}
}

func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

// peStub is not a real executable: just enough bytes for the folder to be
// scanned. CheckGameFolder counts what game.Scan calls playable.
func peStub() []byte { return []byte("MZ") }
