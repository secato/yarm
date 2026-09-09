package app

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/install"
)

// fakeInstaller lets tests control exactly what an install run reports and
// returns, without touching the network, the cache or a real game
// directory.
type fakeInstaller struct {
	// updates are sent in order before the run finishes.
	updates []ProgressUpdate
	result  install.Result
	err     error
	// blockUntilCanceled, when set, makes Install wait for ctx to be
	// canceled before returning — the shape a real download blocks in.
	blockUntilCanceled bool
}

func (f *fakeInstaller) Install(ctx context.Context, req install.Request, send func(ProgressUpdate)) (install.Result, error) {
	for _, u := range f.updates {
		send(u)
	}
	if f.blockUntilCanceled {
		<-ctx.Done()
		return f.result, ctx.Err()
	}
	return f.result, f.err
}

// drainProgress pumps a ProgressScreen through its own Init/Update cycle
// until it reaches applyDoneMsg, following the WaitForActivity command
// chain exactly as the real Bubble Tea loop would.
func drainProgress(t *testing.T, s *ProgressScreen, env Env, onEachUpdate func()) Screen {
	t.Helper()
	var scr Screen = s

	cmd := s.Init()
	for i := 0; cmd != nil && i < 1000; i++ {
		msg := cmd()
		if msg == nil {
			return scr
		}
		var next Screen
		next, cmd = scr.Update(msg, env)
		scr = next
		if onEachUpdate != nil {
			onEachUpdate()
		}
		if _, ok := msg.(applyDoneMsg); ok {
			return scr
		}
	}
	t.Fatal("progress screen never reached applyDoneMsg")
	return scr
}

func singleInstallOp(req install.Request) []folderOp {
	return []folderOp{{Install: &req}}
}

func TestProgressScreenStreamsUpdatesAndFinishes(t *testing.T) {
	fi := &fakeInstaller{
		updates: []ProgressUpdate{
			{Label: "Downloading ReShade 6.8.0 (addon)"},
			{Label: "Downloading ReShade 6.8.0 (addon)", Done: 50, Total: 100},
			{Label: "Copying dxgi.dll"},
		},
		result: install.Result{Written: []string{"Game/dxgi.dll"}},
	}
	s := NewProgressScreen(singleInstallOp(install.Request{Version: "6.8.0", Flavor: install.FlavorAddon}), fi, nil)
	env := Env{Styles: NewStyles(true), Width: 80, Height: 24}

	final := drainProgress(t, s, env, nil)

	ps, ok := final.(*ProgressScreen)
	if !ok {
		t.Fatalf("final screen is %T, want *ProgressScreen", final)
	}
	if !ps.finished {
		t.Error("finished should be true once applyDoneMsg arrives")
	}
	if ps.failed() {
		t.Errorf("failed() = true, want false")
	}
	if len(ps.lines) != 2 {
		// The duplicate "Downloading ReShade..." label collapses into one
		// log line since only the byte counts changed, not the label.
		t.Errorf("lines = %v, want 2 (dedup on unchanged label)", ps.lines)
	}
}

// esc must cancel the run rather than merely leaving the screen — a
// half-finished install left running in the background would be worse
// than either finishing or rolling back.
func TestProgressScreenEscCancelsRun(t *testing.T) {
	fi := &fakeInstaller{blockUntilCanceled: true}
	s := NewProgressScreen(singleInstallOp(install.Request{Version: "6.8.0"}), fi, nil)
	env := Env{Styles: NewStyles(true), Width: 80, Height: 24}

	cmd := s.Init()

	_, _, handled := s.HandleBack()
	if !handled {
		t.Fatal("HandleBack() should handle esc while a job is running")
	}

	if s.ctx.Err() == nil {
		t.Fatal("canceling should cancel the run's context")
	}

	// The blocked fakeInstaller now returns ctx.Err(); drain until done.
	final := drainToDone(t, s, cmd, env)
	ps := final.(*ProgressScreen)
	if !ps.finished {
		t.Fatal("the screen should still reach finished after a cancel")
	}
	if !ps.canceled {
		t.Error("canceled should be true when the context was the cause of the error")
	}
}

