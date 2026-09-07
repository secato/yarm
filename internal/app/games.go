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
		// A game name rarely needs more than this, and every column past
		// it is space the side panel could be using instead.
		maxNameW = 34
	)
	nameW := width - sourceW - statusW - 6
	switch {
	case nameW < minNameW:
		nameW = minNameW
	case nameW > maxNameW:
		nameW = maxNameW
	}
	return []table.Column{
		{Title: "Game", Width: nameW},
		{Title: "Source", Width: sourceW},
		{Title: "ReShade", Width: statusW},
	}
}

// gamesTableWidth is what the table occupies once its name column has hit
// its cap — the point past which extra width is just padding.
func gamesTableWidth() int {
	w := 0
	for _, c := range gamesColumns(1 << 20) {
		w += c.Width
	}
	return w + 6
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

	// Every action runs from this list now. A game with several folders
	// needs one more question answered first — which folder — and the
	// picker asks it; the key is offered either way, because offering it
	// only for one-folder games reads as the action being unavailable
	// rather than as needing one more step.
	e, ok := s.selected()
	if !ok {
		return bindings
	}
	// Adopting has no key of its own here — `a` already means "add folder"
	// — but install redirects to it for a folder whose ReShade yarm did
	// not put there, so the binding says what pressing i will actually do.
	if len(groupsWithUnmanaged(e)) > 0 {
		bindings = append([]key.Binding{adoptBinding}, bindings...)
	}
	if len(groupsWithInstall(e)) > 0 {
		bindings = append([]key.Binding{editInstallBinding, s.keys.Uninstall}, bindings...)
	}
	if len(installableGroups(e)) > 0 {
		bindings = append([]key.Binding{s.keys.Install}, bindings...)
	}
	return bindings
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

	case key.Matches(msg, s.keys.Install):
		if e, ok := s.selected(); ok {
			return s, pickFolder(e, s.deps, verbInstall, installableGroups(e), startInstallForGroup)
		}
		return s, nil

	case key.Matches(msg, editInstallBinding):
		if e, ok := s.selected(); ok {
			return s, pickFolder(e, s.deps, verbEdit, groupsWithInstall(e), startInstallForGroup)
		}
		return s, nil

	case key.Matches(msg, s.keys.Uninstall):
		if e, ok := s.selected(); ok {
			return s, pickFolder(e, s.deps, verbUninstall, groupsWithInstall(e), startUninstallForGroup)
		}
		return s, nil

	// Enter does the obvious thing to the highlighted game: edit what it
	// has, adopt what was found on disk, or install when it has neither.
	case key.Matches(msg, s.keys.Enter):
		if e, ok := s.selected(); ok {
			verb, groups := installOrEdit(e)
			return s, pickFolder(e, s.deps, verb, groups, startInstallForGroup)
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

	// Two fifths, not one third: the panel is now the only place a game's
	// folders, install and executables are shown — there is no detail
	// screen behind it any more — while the table needs only enough for a
	// name, a source and a status.
	s.detailWidth = env.Width * 2 / 5
	if s.detailWidth < 28 {
		s.detailWidth = 0 // too narrow to be useful; drop the panel
	}
	// The table only needs what its columns actually use; whatever it does
	// not need goes to the panel rather than to empty space between the
	// name column and the source column.
	tableWidth := env.Width - s.detailWidth
	if s.detailWidth > 0 {
		tableWidth -= 2
		if used := gamesTableWidth(); tableWidth > used {
			s.detailWidth += tableWidth - used
			tableWidth = used
		}
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

	detail, warned := "", false
	if entry, ok := s.selected(); ok {
		detail, warned = s.renderDetail(entry, env)
	}
	// Match the table's height so the panel border frames the body rather
	// than stopping wherever its text happens to end.
	panelHeight := env.Height - 4
	if panelHeight < 3 {
		panelHeight = 3
	}

	// The add-on warning is appended after the body has been cut to fit,
	// so it is the executables and effect lists that give way rather than
	// the one line here that is about safety.
	var warning strings.Builder
	if warned {
		writeAnticheatWarning(&warning, env, s.detailWidth-6)
	}
	// lipgloss.Height sets a minimum, not a maximum: a game with several
	// folders full of executables would otherwise push the panel's own
	// border off the bottom of the window.
	panel := env.Styles.Panel.
		Width(s.detailWidth - 2).
		Height(panelHeight).
		Render(clipLines(detail, panelHeight-countLines(warning.String())) + warning.String())

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

func (s *GamesScreen) renderDetail(e GameEntry, env Env) (body string, warned bool) {
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
			b.WriteString(indentLines(reshadeStatusText(grp, env, "press enter, then a to adopt it", s.detailWidth-6-len(indent)), indent))
			var exes strings.Builder
			writeSectionHeader(&exes, env, "Executables", grp.playableCount())
			b.WriteString(indentLines(exes.String(), indent))
			stripPrefix := ""
			if grp.Dir != "" {
				stripPrefix = grp.Dir + "/"
			}
			// Executables are context — ReShade covers the whole folder
			// either way — and they are what the panel's clip would eat
			// the anti-cheat warning to make room for. A few, then a
			// count, the way the detail screen already does it.
			const maxExes = 4
			shown, hidden := 0, 0
			for _, ex := range grp.Exes {
				if ex.Skipped {
					continue
				}
				if shown >= maxExes {
					hidden++
					continue
				}
				shown++
				name := strings.TrimPrefix(ex.Path, stripPrefix)
				b.WriteString(indent + "  " + truncate(name, s.detailWidth-8) + "\n")
				b.WriteString(env.Styles.Faint.Render(
					indent+fmt.Sprintf("    %s · %s", ex.Arch, apiLabel(ex.API))) + "\n")
			}
			if hidden > 0 {
				b.WriteString(env.Styles.Faint.Render(
					indent+fmt.Sprintf("  +%d more", hidden)) + "\n")
			}
			// Last, below everything else in this folder's block: a real
			// safety warning belongs at the bottom of the pane, not
			// sandwiched between the ReShade status and the executables.
			// The warning is returned separately rather than written
			// here: the panel has a fixed height, and whatever is at the
			// bottom of a too-long block is what gets clipped away. A
			// safety notice must not be the part that gives way.
			if groupIsAddon(grp) {
				warned = true
			}
		}
	}

	return b.String(), warned
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
