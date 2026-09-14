package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

// drive feeds messages through the root model, resolving the control
// messages that drive navigation.
//
// It deliberately does not chase every command: a focused text input
// returns textinput.Blink, a timer that regenerates itself, so following
// commands blindly would never terminate. Only the app's own control
// messages are followed, which is what navigation is made of.
func drive(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()

	var cur tea.Model = m
	var apply func(cmd tea.Cmd, depth int)

	apply = func(cmd tea.Cmd, depth int) {
		if cmd == nil || depth > 8 {
			return
		}
		produced := cmd()
		switch msg := produced.(type) {
		case nil:
			return
		case tea.BatchMsg:
			for _, c := range msg {
				apply(c, depth+1)
			}
		case pushScreenMsg, popScreenMsg, popToRootMsg, statusMsg, errorMsg, showOverlayMsg, uninstallDoneMsg, adoptDoneMsg,
			progressUpdateMsg, applyDoneMsg:
			next, follow := cur.Update(msg)
			cur = next
			apply(follow, depth+1)
		default:
			// Anything else (timers, blinks, widget internals) is not
			// what these tests are about.
		}
	}

	for _, msg := range msgs {
		next, cmd := cur.Update(msg)
		cur = next
		apply(cmd, 0)
	}
	return cur.(Model)
}

func loaded(t *testing.T) Model {
	t.Helper()
	m := New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps(), false))
	return drive(t, m,
		tea.WindowSizeMsg{Width: termWidth, Height: termHeight},
		gamesLoadedMsg{entries: sampleEntries()},
	)
}

// The window size arrives before the async scan finishes, so the table is
// sized while it has no rows. It parks its cursor at -1 there; if that is
// not reset when rows arrive, the first game is never selected and enter
// silently does nothing.
func TestCursorRecoversAfterEmptyResize(t *testing.T) {
	m := loaded(t)
	gs := m.Screen().(*GamesScreen)

	if got := gs.table.Cursor(); got != 0 {
		t.Fatalf("cursor = %d, want 0 after rows arrive", got)
	}
	if _, ok := gs.selected(); !ok {
		t.Error("no game is selected, so enter would do nothing")
	}
}

// Enter does the obvious thing to the highlighted game rather than opening
// a screen to look at it: a game with one folder and no install goes
// straight to the wizard.
func TestEnterActsOnTheGameAndEscReturns(t *testing.T) {
	m := loaded(t)

	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := m.Screen().(*WizardScreen); !ok {
		t.Fatalf("after enter the screen is %T, want *WizardScreen", m.Screen())
	}

	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := m.Screen().(*GamesScreen); !ok {
		t.Fatalf("after esc the screen is %T, want *GamesScreen", m.Screen())
	}
}

