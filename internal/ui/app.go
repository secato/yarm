// Package ui is yarm's window: the only package that imports MyGo. Everything
// it decides lives in internal/core; this package only draws it and turns
// clicks into calls.
package ui

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/secato/yarm/internal/config"
	"github.com/secato/yarm/internal/core"
	"github.com/secato/yarm/internal/desktop"
)

// Options is what the window needs from main: the wired dependencies, and
// how to build the game loader again when Settings changes the providers.
type Options struct {
	Deps core.Deps
	// NewLoader builds the games loader from a configuration. It is called
	// at start and whenever a provider is switched on or off.
	NewLoader func(cfg config.Config) core.GamesLoader
	// Version is shown nowhere but the log; the window has no About box.
	Version string
	// Theme overrides the saved look ("dark" or "light") for this run, as
	// YARM_THEME does; "" follows the saved choice.
	Theme string
}

// view is what the main pane shows.
type view int

const (
	viewGame view = iota
	viewResources
	viewCustom
)

// loadState is the life of something loaded in the background.
type loadState int

const (
	stateIdle loadState = iota
	stateLoading
	stateReady
	stateFailed
)

// App is the state of the window.
type App struct {
	opts Options
	deps core.Deps
	cfg  config.Config
	win  *mygo.Window
	ctx  *ui.Context

	// Everything that touches the outside world goes through these three, so
	// tests can run it inline: spawn starts background work, onMain brings
	// its result back to the UI thread, and invalidate asks for a redraw.
	spawn      func(func())
	onMain     func(func())
	invalidate func()
	// pick asks for a folder with the system's own dialog; blocking, so
	// it runs off the UI thread. Tests replace it.
	pick func(title string) string

	loader core.GamesLoader

	// games
	games     []core.GameEntry
	gamesLoad loadState
	gamesErr  string
	loadNote  string // what the scan is doing, under the loading bar
	query     string
	search    ui.Handle // the search box, so / and Ctrl+F can reach it
	offline   string    // the reason the catalog is not live, "" when it is

	// the selection
	view     view
	selected string // game id
	tab      int

	// the catalog, loaded once and refreshed on rescan
	data     core.WizardData
	dataLoad loadState
	dataErr  string

	sess        *core.Session
	sessGame    string
	showAll     bool // shader list: everything, not the shortlist
	addonAll    bool
	shaderQ     string
	addonQ      string
	renodxQ     string
	dllOverride bool // the ReShade tab shows the proxy DLL picker
	offlineOpen bool // the offline banner shows the reason

	dock dock

	res     resourcesView
	custom  customView
	setting settingsView
	add     addGameView

	uninstalling bool // the uninstall dialog is open
	adopting     bool

	// look
	themeMode    string // YARM_THEME for this run: "dark", "light", "" for the saved choice
	omarchy      *desktop.Look
	omarchyStamp string
}

// New builds the app state. Nothing is loaded until Start.
func New(opts Options) *App {
	a := &App{
		opts: opts, deps: opts.Deps, cfg: opts.Deps.Config,
		spawn:      func(f func()) { go f() },
		onMain:     mygo.RunOnMain,
		themeMode:  opts.Theme,
		gamesLoad:  stateIdle,
		invalidate: func() {},
		pick:       pickFolder,
	}
	if opts.NewLoader != nil {
		a.loader = opts.NewLoader(a.cfg)
	}
	return a
}

// Start is called once the window exists.
func (a *App) Start(win *mygo.Window) {
	a.win = win
	a.invalidate = win.Invalidate
	a.readOmarchy()
	a.rescan(false)
	go a.pollOmarchy()
}

// bg runs work off the UI thread and then done on it, with a redraw. The
// result travels in the closures' captures, never through shared state.
func (a *App) bg(work func(), done func()) {
	a.spawn(func() {
		work()
		a.onMain(func() {
			done()
			a.invalidate()
		})
	})
}

// toast shows a short message about what just happened.
func (a *App) toast(msg string) {
	if a.ctx != nil {
		a.ctx.Toast(msg)
	}
}

// ---- games ----

// rescan finds the games again. refreshCatalog also pulls the catalogs from
// the network, which is what Ctrl+R means; the first load uses what is on
// disk when it is fresh enough.
func (a *App) rescan(refreshCatalog bool) {
	if a.loader == nil || a.gamesLoad == stateLoading {
		return
	}
	a.gamesLoad = stateLoading
	a.loadNote = "Checking Steam, GOG, Epic and Battle.net"
	if inv, ok := a.loader.(interface{ InvalidateCache() }); ok {
		inv.InvalidateCache()
	}
	loader, deps := a.loader, a.deps

	var games []core.GameEntry
	var gerr error
	var data core.WizardData
	var derr error
	a.bg(func() {
		games, gerr = loader.LoadGames(context.Background())
		if refreshCatalog && deps.CatalogRefresh != nil {
			if err := deps.CatalogRefresh.RefreshCatalog(context.Background()); err != nil {
				slog.Warn("catalog refresh failed", "error", err)
			}
			// The memoized load still holds what the disk had before the
			// refresh; drop it so the load below sees the new catalogs.
			if r, ok := deps.WizardData.(interface{ Reset() }); ok {
				r.Reset()
			}
		}
		if deps.WizardData != nil {
			data, derr = deps.WizardData.LoadWizardData(context.Background())
		}
	}, func() {
		a.finishScan(games, gerr, data, derr)
	})
}

