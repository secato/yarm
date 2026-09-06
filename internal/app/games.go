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

// gamesLoadedMsg carries the result of a discovery run, success or not.
//
// Built as a plain tea.Cmd in load() rather than through Async: Async
// would route a discovery failure to the shell's generic error overlay
// without ever reaching this screen's Update, leaving "scanning…" as the
// title forever once the dialog closed even though "r" would still
// silently work to retry.
type gamesLoadedMsg struct {
	entries []GameEntry
	err     error
}

// GamesScreen is the home screen: a table of discovered games with a
// detail panel for the selected one.
type GamesScreen struct {
	loader GamesLoader
	deps   Deps
	keys   KeyMap

	entries   []GameEntry
	filtered  []GameEntry
	table     table.Model
	filter    textinput.Model
	filtering bool
	loading   bool

	// firstRun shows a welcome banner once the initial scan completes,
	// dismissed by the user's first keypress (which still does whatever
	// it would normally do — pressing "a" both dismisses the banner and
	// opens add-folder).
	firstRun bool

	// detailWidth is how much of the window the right-hand panel takes.
	detailWidth int
}

// NewGamesScreen returns the home screen, which loads its games on Init.
// firstRun shows a one-time welcome banner once the scan completes — the
// caller's signal that config.yaml did not exist before this launch.
func NewGamesScreen(loader GamesLoader, deps Deps, firstRun bool) *GamesScreen {
	fi := textinput.New()
	fi.Placeholder = "filter games"
	fi.Prompt = "/"
	// bubbles/textinput's placeholder rendering sizes its internal buffer
	// from Width(), not from the placeholder string itself: left at the
	// zero value, placeholderView renders only the placeholder's first
	// character and stops. resize() keeps this current with env.Width;
	// this is just a sane value before the first WindowSizeMsg arrives.
	fi.SetWidth(30)

	return &GamesScreen{
		loader:   loader,
		deps:     deps,
		keys:     DefaultKeyMap(),
		filter:   fi,
		loading:  true,
		firstRun: firstRun,
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
	loader := s.loader
	return func() tea.Msg {
		entries, err := loader.LoadGames(context.Background())
		return gamesLoadedMsg{entries: entries, err: err}
	}
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
	bindings := []key.Binding{
		s.keys.Enter, s.keys.Filter, s.keys.Rescan, s.keys.AddGame,
		s.keys.Cache, s.keys.Custom, s.keys.Setting,
	}

	// Acting directly from the list only makes sense for a game with
	// exactly one folder — with more than one there is no single
	// install/uninstall to act on without saying which, which is what the
	// detail view (enter) is for.
	entry, grp, ok := s.singleGroupSelection()
	if !ok {
		return bindings
	}
	switch {
	case grp.Unmanaged != nil:
		bindings = append([]key.Binding{key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "adopt ReShade"))}, bindings...)
	case len(entry.PlayableExes()) > 0 && grp.Installed != nil:
		bindings = append([]key.Binding{editInstallBinding, s.keys.Uninstall}, bindings...)
	case len(entry.PlayableExes()) > 0:
		bindings = append([]key.Binding{s.keys.Install}, bindings...)
	}
	return bindings
}

// singleGroupSelection returns the highlighted game and its one folder
// group, when it has exactly one.
func (s *GamesScreen) singleGroupSelection() (GameEntry, FolderGroup, bool) {
	entry, ok := s.selected()
	if !ok || len(entry.Groups) != 1 {
		return GameEntry{}, FolderGroup{}, false
	}
	return entry, entry.Groups[0], true
}

// Update implements Screen.
func (s *GamesScreen) Update(msg tea.Msg, env Env) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.resize(env)
		return s, nil

	case gamesLoadedMsg:
		s.loading = false
		if msg.err != nil {
			return s, ReportError(msg.err)
		}
		s.entries = msg.entries
		s.applyFilter()
		s.resize(env)
		return s, SetStatus(fmt.Sprintf("found %d game(s)", len(s.entries)))

	case uninstallDoneMsg:
		return s, PushScreen(NewUninstallResultScreen(msg.exePath, msg.result, nil))

	case adoptDoneMsg:
		return s, PushScreen(NewAdoptResultScreen(msg.exePath, msg.install, nil))

	case tea.KeyPressMsg:
		return s.handleKey(msg, env)
	}

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
}

