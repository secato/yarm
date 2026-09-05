package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// The fixed size the plan specifies, so rendered output is stable.
const (
	termWidth  = 100
	termHeight = 30
)

// session pairs a running program with everything it has drawn.
//
// The output has to be accumulated as the test goes: tm.Output() reads
// from a buffer that returns io.EOF the moment it is empty, so a single
// read captures only whatever happened to be there at that instant.
type session struct {
	tm    *teatest.TestModel
	drawn bytes.Buffer
	chunk []byte
}

// newTest starts a TestModel at a fixed size with color disabled, which is
// what makes rendered output comparable across machines and CI.
func newTest(t *testing.T, m tea.Model) *session {
	t.Helper()
	return &session{
		tm: teatest.NewTestModel(t, m,
			teatest.WithInitialTermSize(termWidth, termHeight),
			teatest.WithProgramOptions(tea.WithColorProfile(colorprofile.NoTTY)),
		),
		chunk: make([]byte, 16*1024),
	}
}

// collect drains whatever the program has written since the last call.
func (s *session) collect() string {
	out := s.tm.Output()
	for {
		n, err := out.Read(s.chunk)
		if n > 0 {
			s.drawn.Write(s.chunk[:n])
		}
		if err != nil || n == 0 {
			break
		}
	}
	return s.drawn.String()
}

// send forwards a message to the program.
func (s *session) send(msg tea.Msg) { s.tm.Send(msg) }

// typeText types a string into the program.
func (s *session) typeText(text string) { s.tm.Type(text) }

// waitForText blocks until the program has drawn want.
//
// Assertions go through this rather than scanning the final output,
// because Bubble Tea's renderer emits diffs: a line already on screen is
// not rewritten, so the closing frame holds only what changed last.
func waitForText(t *testing.T, s *session, want string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if strings.Contains(s.collect(), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q to be drawn.\nGot:\n%s", want, s.drawn.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// finish ends the program and returns the final root model, so tests can
// assert on state rather than on terminal bytes.
//
// It quits directly rather than sending "q": an open overlay or a focused
// text input deliberately swallows plain keys.
func finish(t *testing.T, s *session) Model {
	t.Helper()
	if err := s.tm.Quit(); err != nil {
		t.Fatalf("quit: %v", err)
	}
	final := s.tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second))
	m, ok := final.(Model)
	if !ok {
		t.Fatalf("final model is %T, want app.Model", final)
	}
	return m
}

func TestGamesScreenListsGames(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps())))

	for _, want := range []string{
		"Control Ultimate Edition",
		"ELDEN RING",
		"Dota 2",
		"My GOG Game",
		"steam",
		"manual",
	} {
		waitForText(t, s, want)
	}
	finish(t, s)
}

// The status column is how a user sees at a glance what is installed, and
// why a game that looks empty is not a failed scan.
func TestGamesScreenShowsStatusColumn(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps())))

	waitForText(t, s, "6.8.0 addon")
	waitForText(t, s, "native build")
	waitForText(t, s, "no .exe found")
	finish(t, s)
}

func TestGamesScreenEmptyState(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(fakeLoader{}, fakeDeps())))

	waitForText(t, s, "No games found")
	waitForText(t, s, "add a game folder")
	finish(t, s)
}

// A discovery failure must surface in the UI, not take the program down.
func TestGamesScreenReportsLoadError(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(errLoader, fakeDeps())))

	waitForText(t, s, "Something went wrong")
	waitForText(t, s, "steam library is unreadable")

	m := finish(t, s)
	if m.overlay == nil {
		t.Error("the error overlay should still be open")
	}
}

func TestHelpOverlayListsScreenBindings(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps())))
	waitForText(t, s, "ELDEN RING")

	s.send(tea.KeyPressMsg{Code: '?', Text: "?"})
	waitForText(t, s, "Keys")
	waitForText(t, s, "rescan")

	finish(t, s)
}

func TestGameDetailRenders(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps())))
	waitForText(t, s, "ELDEN RING")

	s.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	waitForText(t, s, "Control Ultimate Edition")
	waitForText(t, s, "executable(s) hidden")

	// Skipped executables appear only when asked for.
	s.send(tea.KeyPressMsg{Code: 't', Text: "t"})
	waitForText(t, s, "VC_redist.x64.exe")

	finish(t, s)
}

func TestAddFolderScreenRejectsMissingPath(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps())))
	waitForText(t, s, "ELDEN RING")

	s.send(tea.KeyPressMsg{Code: 'a', Text: "a"})
	waitForText(t, s, "add a game folder")

	s.typeText("/definitely/not/a/real/path")
	waitForText(t, s, "no such folder")

	finish(t, s)
}

// A real folder with no executables is valid but worth flagging.
func TestAddFolderScreenWarnsOnEmptyFolder(t *testing.T) {
	dir := t.TempDir()
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps())))
	waitForText(t, s, "ELDEN RING")

	s.send(tea.KeyPressMsg{Code: 'a', Text: "a"})
	waitForText(t, s, "add a game folder")
	s.typeText(dir)
	waitForText(t, s, "holds no .exe files")

	finish(t, s)
}

// Ctrl+C must quit from anywhere, including from inside a text input that
// is otherwise swallowing keys.
func TestCtrlCQuitsFromTextInput(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps())))
	waitForText(t, s, "ELDEN RING")

	s.send(tea.KeyPressMsg{Code: '/', Text: "/"})
	s.typeText("dota")
	s.send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	s.tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}
