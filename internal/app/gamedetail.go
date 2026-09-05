package app

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
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

// adoptDoneMsg carries an adopt run's result back to the screen that
// started it, so it can hand off to a result screen.
type adoptDoneMsg struct {
	exePath string
	install state.Install
}

// Init implements Screen.
func (s *GameDetailScreen) Init() tea.Cmd { return nil }

// Title implements Screen.
func (s *GameDetailScreen) Title() string { return "game — " + s.entry.Name }

// KeyBindings implements Screen.
func (s *GameDetailScreen) KeyBindings() []key.Binding {
	bindings := []key.Binding{showAllBinding, s.keys.Back}
	if len(s.visibleExes()) > 0 {
		install := s.keys.Install
		if exe, ok := s.selected(); ok && exe.Installed != nil {
			install = key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "update ReShade"))
		}
		bindings = append([]key.Binding{install, s.keys.Uninstall}, bindings...)
	}
	if exe, ok := s.selected(); ok {
		if grp, ok := s.entry.GroupFor(exe.Path); ok && grp.Unmanaged != nil {
			bindings = append([]key.Binding{s.keys.Manage}, bindings...)
		}
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

	case adoptDoneMsg:
		return s, PushScreen(NewAdoptResultScreen(msg.exePath, msg.install, nil))

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
		case key.Matches(msg, s.keys.Manage):
			return s.startAdopt()
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

// startAdopt confirms, then records, the unmanaged install found in the
// highlighted executable's folder. The install is tied to the folder's
// primary executable, not necessarily the one highlighted — ReShade
// intercepts by directory, so pressing m on a launcher stub sitting next
// to the real game exe should still record the install against the game
// exe, not the stub.
func (s *GameDetailScreen) startAdopt() (Screen, tea.Cmd) {
	exe, ok := s.selected()
	if !ok || s.deps.Adopter == nil {
		return s, nil
	}
	grp, ok := s.entry.GroupFor(exe.Path)
	if !ok || grp.Unmanaged == nil {
		return s, nil
	}
	candidate := *grp.Unmanaged
	target := grp.primaryExe()

	exePath := target.Path
	g, executable := s.entry.Game, target.Executable
	action := Async(context.Background(),
		func(ctx context.Context) (state.Install, error) {
			return s.deps.Adopter.Adopt(g, executable, candidate)
		},
		func(in state.Install) tea.Msg {
			return adoptDoneMsg{exePath: exePath, install: in}
		},
	)

	detail := fmt.Sprintf(
		"Found ReShade (%s) already installed here, with %d file(s). "+
			"Tracking it lets yarm update or uninstall it later; nothing on disk changes now.",
		candidate.DLLName, candidate.FileCount())

	return s, Confirm("Track the existing ReShade install on "+exePath+"?", detail, action)
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
		archW = 8
		apiW  = 14 // room for "DirectX 12"-style labels
	)
	pathW := env.Width - archW - apiW - 6
	if pathW < 20 {
		pathW = 20
	}

	s.table.SetColumns([]table.Column{
		{Title: "Executable", Width: pathW},
		{Title: "Arch", Width: archW},
		{Title: "API", Width: apiW},
	})
	s.table.SetWidth(env.Width)

	// Everything printed above the table in View(): the root path, a blank
	// line, the "ReShade" header, the section itself, a blank line, and
	// the "Executables" header.
	fixed := 5 + strings.Count(s.reshadeSection(env), "\n")
	h := env.Height - fixed
	if h < 3 {
		h = 3
	}
	s.table.SetHeight(h)

	rows := make([]table.Row, 0, len(s.entry.Exes))
	for _, e := range s.visibleExes() {
		name := e.Path
		if e.Skipped {
			name = "· " + name
		}
		rows = append(rows, table.Row{name, string(e.Arch), apiLabel(e.API)})
	}
	s.table.SetRows(rows)
}

// reshadeSection renders what is installed (or found) for every folder
// this game's executables live in — shown before the executables table so
// "what's actually installed" is the first thing the user sees, not
// something they have to infer from a blank column.
func (s *GameDetailScreen) reshadeSection(env Env) string {
	var b strings.Builder
	multi := len(s.entry.Groups) > 1
	for i, grp := range s.entry.Groups {
		if multi && grp.Dir != "" {
			b.WriteString(env.Styles.Faint.Render(grp.Dir + "/"))
			b.WriteString("\n")
		}
		writeReShadeStatus(&b, grp, env, "press m to track it")
		if i < len(s.entry.Groups)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// View implements Screen.
func (s *GameDetailScreen) View(env Env) string {
	var b strings.Builder
	b.WriteString(env.Styles.Faint.Render(s.entry.Root))
	b.WriteString("\n\n")

	switch {
	case s.entry.ScanErr != nil:
		b.WriteString(env.Styles.Bad.Render("Could not scan this folder"))
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render(friendlyError(s.entry.ScanErr)))
		return b.String()
	case s.entry.NativeBuild:
		b.WriteString(env.Styles.Warn.Render("Native build"))
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render(
			"This game ships a native Linux or macOS binary. ReShade proxies a DLL\n" +
				"through the Windows loader, so it does not apply here."))
		return b.String()
	case len(s.entry.Exes) == 0:
		b.WriteString(env.Styles.Faint.Render("No executables were found in this folder."))
		return b.String()
	}

	b.WriteString(env.Styles.Subtitle.Render("ReShade"))
	b.WriteString("\n")
	b.WriteString(s.reshadeSection(env))
	b.WriteString("\n")

	b.WriteString(env.Styles.Subtitle.Render("Executables"))
	b.WriteString("\n")
	b.WriteString(s.table.View())

	hidden := len(s.entry.Exes) - len(s.visibleExes())
	if hidden > 0 {
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render(
			fmt.Sprintf("%d executable(s) hidden — press t to show all", hidden)))
	}
	return b.String()
}
