package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
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

// NewApplyResultScreen summarizes a batch of folderOps. Most Applies are a
// single install, still reported exactly as before; a game with several
// folders, or a move (install+uninstall together), produces one outcome per
// op and all of them get reported.
func NewApplyResultScreen(outcomes []folderOutcome, canceled bool) *ResultScreen {
	s := &ResultScreen{keys: DefaultKeyMap()}
	multi := len(outcomes) > 1

	failedAt := -1
	for i, o := range outcomes {
		if o.Attempted && o.Err != nil {
			failedAt = i
			break
		}
	}

	switch {
	case canceled:
		s.title = "canceled"
		s.lines = append(s.lines, "Canceled before it finished.")
		for _, o := range outcomes {
			if !o.Attempted || o.Op.Uninstall != nil || len(o.Result.Written) == 0 {
				continue
			}
			s.lines = append(s.lines, applyLine(o, multi,
				fmt.Sprintf("%d file(s) had already been written and were rolled back.", len(o.Result.Written))))
		}
	case failedAt >= 0:
		s.title = outcomes[failedAt].Op.failVerb() + " failed"
		s.lines = append(s.lines, applyLine(outcomes[failedAt], multi, outcomes[failedAt].Err.Error()))
	default:
		s.ok = true
		s.title = "applied"
		if !multi {
			s.title = outcomes[0].Op.verb()
		}
		for _, o := range outcomes {
			s.lines = append(s.lines, describeOutcome(o, multi)...)
		}
	}
	return s
}

// applyLine prefixes a line with its folder, but only when there is more
// than one op to tell apart.
func applyLine(o folderOutcome, multi bool, line string) string {
	if !multi {
		return line
	}
	return folderLabel(o.Op.Dir) + " " + line
}

// describeOutcome reports what one successful op did, in the same terms
// NewUninstallResultScreen and the old NewInstallResultScreen always used.
func describeOutcome(o folderOutcome, multi bool) []string {
	if o.Op.Uninstall != nil {
		return describeUninstallOutcome(o, multi)
	}
	return describeInstallOutcome(o, multi)
}

func describeInstallOutcome(o folderOutcome, multi bool) []string {
	req, res := o.Op.Install, o.Result
	lines := []string{applyLine(o, multi, fmt.Sprintf("ReShade %s (%s) → %s", req.Version, req.Flavor, req.Exe.Path))}

	written := fmt.Sprintf("%d file(s) written", len(res.Written))
	// The size is worth a few characters here: it is the answer to "what
	// did that cost me", and the only place the install's footprint on
	// disk is ever stated.
	if res.Bytes > 0 {
		written += " (" + humanSize(res.Bytes) + ")"
	}
	lines = append(lines, applyLine(o, multi, written+"."))
	if n := len(res.Skipped); n > 0 {
		lines = append(lines, applyLine(o, multi, fmt.Sprintf("%d file(s) unchanged.", n)))
	}
	if n := len(res.Removed); n > 0 {
		lines = append(lines, applyLine(o, multi, fmt.Sprintf("%d file(s) from a previous install removed.", n)))
	}
	for _, w := range res.Warnings {
		lines = append(lines, applyLine(o, multi, "! "+w))
	}
	return lines
}

func describeUninstallOutcome(o folderOutcome, multi bool) []string {
	res := o.Removed
	lines := []string{applyLine(o, multi, o.Op.Uninstall.Exe)}
	lines = append(lines, applyLine(o, multi, fmt.Sprintf("%d file(s) removed.", len(res.Removed))))
	if n := len(res.Kept); n > 0 {
		lines = append(lines, applyLine(o, multi, fmt.Sprintf("%d file(s) kept: modified since installation.", n)))
	}
	if n := len(res.Restored); n > 0 {
		lines = append(lines, applyLine(o, multi, fmt.Sprintf("%d backed-up file(s) restored.", n)))
	}
	return lines
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

// NewAdoptResultScreen summarizes an install.Adopt call.
func NewAdoptResultScreen(exePath string, in state.Install, err error) *ResultScreen {
	s := &ResultScreen{keys: DefaultKeyMap()}

	if err != nil {
		s.title = "tracking failed"
		s.lines = append(s.lines, err.Error())
		return s
	}

	s.ok = true
	s.title = "now tracked"
	s.lines = append(s.lines, exePath)
	s.lines = append(s.lines, fmt.Sprintf("%d file(s) recorded; nothing on disk changed.", len(in.Files)))
	s.lines = append(s.lines, "yarm can update or uninstall this ReShade install from now on.")
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