func (s *GamesScreen) handleKey(msg tea.KeyPressMsg, env Env) (Screen, tea.Cmd) {
	// The first keypress dismisses the welcome banner and still does
	// whatever it would normally do.
	s.firstRun = false

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

	case key.Matches(msg, s.keys.Cache):
		return s, PushScreen(NewResourcesScreen(s.deps))

	case key.Matches(msg, s.keys.Custom):
		return s, PushScreen(NewCustomScreen(s.deps.CustomDir))

	case key.Matches(msg, s.keys.Setting):
		return s, PushScreen(NewSettingsScreen(s.deps.ConfigDir, s.deps.Config))

	case key.Matches(msg, s.keys.Install) || key.Matches(msg, editInstallBinding):
		if entry, grp, ok := s.singleGroupSelection(); ok {
			return s, startInstallForGroup(entry, grp, s.deps)
		}
		return s, nil

	case key.Matches(msg, s.keys.Uninstall):
		if entry, grp, ok := s.singleGroupSelection(); ok {
			return s, startUninstallForGroup(entry, grp, s.deps)
		}
		return s, nil

	case key.Matches(msg, s.keys.Enter):
		if entry, ok := s.selected(); ok {
			return s, PushScreen(NewGameDetailScreen(entry, s.deps))
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
	filterWidth := env.Width - 6
	if filterWidth < 10 {
		filterWidth = 10
	}
	s.filter.SetWidth(filterWidth)

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
		case e.ScanErr != nil:
			status = "⚠ can't read folder"
		case e.HasUnmanaged():
			status = "found, untracked"
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

	banner := ""
	if s.firstRun {
		banner = s.welcomeBanner(env) + "\n\n"
	}

	if len(s.entries) == 0 {
		return banner + env.Styles.Faint.Render(
			"No games found.\n\nPress a to add a game folder, or r to rescan.")
	}

	filterLine := ""
	if s.filtering || s.filter.Value() != "" {
		filterLine = s.filter.View()
	}

	left := lipgloss.JoinVertical(lipgloss.Left, s.table.View(), filterLine)
	if s.detailWidth == 0 {
		return banner + left
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
	// lipgloss.Height sets a minimum, not a maximum: a game with several
	// folders full of executables would otherwise push the panel's own
	// border off the bottom of the window.
	panel := env.Styles.Panel.
		Width(s.detailWidth - 2).
		Height(panelHeight).
		Render(clipLines(detail, panelHeight))

	return banner + lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", panel)
}

// renderDetail draws the side panel for one game.
// welcomeBanner summarizes the first run: that yarm's directories were
// just created, what Steam found (if anything), and how to add a folder
// it did not find on its own.
func (s *GamesScreen) welcomeBanner(env Env) string {
	steamGames := 0
	for _, e := range s.entries {
		if e.Provider == "steam" {
			steamGames++
		}
	}

	var b strings.Builder
	b.WriteString(env.Styles.Title.Render("Welcome to yarm!"))
	b.WriteString("\n")
	b.WriteString(env.Styles.Faint.Render("Your config, data and cache directories were just created."))
	b.WriteString("\n")

	switch steamGames {
	case 0:
		b.WriteString(env.Styles.Faint.Render("No Steam library was found."))
	case 1:
		b.WriteString(env.Styles.Faint.Render("Steam found 1 game."))
	default:
		b.WriteString(env.Styles.Faint.Render(fmt.Sprintf("Steam found %d games.", steamGames)))
	}
	b.WriteString(" ")
	b.WriteString(env.Styles.Accent.Render("Press a to add a folder yourself."))
	return b.String()
}

func (s *GamesScreen) renderDetail(e GameEntry, env Env) string {
	var b strings.Builder
	b.WriteString(env.Styles.Subtitle.Render(e.Name))
	b.WriteString("\n")
	b.WriteString(env.Styles.Faint.Render(e.ID))
	b.WriteString("\n\n")
	b.WriteString(env.Styles.Faint.Render(truncate(e.Root, s.detailWidth-6)))
	b.WriteString("\n\n")

	switch {
	case e.ScanErr != nil:
		b.WriteString(env.Styles.Bad.Render("Could not scan this folder"))
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render(friendlyError(e.ScanErr)))
	case e.NativeBuild:
		b.WriteString(env.Styles.Warn.Render("Native build"))
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render(
			"ReShade supports Windows executables only."))
	case len(e.Groups) == 0:
		b.WriteString(env.Styles.Faint.Render("No executables found."))
	default:
		multi := len(e.Groups) > 1
		for i, grp := range e.Groups {
			if i > 0 {
				b.WriteString("\n")
			}
			indent := ""
			if multi {
				header := grp.Dir + "/"
				if grp.Dir == "" {
					header = "(game root)/"
				}
				b.WriteString(env.Styles.Subtitle.Render(header))
				b.WriteString("\n")
				indent = "  "
			}
			b.WriteString(indentLines(reshadeStatusText(grp, env, "press enter, then a to adopt it"), indent))
			b.WriteString(indent + "Executables\n")
			stripPrefix := ""
			if grp.Dir != "" {
				stripPrefix = grp.Dir + "/"
			}
			for _, ex := range grp.Exes {
				if ex.Skipped {
					continue
				}
				name := strings.TrimPrefix(ex.Path, stripPrefix)
				b.WriteString(indent + "  " + truncate(name, s.detailWidth-8) + "\n")
				b.WriteString(env.Styles.Faint.Render(
					indent+fmt.Sprintf("    %s · %s", ex.Arch, apiLabel(ex.API))) + "\n")
			}
			// Last, below everything else in this folder's block: a real
			// safety warning belongs at the bottom of the pane, not
			// sandwiched between the ReShade status and the executables.
			if groupIsAddon(grp) {
				b.WriteString(indent + env.Styles.Bad.Render(anticheatWarning) + "\n")
			}
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
