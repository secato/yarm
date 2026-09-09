package app

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
)

// maxLogLines bounds how many progress lines the screen keeps: the last
// eight, which is enough to see what is happening without the screen
// becoming a scrollback nobody reads.
const maxLogLines = 8

// progressUpdateMsg carries one narrated step of a running install.
type progressUpdateMsg ProgressUpdate

// applyDoneMsg is the final message a run delivers, successful or not:
// one outcome per operation, in the order they were submitted.
type applyDoneMsg struct {
	outcomes []folderOutcome
}

// ProgressScreen runs an install (or reports on one already running) and
// shows what it is doing as it goes.
//
// The work happens in a goroutine started from Init, not inside a single
// tea.Cmd: a Cmd can only ever deliver one message, and this needs to
// stream several. Progress flows back over a channel, following the
// "waitForActivity" pattern — each message drained from the channel triggers re-issuing the same
// listen command, until the job sends its final result and closes it.
type ProgressScreen struct {
	keys        KeyMap
	ops         []folderOp
	installer   Installer
	uninstaller UninstallRunner

	ctx    context.Context
	cancel context.CancelFunc
	ch     chan tea.Msg

	bar   progress.Model
	lines []string
	label string
	done  int64
	total int64

	finished bool
	canceled bool
	outcomes []folderOutcome
}

// NewProgressScreen returns a screen that will run ops in order once
// pushed. Most Applies are one op; a game with more than one folder can
// be several.
func NewProgressScreen(ops []folderOp, installer Installer, uninstaller UninstallRunner) *ProgressScreen {
	ctx, cancel := context.WithCancel(context.Background())
	return &ProgressScreen{
		keys:        DefaultKeyMap(),
		ops:         ops,
		installer:   installer,
		uninstaller: uninstaller,
		ctx:         ctx,
		cancel:      cancel,
		bar:         progress.New(),
	}
}

// Init implements Screen: it starts the work and begins listening for its
// progress.
func (s *ProgressScreen) Init() tea.Cmd {
	s.ch = StreamJob(s.ctx, func(ctx context.Context, send func(tea.Msg)) {
		send(applyDoneMsg{outcomes: s.runOps(ctx, send)})
	})
	return WaitForActivity(s.ch)
}

// runOps applies each operation in turn, stopping at the first failure or
// cancellation.
//
// Each op is atomic on its own — the executor rolls itself back — but the
// batch is not, so the outcomes have to record exactly how far it got.
// Pressing on after a failure would mean writing into more folders after
// something already went wrong, which is not what anyone wants from a tool
// whose whole point is being able to undo itself.
func (s *ProgressScreen) runOps(ctx context.Context, send func(tea.Msg)) []folderOutcome {
	// Every op gets an outcome up front, so the ones never reached still
	// name their folder rather than vanishing from the report.
	outcomes := make([]folderOutcome, len(s.ops))
	for i, op := range s.ops {
		outcomes[i].Op = op
	}

	for i, op := range s.ops {
		if ctx.Err() != nil {
			break
		}
		outcomes[i].Attempted = true

		switch {
		case op.Uninstall != nil:
			send(progressUpdateMsg(ProgressUpdate{Label: s.narrate(op, "Removing ReShade")}))
			outcomes[i].Removed, outcomes[i].Err = s.uninstaller.Uninstall(*op.Uninstall)
		default:
			outcomes[i].Result, outcomes[i].Err = s.installer.Install(ctx, *op.Install, func(u ProgressUpdate) {
				u.Label = s.narrate(op, u.Label)
				send(progressUpdateMsg(u))
			})
		}

		if outcomes[i].Err != nil {
			break
		}
	}
	return outcomes
}

// narrate prefixes a progress line with its folder, but only when there
// is more than one to tell apart — a single install should read exactly
// as it always has.
func (s *ProgressScreen) narrate(op folderOp, label string) string {
	if len(s.ops) < 2 || label == "" {
		return label
	}
	return folderLabel(op.Dir) + " " + label
}

// Title implements Screen.
func (s *ProgressScreen) Title() string {
	if s.finished {
		return "installing — done"
	}
	return "installing…"
}

// KeyBindings implements Screen.
func (s *ProgressScreen) KeyBindings() []key.Binding {
	if s.finished {
		return nil
	}
	return []key.Binding{s.keys.Back}
}

// HandleBack implements backHandler: esc cancels a running install rather
// than leaving the screen. Once finished, the shell's ordinary pop applies
// (in practice the screen has already been replaced by a Result screen by
// then, so this rarely fires).
func (s *ProgressScreen) HandleBack() (Screen, tea.Cmd, bool) {
	if s.finished {
		return s, nil, false
	}
	s.cancel()
	return s, SetStatus("canceling…"), true
}

// Update implements Screen.
func (s *ProgressScreen) Update(msg tea.Msg, env Env) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case progressUpdateMsg:
		s.apply(ProgressUpdate(msg))
		return s, WaitForActivity(s.ch)

	case applyDoneMsg:
		s.finished = true
		s.outcomes = msg.outcomes
		s.canceled = s.ctx.Err() != nil
		return s, PushScreen(NewApplyResultScreen(s.outcomes, s.canceled))
	}
	return s, nil
}

// apply records one progress update and its log line.
func (s *ProgressScreen) apply(u ProgressUpdate) {
	s.label, s.done, s.total = u.Label, u.Done, u.Total
	if u.Label == "" {
		return
	}
	if len(s.lines) == 0 || s.lines[len(s.lines)-1] != u.Label {
		s.lines = append(s.lines, u.Label)
		if len(s.lines) > maxLogLines {
			s.lines = s.lines[len(s.lines)-maxLogLines:]
		}
	}
}

// failed reports whether any operation ended in an error.
func (s *ProgressScreen) failed() bool {
	for _, o := range s.outcomes {
		if o.Err != nil {
			return true
		}
	}
	return false
}

// percent returns the current step's completion in [0,1], or -1 when the
// size is unknown.
func (s *ProgressScreen) percent() float64 {
	if s.total <= 0 {
		return -1
	}
	p := float64(s.done) / float64(s.total)
	if p > 1 {
		p = 1
	}
	return p
}

// View implements Screen.
func (s *ProgressScreen) View(env Env) string {
	var b strings.Builder

	switch {
	case s.finished && !s.failed():
		b.WriteString(env.Styles.Good.Render("Done."))
	case s.finished:
		b.WriteString(env.Styles.Bad.Render("The install did not finish."))
	default:
		b.WriteString(s.label)
		if p := s.percent(); p >= 0 {
			b.WriteString("\n")
			b.WriteString(s.bar.ViewAs(p))
		}
	}
	b.WriteString("\n\n")

	for _, line := range s.lines {
		b.WriteString(env.Styles.Faint.Render(line))
		b.WriteString("\n")
	}

	if !s.finished {
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render("esc cancels"))
	}
	return b.String()
}