func TestAddFolderOpensAndCancels(t *testing.T) {
	m := loaded(t)

	m = drive(t, m, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if _, ok := m.Screen().(*AddFolderScreen); !ok {
		t.Fatalf("after a the screen is %T, want *AddFolderScreen", m.Screen())
	}

	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := m.Screen().(*GamesScreen); !ok {
		t.Fatalf("after esc the screen is %T, want *GamesScreen", m.Screen())
	}
}

// While a text input has focus, plain letters must reach it rather than
// firing global single-key bindings.
func TestFilterSwallowsGlobalKeys(t *testing.T) {
	m := loaded(t)
	m = drive(t, m, tea.KeyPressMsg{Code: '/', Text: "/"})

	gs := m.Screen().(*GamesScreen)
	if !gs.CapturesInput() {
		t.Fatal("the filter should capture input once opened")
	}

	// "q" would normally quit and "a" would open add-folder.
	m = drive(t, m,
		tea.KeyPressMsg{Code: 'q', Text: "q"},
		tea.KeyPressMsg{Code: 'a', Text: "a"},
	)

	if m.quitting {
		t.Error("typing q into the filter quit the program")
	}
	if _, ok := m.Screen().(*GamesScreen); !ok {
		t.Fatalf("typing into the filter navigated to %T", m.Screen())
	}
	if got := m.Screen().(*GamesScreen).filter.Value(); got != "qa" {
		t.Errorf("filter value = %q, want %q", got, "qa")
	}
}

func TestFilterNarrowsAndClears(t *testing.T) {
	m := loaded(t)
	m = drive(t, m, tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "ridge" {
		m = drive(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	gs := m.Screen().(*GamesScreen)
	if len(gs.filtered) != 1 || gs.filtered[0].Name != "Ridgeline" {
		t.Fatalf("filtered = %d entries, want just Ridgeline", len(gs.filtered))
	}
	if got := gs.Title(); got != "games — 1 of 4" {
		t.Errorf("title = %q", got)
	}

	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	gs = m.Screen().(*GamesScreen)
	if len(gs.filtered) != 4 {
		t.Errorf("esc should clear the filter, got %d entries", len(gs.filtered))
	}
}

// Filtering down must not leave the cursor pointing past the end.
func TestFilterClampsCursor(t *testing.T) {
	m := loaded(t)
	gs := m.Screen().(*GamesScreen)
	gs.table.SetCursor(3)

	m = drive(t, m, tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "ridge" {
		m = drive(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	gs = m.Screen().(*GamesScreen)
	if c := gs.table.Cursor(); c < 0 || c >= len(gs.filtered) {
		t.Errorf("cursor = %d, out of range for %d filtered rows", c, len(gs.filtered))
	}
	if _, ok := gs.selected(); !ok {
		t.Error("nothing selected after filtering")
	}
}

// Esc at the top level must not pop past the home screen.
func TestEscAtRootDoesNothing(t *testing.T) {
	m := loaded(t)
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if _, ok := m.Screen().(*GamesScreen); !ok {
		t.Fatalf("esc at the root navigated to %T", m.Screen())
	}
}

// An overlay is modal: keys go to it, not to the screen behind.
func TestOverlayIsModal(t *testing.T) {
	m := loaded(t)
	m = drive(t, m, tea.KeyPressMsg{Code: '?', Text: "?"})
	if m.overlay == nil {
		t.Fatal("? should open the help overlay")
	}

	// "a" would open add-folder if it reached the screen.
	m = drive(t, m, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if m.overlay != nil {
		t.Error("any key should dismiss the help overlay")
	}
	if _, ok := m.Screen().(*GamesScreen); !ok {
		t.Fatalf("a key consumed by the overlay still navigated to %T", m.Screen())
	}
}

func TestConfirmOverlayRunsActionOnYes(t *testing.T) {
	m := loaded(t)

	ran := false
	action := func() tea.Msg {
		ran = true
		return statusMsg{text: "done"}
	}

	m = drive(t, m, showOverlayMsg{overlay: confirmOverlay{
		question: "Delete everything?", keys: DefaultKeyMap(), onYes: action,
	}})
	if m.overlay == nil {
		t.Fatal("the confirm overlay should be open")
	}

	m = drive(t, m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if ran {
		t.Error("answering no ran the action")
	}
	if m.overlay != nil {
		t.Error("answering no should close the overlay")
	}

	m = drive(t, m, showOverlayMsg{overlay: confirmOverlay{
		question: "Delete everything?", keys: DefaultKeyMap(), onYes: action,
	}})
	m = drive(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !ran {
		t.Error("answering yes did not run the action")
	}
	if m.status != "done" {
		t.Errorf("status = %q, want the action's message", m.status)
	}
}

// A confirm dialog must never treat Enter as "yes" — a destructive action
// (uninstall, delete, adopt) should not fire from a leftover Enter press
// carried in from whatever the user was doing on the previous screen.
func TestConfirmOverlayEnterDoesNotRunAction(t *testing.T) {
	m := loaded(t)

	ran := false
	action := func() tea.Msg {
		ran = true
		return statusMsg{text: "done"}
	}

	m = drive(t, m, showOverlayMsg{overlay: confirmOverlay{
		question: "Delete everything?", keys: DefaultKeyMap(), onYes: action,
	}})
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if ran {
		t.Error("Enter ran the confirm action; it must default to no")
	}
	if m.overlay != nil {
		t.Error("Enter should close the overlay as a cancel, not leave it open")
	}
}

// The theme is chosen from what the terminal reports, not guessed.
func TestBackgroundColorSetsTheme(t *testing.T) {
	m := loaded(t)
	dark := m.styles

	m = drive(t, m, tea.BackgroundColorMsg{Color: lightBG{}})
	if m.styles.Title.GetForeground() == dark.Title.GetForeground() {
		t.Error("a light background should produce a different palette")
	}
}

// lightBG is a stand-in for a light terminal background.
type lightBG struct{}

func (lightBG) RGBA() (r, g, b, a uint32) { return 0xffff, 0xffff, 0xffff, 0xffff }

// Rendering must not panic before the first window size arrives.
func TestRenderBeforeReady(t *testing.T) {
	m := New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps(), false))
	if got := m.View().Content; got == "" {
		t.Error("the pre-ready view should say something")
	}
}

func TestViewSetsAltScreenAndTitle(t *testing.T) {
	m := loaded(t)
	v := m.View()
	if !v.AltScreen {
		t.Error("the TUI should run in the alternate screen")
	}
	if v.WindowTitle != "yarm" {
		t.Errorf("window title = %q", v.WindowTitle)
	}
}

// A pushed screen has never seen a WindowSizeMsg — Bubble Tea only sends
// one on a real resize — so it must still render correctly from Env alone.
func TestPushedScreenIsSized(t *testing.T) {
	m := loaded(t)
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	ds, ok := m.Screen().(*WizardScreen)
	if !ok {
		t.Fatalf("screen is %T, want *WizardScreen", m.Screen())
	}

	// It has never been sized by a WindowSizeMsg, so anything it renders
	// has to come from Env — and it must not overflow it.
	body := ds.View(m.env())
	if body == "" {
		t.Error("the pushed screen rendered nothing")
	}
	if got := countLines(body); got > m.env().Height {
		t.Errorf("rendered %d lines into a height of %d:\n%s", got, m.env().Height, body)
	}
}

// A screen buried on the stack may have missed a resize.
func TestPoppedScreenIsResized(t *testing.T) {
	m := loaded(t)
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	// Shrink the window while the detail screen is in front.
	m = drive(t, m, tea.WindowSizeMsg{Width: 60, Height: 20})
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

	gs, ok := m.Screen().(*GamesScreen)
	if !ok {
		t.Fatalf("screen is %T, want *GamesScreen", m.Screen())
	}
	if got := gs.table.Width(); got > 60 {
		t.Errorf("table width = %d after the window shrank to 60", got)
	}
}

// The uninstall confirm flow, end to end through the root model: press u
// on an installed executable, confirm, and land on a Result screen.
func TestUninstallConfirmFlow(t *testing.T) {
	m := loaded(t)
	// Navigate to Ember Hollow (index 2 of sampleEntries), which has an
	// install recorded on its first executable.
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyDown})

	gs, ok := m.Screen().(*GamesScreen)
	if !ok {
		t.Fatalf("screen = %T, want *GamesScreen", m.Screen())
	}
	if e, _ := gs.selected(); e.Name != "Ember Hollow" {
		t.Fatalf("selected = %q, want Ember Hollow", e.Name)
	}

	m = drive(t, m, tea.KeyPressMsg{Code: 'u', Text: "u"})
	if m.overlay == nil {
		t.Fatal("'u' on an installed executable should open a confirm overlay")
	}

	m = drive(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if m.overlay != nil {
		t.Error("confirming should close the overlay")
	}

	rs, ok := m.Screen().(*ResultScreen)
	if !ok {
		t.Fatalf("screen after confirming = %T, want *ResultScreen", m.Screen())
	}
	if !rs.ok {
		t.Errorf("result should report success, lines = %v", rs.lines)
	}
}

// 'u' must do nothing on an executable with no recorded install — there
// is nothing to confirm.
func TestUninstallKeyNoOpWithoutInstall(t *testing.T) {
	m := loaded(t) // cursor starts on Vantage Point Deluxe, uninstalled

	m = drive(t, m, tea.KeyPressMsg{Code: 'u', Text: "u"})
	if m.overlay != nil {
		t.Error("'u' on a not-installed executable should not open a confirm overlay")
	}
}

// Pressing i opens the wizard on the highlighted executable.
func TestInstallKeyOpensWizard(t *testing.T) {
	m := loaded(t)
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}) // -> Vantage Point's detail

	m = drive(t, m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	if _, ok := m.Screen().(*WizardScreen); !ok {
		t.Fatalf("screen after 'i' = %T, want *WizardScreen", m.Screen())
	}
}

// A single-folder game can be installed into directly from the games
// list, without drilling into the detail screen first.
func TestGamesScreenInstallDirectlyFromList(t *testing.T) {
	m := loaded(t) // cursor starts on Vantage Point Deluxe, single folder, uninstalled

	m = drive(t, m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	if _, ok := m.Screen().(*WizardScreen); !ok {
		t.Fatalf("screen after 'i' from the list = %T, want *WizardScreen", m.Screen())
	}
}

// A single-folder game that already has an install can be edited directly
// from the list with 'e'.
func TestGamesScreenEditDirectlyFromList(t *testing.T) {
	m := loaded(t)
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyDown}) // -> Ember Hollow, installed

	m = drive(t, m, tea.KeyPressMsg{Code: 'e', Text: "e"})
	wiz, ok := m.Screen().(*WizardScreen)
	if !ok {
		t.Fatalf("screen after 'e' from the list = %T, want *WizardScreen", m.Screen())
	}
	if got := wiz.exe.Path; got != "Game/emberhollow.exe" {
		t.Errorf("wizard targets %q, want the actually-installed exe", got)
	}
}

// A single-folder game that already has an install can be uninstalled
// directly from the list with 'u'.
func TestGamesScreenUninstallDirectlyFromList(t *testing.T) {
	m := loaded(t)
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyDown}) // -> Ember Hollow, installed

	m = drive(t, m, tea.KeyPressMsg{Code: 'u', Text: "u"})
	if m.overlay == nil {
		t.Fatal("'u' from the list on an installed game should open a confirm overlay")
	}

	m = drive(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if _, ok := m.Screen().(*ResultScreen); !ok {
		t.Fatalf("screen after confirming = %T, want *ResultScreen", m.Screen())
	}
}

// A game with more than one folder has no single folder to edit or
// uninstall from the list — but install still works, by opening the folder
// list first. Offering the key for one-folder games and silently dropping
// it for the rest reads as the action being unavailable, not as needing one
// more step.
func TestGamesScreenMultiFolderGameOffersInstallButNotEditOrUninstall(t *testing.T) {
	installed := state.Install{Exe: "Release/Game.exe", ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "normal"}}
	exes := []Executable{
		{Executable: game.Executable{Path: "Release/Game.exe"}, Installed: &installed},
		{Executable: game.Executable{Path: "Ship/Game.exe"}},
	}
	entry := GameEntry{
		Game:   game.Game{ID: "manual:x", Name: "Two Folders", Root: "/games/two"},
		Exes:   exes,
		Groups: groupByFolder("/games/two", exes),
	}
	if len(entry.Groups) != 2 {
		t.Fatalf("setup: groups = %d, want 2", len(entry.Groups))
	}

	m := New(NewGamesScreen(fakeLoader{entries: []GameEntry{entry}}, fakeDeps(), false))
	m = drive(t, m,
		tea.WindowSizeMsg{Width: termWidth, Height: termHeight},
		gamesLoadedMsg{entries: []GameEntry{entry}},
	)

	gs := m.Screen().(*GamesScreen)
	offered := map[string]bool{}
	for _, b := range gs.KeyBindings() {
		offered[b.Help().Desc] = true
	}
	for _, want := range []string{"install ReShade", "edit install", "uninstall"} {
		if !offered[want] {
			t.Errorf("%q should be offered; bindings = %v", want, offered)
		}
	}

	// Pressing one opens the wizard directly — which folder(s) it applies
	// to is the wizard's own first step, not a screen in front of it.
	m = drive(t, m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	wiz, ok := m.Screen().(*WizardScreen)
	if !ok {
		t.Fatalf("screen after i = %T, want *WizardScreen", m.Screen())
	}
	if len(wiz.paths.groups) != 2 {
		t.Errorf("wizard's Paths step lists %d folder(s), want 2", len(wiz.paths.groups))
	}
}

// PopToRoot must discard the whole navigation stack — however deep it
// is — and reload the screen at the bottom, not merely pop one level.
// This is what a finished install or uninstall uses to get back to a
// games list that reflects what just changed.
func TestPopToRootClearsStackAndReloads(t *testing.T) {
	m := loaded(t)
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}) // -> Wizard
	if _, ok := m.Screen().(*WizardScreen); !ok {
		t.Fatalf("setup: screen = %T, want *WizardScreen", m.Screen())
	}

	m = drive(t, m, popToRootMsg{})

	if _, ok := m.Screen().(*GamesScreen); !ok {
		t.Fatalf("after PopToRoot, screen = %T, want *GamesScreen", m.Screen())
	}
	if got := len(m.stack); got != 0 {
		t.Errorf("stack has %d entries after PopToRoot, want 0", got)
	}
}

// The adopt confirm flow, end to end through the root model: press a on an
// unmanaged folder, confirm, and land on a Result screen. ScanUnmanaged
// probes the real filesystem, so this entry points at a real temp
// directory holding a minimal ReShade install; the actual recording is
// faked so the test does not depend on installs.json.
func TestAdoptConfirmFlow(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"dxgi.dll":        "dll body",
		install.ININame:   "[GENERAL]\n",
		"emberhollow.exe": "the game",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	exes := []Executable{{
		Executable: game.Executable{Path: "emberhollow.exe", Arch: game.ArchX64, API: game.APID3D12},
	}}
	entry := GameEntry{
		Game:   game.Game{ID: "manual:x", Name: "Manual Game", Provider: "manual", Root: dir},
		Exes:   exes,
		Groups: groupByFolder(dir, exes),
	}

	m := New(NewGamesScreen(fakeLoader{entries: []GameEntry{entry}}, fakeDeps(), false))
	m = drive(t, m,
		tea.WindowSizeMsg{Width: termWidth, Height: termHeight},
		gamesLoadedMsg{entries: []GameEntry{entry}},
	)
	// Adopting has no key of its own: `a` adds a folder, and install
	// redirects to adopting when the folder's ReShade is not yarm's.
	m = drive(t, m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	if m.overlay == nil {
		t.Fatal("i on an unmanaged folder should open the adopt confirmation")
	}

	m = drive(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if m.overlay != nil {
		t.Error("confirming should close the overlay")
	}

	rs, ok := m.Screen().(*ResultScreen)
	if !ok {
		t.Fatalf("screen after confirming = %T, want *ResultScreen", m.Screen())
	}
	if !rs.ok {
		t.Errorf("result should report success, lines = %v", rs.lines)
	}
}

// 'a' must do nothing on a folder that is not flagged unmanaged — there
// is nothing to confirm.
func TestAdoptKeyNoOpWithoutUnmanaged(t *testing.T) {
	m := loaded(t) // cursor starts on Vantage Point Deluxe, not unmanaged
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	m = drive(t, m, tea.KeyPressMsg{Code: 'a', Text: "a"})
	if m.overlay != nil {
		t.Error("'a' on a not-unmanaged folder should not open a confirm overlay")
	}
}

// The welcome banner shows once, only on a genuine first run, counts
// what each provider found, and disappears on the very next keypress
// without swallowing it.
func TestFirstRunWelcomeBanner(t *testing.T) {
	m := New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps(), true))
	m = drive(t, m,
		tea.WindowSizeMsg{Width: termWidth, Height: termHeight},
		gamesLoadedMsg{entries: sampleEntries()},
	)

	gs := m.Screen().(*GamesScreen)
	if !gs.firstRun {
		t.Fatal("firstRun should still be true before any keypress")
	}
	body := gs.View(m.env())
	if !strings.Contains(body, "Welcome to yarm") {
		t.Errorf("the welcome banner should be shown:\n%s", body)
	}
	// sampleEntries has 3 steam-provider games (Vantage Point, Ridgeline,
	// Ember Hollow RING) and 1 manual one; the banner counts both, with
	// a per-provider breakdown.
	if !strings.Contains(body, "Found 4 games (manual 1, steam 3)") {
		t.Errorf("banner should break the count down per provider:\n%s", body)
	}

	// Any keypress dismisses it — and still does what it would normally
	// do (down moves the table cursor).
	before := gs.table.Cursor()
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	gs = m.Screen().(*GamesScreen)
	if gs.firstRun {
		t.Error("firstRun should be false after the first keypress")
	}
	if gs.table.Cursor() == before {
		t.Error("the dismissing keypress should still move the cursor")
	}
	if strings.Contains(gs.View(m.env()), "Welcome to yarm") {
		t.Error("the banner should no longer render after being dismissed")
	}
}

// A fresh install with no Steam library and nothing added yet must still
// show the banner in the empty-state view.
func TestFirstRunWelcomeBannerWithNoGames(t *testing.T) {
	m := New(NewGamesScreen(fakeLoader{}, fakeDeps(), true))
	m = drive(t, m,
		tea.WindowSizeMsg{Width: termWidth, Height: termHeight},
		gamesLoadedMsg{entries: nil},
	)

	gs := m.Screen().(*GamesScreen)
	body := gs.View(m.env())
	if !strings.Contains(body, "Welcome to yarm") {
		t.Errorf("the banner should show even with zero games found:\n%s", body)
	}
	if !strings.Contains(body, "No games were found") {
		t.Errorf("banner should say no games were found:\n%s", body)
	}
}

// A normal (non-first) run must never show the banner.
func TestNoWelcomeBannerOnNormalRun(t *testing.T) {
	m := loaded(t) // fakeDeps()/firstRun defaults to false via `loaded`
	gs := m.Screen().(*GamesScreen)
	if strings.Contains(gs.View(m.env()), "Welcome to yarm") {
		t.Error("a normal run should never show the welcome banner")
	}
}

// Below the hard floor, no screen's layout degrades gracefully — the shell
// must say so plainly instead of drawing something truncated or garbled.
func TestTooSmallTerminalShowsHonestMessage(t *testing.T) {
	m := New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps(), false))
	m = drive(t, m,
		tea.WindowSizeMsg{Width: 20, Height: 5},
		gamesLoadedMsg{entries: sampleEntries()},
	)

	body := m.render()
	if !strings.Contains(body, "terminal too small") {
		t.Errorf("a 20x5 terminal should show the too-small message, got:\n%s", body)
	}

	m = drive(t, m, tea.WindowSizeMsg{Width: termWidth, Height: termHeight})
	if strings.Contains(m.render(), "terminal too small") {
		t.Error("growing back above the floor should stop showing the too-small message")
	}
}

// The status bar is one row. The resources screen has enough bindings to
// overflow a normal terminal, and a wrapped status line makes the whole
// frame taller than the window — which scrolls the header out of view.
func TestStatusLineNeverWraps(t *testing.T) {
	m := New(NewResourcesScreen(Deps{}))
	for _, width := range []int{40, 60, 80, 100, 160} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		got := next.(Model).render()
		for _, line := range strings.Split(got, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("at width %d a line is %d columns wide: %q", width, w, line)
			}
		}
	}
}
