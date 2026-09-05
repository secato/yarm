package app

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// gamesLoadedMsg carries the result of a discovery run.
type gamesLoadedMsg struct{ entries []GameEntry }

// GamesScreen is the home screen: a table of discovered games with a
// detail panel for the selected one.
type GamesScreen struct {
	loader GamesLoader
	keys   KeyMap

	entries   []GameEntry
	filtered  []GameEntry
	table     table.Model
	filter    textinput.Model
	filtering bool
	loading   bool

	// detailWidth is how much of the window the right-hand panel takes.
	detailWidth int
}

// NewGamesScreen returns the home screen, which loads its games on Init.
func NewGamesScreen(loader GamesLoader) *GamesScreen {
	fi := textinput.New()
	fi.Placeholder = "filter games"
	fi.Prompt = "/"

	return &GamesScreen{
		loader:  loader,
		keys:    DefaultKeyMap(),
		filter:  fi,
		loading: true,
		table: table.New(
			table.WithColumns(gamesColumns(80)),
			table.WithFocused(true),
		),
	}
}

// gamesColumns sizes the table for the available width, giving the name
// the slack because it is the column users scan.
func gamesColumns(width int) []table.Column {
	const (
		sourceW  = 8
		statusW  = 16
		minNameW = 16
	)
	nameW := width - sourceW - statusW - 6
	if nameW < minNameW {
		nameW = minNameW
	}
	return []table.Column{
		{Title: "Game", Width: nameW},
		{Title: "Source", Width: sourceW},
		{Title: "ReShade", Width: statusW},
	}
}

// Init implements Screen.
func (s *GamesScreen) Init() tea.Cmd {
	return tea.Batch(s.load(), SetStatus("scanning for games…"))
}

// load discovers games off the UI goroutine.
func (s *GamesScreen) load() tea.Cmd {
	return Async(context.Background(),
		func(ctx context.Context) ([]GameEntry, error) { return s.loader.LoadGames(ctx) },
		func(entries []GameEntry) tea.Msg { return gamesLoadedMsg{entries: entries} },
	)
}

// CapturesInput tells the shell to route plain keys here while the filter
// is open, so typing "q" filters rather than quitting.
func (s *GamesScreen) CapturesInput() bool { return s.filtering }

// Title implements Screen.
func (s *GamesScreen) Title() string {
	if s.loading {
		return "games — scanning…"
	}
	if len(s.filtered) != len(s.entries) {
		return fmt.Sprintf("games — %d of %d", len(s.filtered), len(s.entries))
	}
	return fmt.Sprintf("games — %d found", len(s.entries))
}

// KeyBindings implements Screen.
func (s *GamesScreen) KeyBindings() []key.Binding {
	return []key.Binding{s.keys.Enter, s.keys.Filter, s.keys.Rescan, s.keys.AddGame}
}

// Update implements Screen.
func (s *GamesScreen) Update(msg tea.Msg, env Env) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.resize(env)
		return s, nil

	case gamesLoadedMsg:
		s.loading = false
		s.entries = msg.entries
		s.applyFilter()
		s.resize(env)
		return s, SetStatus(fmt.Sprintf("found %d game(s)", len(s.entries)))

	case tea.KeyPressMsg:
		return s.handleKey(msg, env)
	}

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
}

func (s *GamesScreen) handleKey(msg tea.KeyPressMsg, env Env) (Screen, tea.Cmd) {
	if s.filtering {
		switch msg.String() {
		case "enter":
			s.filtering = false
			s.filter.Blur()
			return s, nil
		case "esc":
			s.filtering = false
			s.filter.Blur()
			s.filter.SetValue("")
			s.applyFilter()
			s.resize(env)
			return s, nil
		}
		var cmd tea.Cmd
		s.filter, cmd = s.filter.Update(msg)
		s.applyFilter()
		s.resize(env)
		return s, cmd
	}

	switch {
	case key.Matches(msg, s.keys.Filter):
		s.filtering = true
		return s, s.filter.Focus()

	case key.Matches(msg, s.keys.Rescan):
		s.loading = true
		return s, tea.Batch(s.load(), SetStatus("rescanning…"))

	case key.Matches(msg, s.keys.AddGame):
		return s, PushScreen(NewAddFolderScreen())

	case key.Matches(msg, s.keys.Enter):
		if entry, ok := s.selected(); ok {
			return s, PushScreen(NewGameDetailScreen(entry))
		}
		return s, nil
	}

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
}

// selected returns the highlighted game.
func (s *GamesScreen) selected() (GameEntry, bool) {
	idx := s.table.Cursor()
	if idx < 0 || idx >= len(s.filtered) {
		return GameEntry{}, false
	}
	return s.filtered[idx], true
}

