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

// The detail view must stay inside the window however many folders and
// executables a game has — falling back to the folder under the cursor,
// which is the one every action on this screen applies to.
func TestGameDetailStaysInsideTheWindow(t *testing.T) {
	s := NewGameDetailScreen(bigGame(4, 12), fakeDeps())
	env := Env{Styles: NewStyles(true), Width: 80, Height: 21}

	body := s.View(env)
	if got := countLines(body); got > env.Height {
		t.Errorf("detail view rendered %d lines into a height of %d:\n%s", got, env.Height, body)
	}
	if !strings.Contains(body, "more folder(s) below") {
		t.Errorf("a list cut short should say how many folders are hidden:\n%s", body)
	}

	// The cursor scrolls the list: moving down brings the folders above
	// into the hidden count.
	s.cursor.down()
	body = s.View(env)
	if !strings.Contains(body, "↑ 1 more folder(s) above") {
		t.Errorf("moving the cursor down should scroll the folder list:\n%s", body)
	}
	if got := countLines(body); got > env.Height {
		t.Errorf("scrolled view rendered %d lines into a height of %d:\n%s", got, env.Height, body)
	}
}

// A folder with a dozen executables lists a few and counts the rest —
// they are context for the folder, not things to act on individually.
func TestGameDetailCapsExecutablesPerFolder(t *testing.T) {
	s := NewGameDetailScreen(bigGame(1, 12), fakeDeps())
	body := s.View(Env{Styles: NewStyles(true), Width: 80, Height: 21})

	if !strings.Contains(body, "+6 more") {
		t.Errorf("12 executables should list 6 and count the other 6:\n%s", body)
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
