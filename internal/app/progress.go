package app

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/install"
)

// maxLogLines bounds how many progress lines the screen keeps, per §6.1
// ("log lines (last 8)").
const maxLogLines = 8

// progressUpdateMsg carries one narrated step of a running install.
type progressUpdateMsg ProgressUpdate

// installDoneMsg is the final message a run delivers, successful or not.
type installDoneMsg struct {
	result install.Result
	err    error
}

// ProgressScreen runs an install (or reports on one already running) and
// shows what it is doing as it goes.
//
// The work happens in a goroutine started from Init, not inside a single
// tea.Cmd: a Cmd can only ever deliver one message, and this needs to
// stream several. Progress flows back over a channel, following the
// "waitForActivity" pattern docs/plan/02-architecture.md §2.3 names —
// each message drained from the channel triggers re-issuing the same
// listen command, until the job sends its final result and closes it.
type ProgressScreen struct {
	keys      KeyMap
	req       install.Request
	installer Installer

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
	result   install.Result
	err      error
}

// NewProgressScreen returns a screen that will run req through installer
// once pushed.
func NewProgressScreen(req install.Request, installer Installer) *ProgressScreen {
	ctx, cancel := context.WithCancel(context.Background())
	return &ProgressScreen{
		keys:      DefaultKeyMap(),
		req:       req,
		installer: installer,
		ctx:       ctx,
		cancel:    cancel,
		bar:       progress.New(),
	}
}

// Init implements Screen: it starts the install and begins listening for
// its progress.
func (s *ProgressScreen) Init() tea.Cmd {
	s.ch = StreamJob(s.ctx, func(ctx context.Context, send func(tea.Msg)) {
		result, err := s.installer.Install(ctx, s.req, func(u ProgressUpdate) {
			send(progressUpdateMsg(u))
		})
		send(installDoneMsg{result: result, err: err})
	})
	return WaitForActivity(s.ch)
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

	case installDoneMsg:
		s.finished = true
		s.result = msg.result
		s.err = msg.err
		s.canceled = msg.err != nil && s.ctx.Err() != nil
		return s, PushScreen(NewInstallResultScreen(s.req, s.result, s.err, s.canceled))
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
	case s.finished && s.err == nil:
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