// applyFilter narrows the list by a case-insensitive substring of the
// game's name.
func (s *GamesScreen) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(s.filter.Value()))
	if q == "" {
		s.filtered = s.entries
		return
	}
	out := make([]GameEntry, 0, len(s.entries))
	for _, e := range s.entries {
		if strings.Contains(strings.ToLower(e.Name), q) {
			out = append(out, e)
		}
	}
	s.filtered = out
}

// resize lays the table out for the current window and refreshes rows.
func (s *GamesScreen) resize(env Env) {
	s.detailWidth = env.Width / 3
	if s.detailWidth < 24 {
		s.detailWidth = 0 // too narrow to be useful; drop the panel
	}
	tableWidth := env.Width - s.detailWidth
	if s.detailWidth > 0 {
		tableWidth -= 2
	}
	if tableWidth < 24 {
		tableWidth = 24
	}

	s.table.SetColumns(gamesColumns(tableWidth))
	s.table.SetWidth(tableWidth)

	height := env.Height - 2 // leave room for the filter line
	if height < 3 {
		height = 3
	}
	s.table.SetHeight(height)

	rows := make([]table.Row, 0, len(s.filtered))
	for _, e := range s.filtered {
		status := e.InstalledSummary()
		switch {
		case status != "":
			status = "✓ " + status
		case e.NativeBuild:
			status = "native build"
		case len(e.PlayableExes()) == 0:
			status = "no .exe found"
		}
		rows = append(rows, table.Row{e.Name, e.Provider, status})
	}
	s.table.SetRows(rows)

	// The table parks its cursor at -1 while it has no rows, which is the
	// state on first paint: the window size arrives before the async scan
	// finishes. Without this, the first game is never selected and enter
	// does nothing. Filtering can also strand the cursor past the end.
	switch {
	case len(rows) == 0:
		// Nothing to point at.
	case s.table.Cursor() < 0:
		s.table.SetCursor(0)
	case s.table.Cursor() >= len(rows):
		s.table.SetCursor(len(rows) - 1)
	}
}

// View implements Screen.
func (s *GamesScreen) View(env Env) string {
	if s.loading && len(s.entries) == 0 {
		return env.Styles.Faint.Render("scanning for games…")
	}
	if len(s.entries) == 0 {
		return env.Styles.Faint.Render(
			"No games found.\n\nPress a to add a game folder, or r to rescan.")
	}

	filterLine := ""
	if s.filtering || s.filter.Value() != "" {
		filterLine = s.filter.View()
	}

	left := lipgloss.JoinVertical(lipgloss.Left, s.table.View(), filterLine)
	if s.detailWidth == 0 {
		return left
	}

	detail := ""
	if entry, ok := s.selected(); ok {
		detail = s.renderDetail(entry, env)
	}
	// Match the table's height so the panel border frames the body rather
	// than stopping wherever its text happens to end.
	panelHeight := env.Height - 4
	if panelHeight < 3 {
		panelHeight = 3
	}
	panel := env.Styles.Panel.
		Width(s.detailWidth - 2).
		Height(panelHeight).
		Render(detail)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", panel)
}

// renderDetail draws the side panel for one game.
func (s *GamesScreen) renderDetail(e GameEntry, env Env) string {
	var b strings.Builder
	b.WriteString(env.Styles.Subtitle.Render(e.Name))
	b.WriteString("\n")
	b.WriteString(env.Styles.Faint.Render(e.ID))
	b.WriteString("\n\n")
	b.WriteString(env.Styles.Faint.Render(truncate(e.Root, s.detailWidth-6)))
	b.WriteString("\n\n")

	exes := e.PlayableExes()
	switch {
	case e.NativeBuild:
		b.WriteString(env.Styles.Warn.Render("Native build"))
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render(
			"ReShade supports Windows executables only."))
	case len(exes) == 0:
		b.WriteString(env.Styles.Faint.Render("No executables found."))
	default:
		b.WriteString(env.Styles.Subtitle.Render("Executables"))
		b.WriteString("\n")
		for _, ex := range exes {
			mark := "  "
			if ex.Installed != nil {
				mark = env.Styles.Good.Render("✓ ")
			}
			b.WriteString(mark + truncate(ex.Path, s.detailWidth-10) + "\n")
			b.WriteString(env.Styles.Faint.Render(
				fmt.Sprintf("    %s · %s", ex.Arch, ex.API)) + "\n")
		}
	}

	return b.String()
}

// truncate shortens a string to width, marking the cut with an ellipsis.
func truncate(s string, width int) string {
	if width <= 1 || lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return "…" + string(runes[len(runes)-width+1:])
}
