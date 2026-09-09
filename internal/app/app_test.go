package app

import (
	"bytes"
	"runtime"
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
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps(), false)))

	for _, want := range []string{
		"Vantage Point Deluxe",
		"Ember Hollow",
		"Ridgeline",
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
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps(), false)))

	waitForText(t, s, "6.8.0 addon")
	waitForText(t, s, "native build")
	waitForText(t, s, "no .exe found")
	finish(t, s)
}

func TestGamesScreenEmptyState(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(fakeLoader{}, fakeDeps(), false)))

	waitForText(t, s, "No games found")
	waitForText(t, s, "add a game folder")
	finish(t, s)
}

// A discovery failure must surface in the UI, not take the program down.
func TestGamesScreenReportsLoadError(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(errLoader, fakeDeps(), false)))

	waitForText(t, s, "Something went wrong")
	waitForText(t, s, "steam library is unreadable")

	m := finish(t, s)
	if m.overlay == nil {
		t.Error("the error overlay should still be open")
	}

	// A discovery failure must not leave the screen stuck claiming it is
	// still scanning: "r" would still silently retry, but the title
	// would go on saying "scanning…" forever, which is what this checks.
	gs, ok := m.Screen().(*GamesScreen)
	if !ok {
		t.Fatalf("screen = %T, want *GamesScreen", m.Screen())
	}
	if gs.loading {
		t.Error("loading should be false once the (failed) load has been handled")
	}
	if got := gs.Title(); strings.Contains(got, "scanning") {
		t.Errorf("Title() = %q, should not still claim to be scanning", got)
	}
}

func TestHelpOverlayListsScreenBindings(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps(), false)))
	waitForText(t, s, "Ember Hollow")

	s.send(tea.KeyPressMsg{Code: '?', Text: "?"})
	waitForText(t, s, "Keys")
	waitForText(t, s, "rescan")

	finish(t, s)
}

func TestGameDetailRenders(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps(), false)))
	waitForText(t, s, "Ember Hollow")

	s.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	waitForText(t, s, "Vantage Point Deluxe")
	waitForText(t, s, "Vantage.exe")

	finish(t, s)
}

func TestAddFolderScreenRejectsMissingPath(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps(), false)))
	waitForText(t, s, "Ember Hollow")

	s.send(tea.KeyPressMsg{Code: 'a', Text: "a"})
	waitForText(t, s, "add a game folder")

	// Absolute for the host: on Windows a leading slash alone is not, and
	// the screen would answer "enter an absolute path" — a different (and
	// correct) complaint than the one under test.
	missing := "/definitely/not/a/real/path"
	if runtime.GOOS == "windows" {
		missing = `C:\definitely\not\a\real\path`
	}
	s.typeText(missing)
	waitForText(t, s, "no such folder")

	finish(t, s)
}

// A real folder with no executables is valid but worth flagging.
func TestAddFolderScreenWarnsOnEmptyFolder(t *testing.T) {
	dir := t.TempDir()
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps(), false)))
	waitForText(t, s, "Ember Hollow")

	s.send(tea.KeyPressMsg{Code: 'a', Text: "a"})
	waitForText(t, s, "add a game folder")
	s.typeText(dir)
	waitForText(t, s, "holds no .exe files")

	finish(t, s)
}

// Ctrl+C must quit from anywhere, including from inside a text input that
// is otherwise swallowing keys.
func TestCtrlCQuitsFromTextInput(t *testing.T) {
	s := newTest(t, New(NewGamesScreen(fakeLoader{entries: sampleEntries()}, fakeDeps(), false)))
	waitForText(t, s, "Ember Hollow")

	s.send(tea.KeyPressMsg{Code: '/', Text: "/"})
	s.typeText("ridge")
	s.send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	s.tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}
