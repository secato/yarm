package app

import (
	"fmt"
	"strings"
	"testing"

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

// The folder picker is a list like any other and must stay inside the
// window, however many folders a game has.
func TestFolderPickerStaysInsideTheWindow(t *testing.T) {
	e := bigGame(12, 4)
	s := &FolderPickScreen{
		entry:  e,
		groups: e.Groups,
		deps:   fakeDeps(),
		keys:   DefaultKeyMap(),
		cursor: newCursorList(len(e.Groups), 0),
		verb:   verbInstall,
	}
	env := Env{Styles: NewStyles(true), Width: 80, Height: 21}

	body := s.View(env)
	if got := countLines(body); got > env.Height {
		t.Errorf("the picker rendered %d lines into a height of %d:\n%s", got, env.Height, body)
	}
	if !strings.Contains(body, "more below") {
		t.Errorf("a list cut short should say how much it hid:\n%s", body)
	}
	if !strings.Contains(body, "executable(s)") {
		t.Errorf("each folder should say what is in it, to choose between them:\n%s", body)
	}
}

// The side panel lists a few executables per folder and counts the rest:
// they are context for the folder, not things to act on individually, and
// the panel has a warning under them that must not be what gets clipped.
func TestSidePanelCapsExecutablesPerFolder(t *testing.T) {
	env := Env{Styles: NewStyles(true), Width: 120, Height: 40}
	body := panelText(t, bigGame(1, 12), env)

	if !strings.Contains(body, "+8 more") {
		t.Errorf("12 executables should list 4 and count the other 8:\n%s", body)
	}
	if strings.Contains(body, "tool11.exe") {
		t.Errorf("executables past the cap should not be listed:\n%s", body)
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
	s := NewCustomScreen("/cache/custom")
	s.loading = false
	for i := 0; i < 40; i++ {
		s.shaders = append(s.shaders, catalog.Custom{
			Name: fmt.Sprintf("Pack %02d", i), Path: fmt.Sprintf("/cache/custom/shaders/pack%02d", i),
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

// The panel is the only place a game's folders, install and executables
// are shown now, so it gets the width the table does not need — including
// whatever the name column leaves over once it hits its cap.
func TestSidePanelTakesTheWidthTheTableDoesNotNeed(t *testing.T) {
	entry := bigGame(1, 2)
	for _, width := range []int{100, 160, 220} {
		env := Env{Styles: NewStyles(true), Width: width, Height: 24}
		s := gamesWith(t, entry, env)

		if s.detailWidth < width*2/5 {
			t.Errorf("width %d: panel is %d columns, want at least two fifths", width, s.detailWidth)
		}
		if s.detailWidth > width-30 {
			t.Errorf("width %d: panel is %d columns, leaving too little for the table", width, s.detailWidth)
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
