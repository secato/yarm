package app

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/game"
)

// bigGame is a game shaped like a real Steam install: several folders,
// each with more executables than fit on screen.
func bigGame(folders, exesPerFolder int) GameEntry {
	e := GameEntry{Game: game.Game{
		ID: "steam:1", Name: "Sprawling Game", Provider: "steam", Root: "/games/Sprawl",
	}}
	for f := 0; f < folders; f++ {
		for x := 0; x < exesPerFolder; x++ {
			e.Exes = append(e.Exes, Executable{Executable: game.Executable{
				Path: fmt.Sprintf("bin%d/tool%02d.exe", f, x),
				Arch: game.ArchX64, API: game.APID3D11,
			}})
		}
	}
	return withGroups([]GameEntry{e})[0]
}

// The Paths pane is a list like any other and must stay inside the window,
// however many folders a game has.
func TestPathsListStaysInsideTheWindow(t *testing.T) {
	e := bigGame(12, 4)
	p := newPathsList(e.Groups, []string{e.Groups[0].Dir}, true)
	env := Env{Styles: NewStyles(true), Width: 80, Height: 21}

	var b strings.Builder
	writePathsList(&b, env, p, env.Height)
	body := b.String()
	if got := countLines(body); got > env.Height {
		t.Errorf("the list rendered %d lines into a height of %d:\n%s", got, env.Height, body)
	}
	if !strings.Contains(body, "more below") {
		t.Errorf("a list cut short should say how much it hid:\n%s", body)
	}
	// Collapsed by default: a game with many folders would otherwise push
	// most of them off screen just to list executables nobody asked to see.
	if !strings.Contains(body, "4 executables") {
		t.Errorf("a folder's executables should collapse to a count until expanded:\n%s", body)
	}
	if strings.Contains(body, "tool00.exe") {
		t.Errorf("an unexpanded folder should not list its executables:\n%s", body)
	}

	// Expanding the folder under the cursor lists its executables, capped
	// the same way the old picker capped them.
	p.toggleExpand()
	b.Reset()
	writePathsList(&b, env, p, env.Height)
	body = b.String()
	if !strings.Contains(body, "tool00.exe") {
		t.Errorf("expanding a folder should name its executables, to tell folders apart:\n%s", body)
	}
	if !strings.Contains(body, "+1 more") {
		t.Errorf("4 executables should list 3 and count the rest:\n%s", body)
	}
	if strings.Contains(body, "tool03.exe") {
		t.Errorf("the fourth executable should have been counted, not listed:\n%s", body)
	}
}

// The games list's side panel is a fixed-height box; its content must be
// clipped to fit rather than pushing the panel's own border off-screen.
func TestGamesDetailPanelIsClippedToItsHeight(t *testing.T) {
	m := New(NewGamesScreen(fakeLoader{entries: []GameEntry{bigGame(4, 12)}}, fakeDeps(), false))
	m = drive(t, m,
		tea.WindowSizeMsg{Width: 100, Height: 24},
		gamesLoadedMsg{entries: []GameEntry{bigGame(4, 12)}},
	)

	body := m.render()
	if got := strings.Count(body, "\n") + 1; got > 24 {
		t.Errorf("the home screen rendered %d lines into a 24-row terminal:\n%s", got, body)
	}
	for _, line := range strings.Split(body, "\n") {
		if lipgloss.Width(line) > 100 {
			t.Errorf("line is %d columns wide:\n%q", lipgloss.Width(line), line)
		}
	}
}

// The custom-content screen windows its two sections rather than printing
// every folder the user has dropped in.
func TestCustomScreenWindowsLongLists(t *testing.T) {
	s := NewCustomScreen("/data/custom")
	s.loading = false
	for i := 0; i < 40; i++ {
		s.shaders = append(s.shaders, catalog.Custom{
			Name: fmt.Sprintf("Pack %02d", i), Path: fmt.Sprintf("/data/custom/shaders/pack%02d", i),
		})
	}
	s.buildRows()
	s.cursor = 30

	env := Env{Styles: NewStyles(true), Width: 80, Height: 21}
	body := s.View(env)
	if got := countLines(body); got > env.Height {
		t.Errorf("custom screen rendered %d lines into a height of %d:\n%s", got, env.Height, body)
	}
	if !strings.Contains(body, "more above") {
		t.Errorf("a windowed list should say how many rows are hidden:\n%s", body)
	}
}

// The panel and the table split the width proportionally, and neither
// holds any of it back: whatever is not the panel is table, and whatever
// the table's fixed columns do not use is name column. A cap on either
// side would show up as a gap between the two.
func TestPanelAndTableSplitTheWholeWidth(t *testing.T) {
	entry := bigGame(1, 2)
	for _, width := range []int{100, 160, 220} {
		env := Env{Styles: NewStyles(true), Width: width, Height: 24}
		s := gamesWith(t, entry, env)

		if want := width * 2 / 5; s.detailWidth != want {
			t.Errorf("width %d: panel is %d columns, want %d", width, s.detailWidth, want)
		}

		tableWidth := width - s.detailWidth - 2
		used := 6
		for _, c := range gamesColumns(tableWidth) {
			used += c.Width
		}
		if used != tableWidth {
			t.Errorf("width %d: the table's columns use %d of the %d columns it was given, leaving a %d-column gap",
				width, used, tableWidth, tableWidth-used)
		}

		if got := lipgloss.Width(s.View(env)); got > width {
			t.Errorf("the games screen rendered %d columns into a width of %d", got, width)
		}
	}
}

