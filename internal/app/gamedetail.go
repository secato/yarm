package app

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/install"
)

// GameDetailScreen lists one game's executables and what is installed into
// each. Pressing i opens the install wizard on the highlighted executable;
// u opens an uninstall confirmation when it has something installed.
type GameDetailScreen struct {
	entry   GameEntry
	deps    Deps
	keys    KeyMap
	table   table.Model
	showAll bool
}

// NewGameDetailScreen returns the detail view for a game.
func NewGameDetailScreen(entry GameEntry, deps Deps) *GameDetailScreen {
	return &GameDetailScreen{
		entry: entry,
		deps:  deps,
		keys:  DefaultKeyMap(),
		table: table.New(table.WithFocused(true)),
	}
}

// showAllBinding toggles executables the scanner flagged as installers,
// crash handlers and the like.
var showAllBinding = key.NewBinding(
	key.WithKeys("t"), key.WithHelp("t", "show all exes"))

// uninstallDoneMsg carries an uninstall run's result back to the screen
// that started it, so it can hand off to a result screen.
type uninstallDoneMsg struct {
	exePath string
	result  install.UninstallResult
}

// Init implements Screen.
func (s *GameDetailScreen) Init() tea.Cmd { return nil }

// Title implements Screen.
func (s *GameDetailScreen) Title() string { return "game — " + s.entry.Name }

// KeyBindings implements Screen.
func (s *GameDetailScreen) KeyBindings() []key.Binding {
	bindings := []key.Binding{showAllBinding, s.keys.Back}
	if len(s.visibleExes()) > 0 {
		bindings = append([]key.Binding{s.keys.Install, s.keys.Uninstall}, bindings...)
	}
	return bindings
}

// Update implements Screen.
func (s *GameDetailScreen) Update(msg tea.Msg, env Env) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.resize(env)
		return s, nil

	case uninstallDoneMsg:
		return s, PushScreen(NewUninstallResultScreen(msg.exePath, msg.result, nil))

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, showAllBinding):
			s.showAll = !s.showAll
			s.resize(env)
			return s, nil
		case key.Matches(msg, s.keys.Install):
			return s.startInstall()
		case key.Matches(msg, s.keys.Uninstall):
			return s.startUninstall()
		}
	}

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
}

// selected returns the executable highlighted in the table.
func (s *GameDetailScreen) selected() (Executable, bool) {
	exes := s.visibleExes()
	i := s.table.Cursor()
	if i < 0 || i >= len(exes) {
		return Executable{}, false
	}
	return exes[i], true
}

// startInstall opens the wizard on the highlighted executable.
func (s *GameDetailScreen) startInstall() (Screen, tea.Cmd) {
	exe, ok := s.selected()
	if !ok {
		return s, nil
	}
	// The wizard's own exe step starts from PlayableExes(), so the
	// preselection has to be an index into that list, not into whatever
	// this screen's "show all" toggle currently displays.
	preselected := 0
	for i, e := range s.entry.PlayableExes() {
		if e.Path == exe.Path {
			preselected = i
			break
		}
	}
	return s, PushScreen(NewWizardScreen(s.entry, preselected, s.deps))
}

// startUninstall confirms, then removes, the install on the highlighted
// executable.
func (s *GameDetailScreen) startUninstall() (Screen, tea.Cmd) {
	exe, ok := s.selected()
	if !ok || exe.Installed == nil || s.deps.Uninstaller == nil {
		return s, nil
	}

	exePath := exe.Path
	action := Async(context.Background(),
		func(ctx context.Context) (install.UninstallResult, error) {
			return s.deps.Uninstaller.Uninstall(install.UninstallRequest{
				GameID: s.entry.ID,
				Exe:    exePath,
			})
		},
		func(res install.UninstallResult) tea.Msg {
			return uninstallDoneMsg{exePath: exePath, result: res}
		},
	)

	return s, Confirm(
		"Uninstall ReShade from "+exePath+"?",
		"This removes only the files yarm created; anything you edited afterward is kept.",
		action,
	)
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