func (a *App) finishScan(games []core.GameEntry, gerr error, data core.WizardData, derr error) {
	if gerr != nil {
		a.gamesLoad, a.gamesErr = stateFailed, core.FriendlyError(gerr)
	} else {
		a.gamesLoad, a.gamesErr, a.games = stateReady, "", games
	}
	switch {
	case derr != nil && a.dataEmpty(data):
		a.dataLoad, a.dataErr = stateFailed, core.FriendlyError(derr)
		a.offline = a.dataErr
	default:
		a.dataLoad, a.dataErr, a.data = stateReady, "", data
		a.offline = ""
		if derr != nil {
			a.offline = core.FriendlyError(derr)
		}
	}
	a.sess, a.sessGame = nil, ""
	// The selected game may have vanished or changed on disk.
	if _, ok := a.entry(a.selected); !ok {
		a.selected = ""
	}
	if a.selected == "" && a.view == viewGame && len(a.games) > 0 {
		a.choose(a.firstGame().ID)
	}
	if a.gamesLoad == stateFailed {
		a.toast("Couldn't read your games: " + a.gamesErr)
	}
}

func (a *App) dataEmpty(d core.WizardData) bool {
	return len(d.Versions) == 0 && len(d.Packages) == 0 && len(d.Addons) == 0
}

// firstGame is the game selected when none is: the first installed one, else
// the first in the list.
func (a *App) firstGame() core.GameEntry {
	for _, g := range a.games {
		if g.InstalledSummary() != "" {
			return g
		}
	}
	return a.games[0]
}

func (a *App) entry(id string) (core.GameEntry, bool) {
	for _, g := range a.games {
		if g.ID == id {
			return g, true
		}
	}
	return core.GameEntry{}, false
}

// choose selects a game and shows its page.
func (a *App) choose(id string) {
	if a.dock.running {
		a.toast("An apply is running. Wait for it to finish, or cancel it.")
		return
	}
	if a.selected != id || a.view != viewGame {
		a.dock = dock{}
	}
	a.selected, a.view = id, viewGame
	a.sess, a.sessGame = nil, ""
	a.tab = 0
	a.shaderQ, a.addonQ, a.renodxQ = "", "", ""
	a.dllOverride = false
}

// session is the edit of the selected game, built on first use after a
// selection. nil when the game cannot be edited (a native build, no folders)
// or the catalog is not there yet.
func (a *App) session() *core.Session {
	if a.sess != nil && a.sessGame == a.selected {
		return a.sess
	}
	e, ok := a.entry(a.selected)
	if !ok || a.dataLoad != stateReady {
		return nil
	}
	installable, _ := core.SplitTargets(e)
	exe, ok := core.Reference(installable)
	if !ok {
		return nil
	}
	a.sess, a.sessGame = core.NewSession(e, exe, installable, a.deps, a.data), e.ID
	return a.sess
}

// ---- the rest of the window's life ----

// readOmarchy loads Omarchy's theme, if it is there.
func (a *App) readOmarchy() {
	if l, stamp, ok := desktop.ReadOmarchy(); ok {
		a.omarchy, a.omarchyStamp = &l, stamp
	}
}

// pollOmarchy notices a theme switch. Omarchy replaces the theme directory
// rather than editing it, and a switch between two dark themes changes
// nothing the desktop portal reports, so the stamp Omarchy writes after each
// switch is what yarm watches. While a switch is half done the read fails
// and the last look stays.
func (a *App) pollOmarchy() {
	for {
		time.Sleep(time.Second)
		l, stamp, ok := desktop.ReadOmarchy()
		if !ok {
			continue
		}
		mygo.RunOnMain(func() {
			if stamp != a.omarchyStamp {
				a.omarchy, a.omarchyStamp = &l, stamp
				a.invalidate()
			}
		})
	}
}

// providerLabel is the name of a game's store for its pill.
func providerLabel(id string) string {
	switch id {
	case "steam":
		return "Steam"
	case "gog":
		return "GOG"
	case "epic":
		return "Epic"
	case "battlenet":
		return "Battle.net"
	case "manual":
		return "Manual"
	}
	if id == "" {
		return ""
	}
	return strings.ToUpper(id[:1]) + id[1:]
}