// drainToDone continues a stream from an already-issued command, used
// when the test needs to trigger something (like a cancel) partway
// through rather than draining start-to-finish.
func drainToDone(t *testing.T, s *ProgressScreen, cmd tea.Cmd, env Env) Screen {
	t.Helper()
	var scr Screen = s
	for i := 0; cmd != nil && i < 1000; i++ {
		msg := cmd()
		if msg == nil {
			return scr
		}
		var next Screen
		next, cmd = scr.Update(msg, env)
		scr = next
		if _, ok := msg.(applyDoneMsg); ok {
			return scr
		}
	}
	t.Fatal("progress screen never reached applyDoneMsg after cancel")
	return scr
}

func TestProgressScreenReportsRealError(t *testing.T) {
	fi := &fakeInstaller{err: errors.New("disk is full")}
	s := NewProgressScreen(singleInstallOp(install.Request{Version: "6.8.0"}), fi, nil)
	env := Env{Styles: NewStyles(true), Width: 80, Height: 24}

	final := drainProgress(t, s, env, nil)
	ps := final.(*ProgressScreen)

	if ps.canceled {
		t.Error("a real failure must not be reported as canceled")
	}
	if !ps.failed() {
		t.Fatal("failed() = false, want true")
	}
	if err := ps.outcomes[0].Err; err == nil || err.Error() != "disk is full" {
		t.Errorf("outcomes[0].Err = %v, want the installer's error", err)
	}
}

// Once finished, esc must not cancel again (there is nothing left to
// cancel) — it should defer to the shell.
func TestProgressScreenBackAfterFinishDefers(t *testing.T) {
	fi := &fakeInstaller{result: install.Result{}}
	s := NewProgressScreen(singleInstallOp(install.Request{Version: "6.8.0"}), fi, nil)
	env := Env{Styles: NewStyles(true), Width: 80, Height: 24}

	final := drainProgress(t, s, env, nil)
	ps := final.(*ProgressScreen)

	_, _, handled := ps.HandleBack()
	if handled {
		t.Error("HandleBack() after finishing should defer to the shell (handled=false)")
	}
}

func TestProgressPercent(t *testing.T) {
	s := &ProgressScreen{}
	if got := s.percent(); got != -1 {
		t.Errorf("percent() with no total = %v, want -1", got)
	}
	s.done, s.total = 50, 100
	if got := s.percent(); got != 0.5 {
		t.Errorf("percent() = %v, want 0.5", got)
	}
	s.done = 150 // a generous server reporting more than Content-Length promised
	if got := s.percent(); got != 1 {
		t.Errorf("percent() over 100%% should clamp to 1, got %v", got)
	}
}

// Sanity: the streaming plumbing itself (StreamJob/WaitForActivity)
// delivers messages in order and terminates.
func TestStreamJobDeliversInOrderAndCloses(t *testing.T) {
	ch := StreamJob(context.Background(), func(ctx context.Context, send func(tea.Msg)) {
		send("one")
		send("two")
	})

	got := []string{}
	cmd := WaitForActivity(ch)
	for i := 0; i < 10; i++ {
		msg := cmd()
		if msg == nil {
			break
		}
		got = append(got, msg.(string))
		cmd = WaitForActivity(ch)
	}

	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Errorf("got %v, want [one two]", got)
	}
}

func TestStreamJobRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	ch := StreamJob(ctx, func(ctx context.Context, send func(tea.Msg)) {
		close(started)
		<-ctx.Done() // would block forever if send ignored cancellation
		send("after cancel")
	})

	<-started
	cancel()

	done := make(chan struct{})
	go func() {
		WaitForActivity(ch)()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForActivity did not return after cancellation")
	}
}
