package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/install"
)

// ResultScreen is the terminal screen of a flow: what an install or an
// uninstall actually did. Its only way out is PopToRoot — the wizard,
// progress screen and game detail view underneath it are all stale once
// something has changed, so there is nowhere useful to go back to.
type ResultScreen struct {
	keys  KeyMap
	title string
	// ok controls the heading's color; failed and canceled runs still
	// show whatever partial result they produced.
	ok    bool
	lines []string
}

// NewInstallResultScreen summarizes an install.Result.
func NewInstallResultScreen(req install.Request, res install.Result, err error, canceled bool) *ResultScreen {
	s := &ResultScreen{keys: DefaultKeyMap()}

	switch {
	case canceled:
		s.title = "install canceled"
		s.lines = append(s.lines, "Canceled before it finished.")
		if len(res.Written) > 0 {
			s.lines = append(s.lines, fmt.Sprintf("%d file(s) had already been written and were rolled back.", len(res.Written)))
		}
	case err != nil:
		s.title = "install failed"
		s.lines = append(s.lines, err.Error())
	default:
		s.ok = true
		s.title = "installed"
		s.lines = append(s.lines, fmt.Sprintf("ReShade %s (%s) → %s", req.Version, req.Flavor, req.Exe.Path))
		s.lines = append(s.lines, fmt.Sprintf("%d file(s) written.", len(res.Written)))
		if n := len(res.Skipped); n > 0 {
			s.lines = append(s.lines, fmt.Sprintf("%d file(s) unchanged.", n))
		}
		if n := len(res.Removed); n > 0 {
			s.lines = append(s.lines, fmt.Sprintf("%d file(s) from a previous install removed.", n))
		}
		for _, w := range res.Warnings {
			s.lines = append(s.lines, "! "+w)
		}
	}
	return s
}

// NewUninstallResultScreen summarizes an install.UninstallResult.
func NewUninstallResultScreen(exePath string, res install.UninstallResult, err error) *ResultScreen {
	s := &ResultScreen{keys: DefaultKeyMap()}

	if err != nil {
		s.title = "uninstall failed"
		s.lines = append(s.lines, err.Error())
		return s
	}

	s.ok = true
	s.title = "uninstalled"
	s.lines = append(s.lines, exePath)
	s.lines = append(s.lines, fmt.Sprintf("%d file(s) removed.", len(res.Removed)))
	if n := len(res.Kept); n > 0 {
		s.lines = append(s.lines, fmt.Sprintf("%d file(s) kept: modified since installation.", n))
	}
	if n := len(res.Restored); n > 0 {
		s.lines = append(s.lines, fmt.Sprintf("%d backed-up file(s) restored.", n))
	}
	return s
}

// Init implements Screen.
func (s *ResultScreen) Init() tea.Cmd { return nil }

// Title implements Screen.
func (s *ResultScreen) Title() string { return s.title }

// KeyBindings implements Screen.
func (s *ResultScreen) KeyBindings() []key.Binding {
	return []key.Binding{s.keys.Enter}
}

// HandleBack implements backHandler: esc here means "done", same as
// enter — there is nothing to step back into.
func (s *ResultScreen) HandleBack() (Screen, tea.Cmd, bool) {
	return s, PopToRoot(), true
}

// Update implements Screen.
func (s *ResultScreen) Update(msg tea.Msg, env Env) (Screen, tea.Cmd) {
	if km, ok := msg.(tea.KeyPressMsg); ok && km.String() == "enter" {
		return s, PopToRoot()
	}
	return s, nil
}

// View implements Screen.
func (s *ResultScreen) View(env Env) string {
	var b strings.Builder
	if s.ok {
		b.WriteString(env.Styles.Good.Render("✓ " + s.title))
	} else {
		b.WriteString(env.Styles.Bad.Render("✗ " + s.title))
	}
	b.WriteString("\n\n")
	for _, line := range s.lines {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(env.Styles.Faint.Render("enter to return to your games"))
	return b.String()
}