// Below the width where a panel is worth having, it is dropped entirely
// rather than squeezed into something unreadable.
func TestSidePanelIsDroppedWhenTooNarrow(t *testing.T) {
	env := Env{Styles: NewStyles(true), Width: 60, Height: 24}
	if s := gamesWith(t, bigGame(1, 2), env); s.detailWidth != 0 {
		t.Errorf("panel width = %d at 60 columns, want it dropped", s.detailWidth)
	}
}

// bubbles' own table styles hardcode a pink for the selected row and give
// the header no color, so a table left on them is the one list in the app
// that ignores the terminal's theme and highlights its cursor differently
// from every other list.
func TestGamesTableUsesTheAppPalette(t *testing.T) {
	for _, dark := range []bool{true, false} {
		styles := NewStyles(dark)
		got := styles.Table()

		if got.Selected.Render("x") != styles.Selected.Render("x") {
			t.Errorf("dark=%v: the table's selected row should be styled like every other list's", dark)
		}
		if !strings.Contains(got.Header.Render("Game"), "\x1b[") {
			t.Errorf("dark=%v: the header should carry the palette's own color", dark)
		}
		// Columns are laid out assuming one column of padding per side.
		if got.Cell.GetPaddingLeft() != 1 || got.Cell.GetPaddingRight() != 1 {
			t.Errorf("dark=%v: cells need the padding the layout assumes", dark)
		}
	}
}

// A cell that sets its own color emits a reset that also clears the row
// highlight bubbles wraps around it, striping the selection.
func TestGamesTableSelectionIsOneUnbrokenHighlight(t *testing.T) {
	env := Env{Styles: NewStyles(true), Width: 60, Height: 8}
	entry := bigGame(1, 1)
	s := gamesWith(t, entry, env)

	var row string
	for _, line := range strings.Split(s.View(env), "\n") {
		if strings.Contains(line, entry.Name) && strings.Contains(line, "\x1b[") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("no styled row for %q in:\n%s", entry.Name, s.View(env))
	}
	if n := strings.Count(row, "\x1b[m"); n != 1 {
		t.Errorf("the selected row resets its styling %d times; it should be one highlight:\n%q", n, row)
	}
}

// Navigation is arrows-only: the vim aliases are gone, including the ones
// bubbles' table brings with it, which also answered to letters this
// screen gives to other actions (f, b, u, d, g) and to space.
func TestNavigationIsArrowsOnly(t *testing.T) {
	keys := DefaultKeyMap()
	for _, tc := range []struct {
		name string
		b    key.Binding
		want []string
	}{
		{"Up", keys.Up, []string{"up"}},
		{"Down", keys.Down, []string{"down"}},
		{"wizard pane left", wizardPaneLeft, []string{"left"}},
		{"wizard pane right", wizardPaneRight, []string{"right"}},
		{"resources pane left", resourcePaneLeft, []string{"left"}},
		{"resources pane right", resourcePaneRight, []string{"right"}},
	} {
		if got := tc.b.Keys(); !slices.Equal(got, tc.want) {
			t.Errorf("%s is bound to %v, want %v", tc.name, got, tc.want)
		}
	}

	table := arrowKeyMap()
	for _, tc := range []struct {
		name string
		b    key.Binding
	}{{"LineUp", table.LineUp}, {"LineDown", table.LineDown}, {"PageUp", table.PageUp}, {"PageDown", table.PageDown}} {
		for _, k := range tc.b.Keys() {
			if len(k) == 1 {
				t.Errorf("the table's %s still answers to the letter %q", tc.name, k)
			}
		}
	}
	if table.PageDown.Enabled() && slices.Contains(table.PageDown.Keys(), "space") {
		t.Error("space pages the table, but means \"toggle\" everywhere else in the app")
	}
}

// Every row of text reaches the screen through one of these two, so
// neither may pass an escape sequence through to a renderer that honors
// SGR colors and OSC 8 hyperlinks.
func TestClipHelpersStripControlCharacters(t *testing.T) {
	const hostile = "\x1b[31mELDEN\rRING\x1b]8;;https://evil.test\x07"

	if got := clipTail(hostile, 200); strings.ContainsAny(got, "\x1b\r\x07") {
		t.Errorf("clipTail() = %q, still holds control characters", got)
	}
	if got := truncate(hostile, 200); strings.ContainsAny(got, "\x1b\r\x07") {
		t.Errorf("truncate() = %q, still holds control characters", got)
	}
	// A name with nothing to strip is untouched, ellipsis rules included.
	if got, want := clipTail("ELDEN RING", 200), "ELDEN RING"; got != want {
		t.Errorf("clipTail() = %q, want %q", got, want)
	}
}
