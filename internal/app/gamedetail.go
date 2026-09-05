package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
)

// GameDetailScreen lists one game's executables and what is installed into
// each. The install wizard hangs off this screen in step 6.
type GameDetailScreen struct {
	entry   GameEntry
	keys    KeyMap
	table   table.Model
	showAll bool
}

// NewGameDetailScreen returns the detail view for a game.
func NewGameDetailScreen(entry GameEntry) *GameDetailScreen {
	return &GameDetailScreen{
		entry: entry,
		keys:  DefaultKeyMap(),
		table: table.New(table.WithFocused(true)),
	}
}

// showAllBinding toggles executables the scanner flagged as installers,
// crash handlers and the like.
var showAllBinding = key.NewBinding(
	key.WithKeys("t"), key.WithHelp("t", "show all exes"))

// Init implements Screen.
func (s *GameDetailScreen) Init() tea.Cmd { return nil }

// Title implements Screen.
func (s *GameDetailScreen) Title() string { return "game — " + s.entry.Name }

// KeyBindings implements Screen.
func (s *GameDetailScreen) KeyBindings() []key.Binding {
	return []key.Binding{showAllBinding, s.keys.Back}
}

// Update implements Screen.
func (s *GameDetailScreen) Update(msg tea.Msg, env Env) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.resize(env)
		return s, nil

	case tea.KeyPressMsg:
		if key.Matches(msg, showAllBinding) {
			s.showAll = !s.showAll
			s.resize(env)
			return s, nil
		}
	}

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
}

// visibleExes returns the executables the table should show.
func (s *GameDetailScreen) visibleExes() []Executable {
	if s.showAll {
		return s.entry.Exes
	}
	return s.entry.PlayableExes()
}

func (s *GameDetailScreen) resize(env Env) {
	const (
		archW   = 8
		apiW    = 9
		statusW = 18
	)
	pathW := env.Width - archW - apiW - statusW - 8
	if pathW < 20 {
		pathW = 20
	}

	s.table.SetColumns([]table.Column{
		{Title: "Executable", Width: pathW},
		{Title: "Arch", Width: archW},
		{Title: "API", Width: apiW},
		{Title: "ReShade", Width: statusW},
	})
	s.table.SetWidth(env.Width)

	h := env.Height - 4
	if h < 3 {
		h = 3
	}
	s.table.SetHeight(h)

	rows := make([]table.Row, 0, len(s.entry.Exes))
	for _, e := range s.visibleExes() {
		status := ""
		if e.Installed != nil {
			status = "✓ " + e.Installed.ReShade.Version + " " + e.Installed.ReShade.Flavor
		} else if !e.API.Supported() {
			status = "unsupported api"
		}
		name := e.Path
		if e.Skipped {
			name = "· " + name
		}
		rows = append(rows, table.Row{name, string(e.Arch), string(e.API), status})
	}
	s.table.SetRows(rows)
}

// View implements Screen.
func (s *GameDetailScreen) View(env Env) string {
	var b strings.Builder
	b.WriteString(env.Styles.Faint.Render(s.entry.Root))
	b.WriteString("\n\n")

	if len(s.entry.Exes) == 0 {
		if s.entry.NativeBuild {
			b.WriteString(env.Styles.Warn.Render("Native build"))
			b.WriteString("\n")
			b.WriteString(env.Styles.Faint.Render(
				"This game ships a native Linux or macOS binary. ReShade proxies a DLL\n" +
					"through the Windows loader, so it does not apply here."))
		} else {
			b.WriteString(env.Styles.Faint.Render("No executables were found in this folder."))
		}
		return b.String()
	}

	b.WriteString(s.table.View())

	hidden := len(s.entry.Exes) - len(s.visibleExes())
	if hidden > 0 {
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render(
			fmt.Sprintf("%d executable(s) hidden — press t to show all", hidden)))
	}
	return b.String()
}
