package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// AddFolderScreen takes a path to a game folder and validates it before
// handing it back.
//
// Validation happens as the user types rather than on submit, so a typo is
// visible immediately instead of after a round trip.
type AddFolderScreen struct {
	input textinput.Model
	keys  KeyMap
	// problem describes why the current path is unusable, or "".
	problem string
	// exes counts what a scan of the entered path would find.
	exes int
	// onAdd, when set, receives an accepted path.
	onAdd func(name, path string) tea.Cmd
}

// NewAddFolderScreen returns the add-folder prompt.
func NewAddFolderScreen() *AddFolderScreen {
	ti := textinput.New()
	ti.Placeholder = "/path/to/game"
	ti.Prompt = "› "
	ti.Focus()

	return &AddFolderScreen{input: ti, keys: DefaultKeyMap()}
}

// WithHandler sets the callback invoked when a valid path is accepted.
func (s *AddFolderScreen) WithHandler(fn func(name, path string) tea.Cmd) *AddFolderScreen {
	s.onAdd = fn
	return s
}

// CapturesInput keeps the shell from treating typed letters as commands.
func (s *AddFolderScreen) CapturesInput() bool { return true }

// Init implements Screen.
func (s *AddFolderScreen) Init() tea.Cmd { return textinput.Blink }

// Title implements Screen.
func (s *AddFolderScreen) Title() string { return "add a game folder" }

// KeyBindings implements Screen.
func (s *AddFolderScreen) KeyBindings() []key.Binding {
	return []key.Binding{s.keys.Enter, s.keys.Back}
}

// Update implements Screen.
func (s *AddFolderScreen) Update(msg tea.Msg, _ Env) (Screen, tea.Cmd) {
	if km, ok := msg.(tea.KeyPressMsg); ok {
		switch km.String() {
		case "esc":
			return s, PopScreen()
		case "enter":
			return s, s.submit()
		}
	}

	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	s.validate()
	return s, cmd
}

// submit accepts the path if it is usable.
func (s *AddFolderScreen) submit() tea.Cmd {
	s.validate()
	if s.problem != "" {
		return nil
	}

	path := expandPath(s.input.Value())
	name := filepath.Base(path)

	if s.onAdd == nil {
		// Nothing is wired up yet; report so the user is not left guessing.
		return tea.Batch(
			SetStatus(fmt.Sprintf("%s looks valid (%d executable(s))", name, s.exes)),
			PopScreen(),
		)
	}
	return tea.Batch(s.onAdd(name, path), PopScreen())
}

// validate checks the entered path, filling in problem and exes.
func (s *AddFolderScreen) validate() {
	raw := strings.TrimSpace(s.input.Value())
	s.problem, s.exes = "", 0
	if raw == "" {
		return
	}

	path := expandPath(raw)
	if !filepath.IsAbs(path) {
		s.problem = "enter an absolute path"
		return
	}

	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		s.problem = "no such folder"
		return
	case err != nil:
		s.problem = err.Error()
		return
	case !info.IsDir():
		s.problem = "that is a file, not a folder"
		return
	}

	s.exes = countExecutables(path)
}

// View implements Screen.
func (s *AddFolderScreen) View(env Env) string {
	var b strings.Builder
	b.WriteString(env.Styles.Faint.Render(
		"Point yarm at a game folder it did not find on its own."))
	b.WriteString("\n\n")
	b.WriteString(s.input.View())
	b.WriteString("\n\n")

	switch {
	case s.input.Value() == "":
		b.WriteString(env.Styles.Faint.Render("enter to add · esc to cancel"))
	case s.problem != "":
		b.WriteString(env.Styles.Bad.Render("✗ " + s.problem))
	case s.exes == 0:
		b.WriteString(env.Styles.Warn.Render(
			"! folder exists but holds no .exe files"))
	default:
		b.WriteString(env.Styles.Good.Render(
			fmt.Sprintf("✓ %d executable(s) found", s.exes)))
	}
	return b.String()
}

// expandPath resolves a leading ~ to the user's home directory, which is
// how people actually type paths.
func expandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}
