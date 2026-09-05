package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
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
		case pushScreenMsg, popScreenMsg, popToRootMsg, statusMsg, errorMsg, showOverlayMsg, uninstallDoneMsg, adoptDoneMsg:
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

func TestEnterOpensDetailAndEscReturns(t *testing.T) {
	m := loaded(t)

	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := m.Screen().(*GameDetailScreen); !ok {
		t.Fatalf("after enter the screen is %T, want *GameDetailScreen", m.Screen())
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
	for _, r := range "dota" {
		m = drive(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	gs := m.Screen().(*GamesScreen)
	if len(gs.filtered) != 1 || gs.filtered[0].Name != "Dota 2" {
		t.Fatalf("filtered = %d entries, want just Dota 2", len(gs.filtered))
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
	for _, r := range "dota" {
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
// one on a real resize — so without the shell handing it the current size
// its table holds no columns or rows and the screen renders blank.
func TestPushedScreenIsSized(t *testing.T) {
	m := loaded(t)
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	ds, ok := m.Screen().(*GameDetailScreen)
	if !ok {
		t.Fatalf("screen is %T, want *GameDetailScreen", m.Screen())
	}

	if got := len(ds.table.Columns()); got == 0 {
		t.Error("the pushed screen has no table columns, so it would render blank")
	}
	if got := len(ds.table.Rows()); got == 0 {
		t.Error("the pushed screen has no table rows")
	}

	body := ds.View(m.env())
	if !strings.Contains(body, "Control.exe") {
		t.Errorf("the detail table did not render its executables:\n%s", body)
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
	// Navigate to ELDEN RING (index 2 of sampleEntries), which has an
	// install recorded on its first executable.
	m = drive(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = drive(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	gd, ok := m.Screen().(*GameDetailScreen)
	if !ok || gd.entry.Name != "ELDEN RING" {
		t.Fatalf("screen = %T (%q), want *GameDetailScreen for ELDEN RING", m.Screen(), gd.entry.Name)
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
	m := loaded(t) // cursor starts on Control Ultimate Edition, uninstalled
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if _, ok := m.Screen().(*GameDetailScreen); !ok {
		t.Fatalf("screen = %T, want *GameDetailScreen", m.Screen())
	}

	m = drive(t, m, tea.KeyPressMsg{Code: 'u', Text: "u"})
	if m.overlay != nil {
		t.Error("'u' on a not-installed executable should not open a confirm overlay")
	}
}

// Pressing i opens the wizard on the highlighted executable.
func TestInstallKeyOpensWizard(t *testing.T) {
	m := loaded(t)
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}) // -> Control's detail

	m = drive(t, m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	if _, ok := m.Screen().(*WizardScreen); !ok {
		t.Fatalf("screen after 'i' = %T, want *WizardScreen", m.Screen())
	}
}

// PopToRoot must discard the whole navigation stack — however deep it
// is — and reload the screen at the bottom, not merely pop one level.
// This is what a finished install or uninstall uses to get back to a
// games list that reflects what just changed.
func TestPopToRootClearsStackAndReloads(t *testing.T) {
	m := loaded(t)
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})   // -> GameDetail
	m = drive(t, m, tea.KeyPressMsg{Code: 'i', Text: "i"}) // -> Wizard
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

// The adopt confirm flow, end to end through the root model: press m on an
// unmanaged executable, confirm, and land on a Result screen. ScanUnmanaged
// probes the real filesystem, so this entry points at a real temp
// directory holding a minimal ReShade install; the actual recording is
// faked so the test does not depend on installs.json.
func TestAdoptConfirmFlow(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"dxgi.dll":      "dll body",
		install.ININame: "[GENERAL]\n",
		"eldenring.exe": "the game",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	exes := []Executable{{
		Executable: game.Executable{Path: "eldenring.exe", Arch: game.ArchX64, API: game.APID3D12},
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
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if _, ok := m.Screen().(*GameDetailScreen); !ok {
		t.Fatalf("screen = %T, want *GameDetailScreen", m.Screen())
	}

	m = drive(t, m, tea.KeyPressMsg{Code: 'm', Text: "m"})
	if m.overlay == nil {
		t.Fatal("'m' on an unmanaged executable should open a confirm overlay")
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

// 'm' must do nothing on an executable that is not flagged unmanaged —
// there is nothing to confirm.
func TestManageKeyNoOpWithoutUnmanaged(t *testing.T) {
	m := loaded(t) // cursor starts on Control Ultimate Edition, not unmanaged
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	m = drive(t, m, tea.KeyPressMsg{Code: 'm', Text: "m"})
	if m.overlay != nil {
		t.Error("'m' on a not-unmanaged executable should not open a confirm overlay")
	}
}

// The welcome banner shows once, only on a genuine first run, counts
// Steam-provided games specifically (not manual ones), and disappears on
// the very next keypress without swallowing it.
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
	// sampleEntries has 3 steam-provider games (Control, Dota 2, ELDEN
	// RING) and 1 manual one; the banner counts only the Steam ones.
	if !strings.Contains(body, "Steam found 3 games") {
		t.Errorf("banner should count only Steam-provided games:\n%s", body)
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
	if !strings.Contains(body, "No Steam library was found") {
		t.Errorf("banner should say no Steam library was found:\n%s", body)
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
