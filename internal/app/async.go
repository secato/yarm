package app

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

// Async runs work off the UI goroutine and delivers its result as a
// message.
//
// Bubble Tea already runs commands concurrently; this wrapper exists so
// the common shape — do slow work, then either report an error or deliver
// a typed result — is written once rather than in every screen, and so
// cancellation is always wired through.
func Async[T any](ctx context.Context, work func(context.Context) (T, error), onDone func(T) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		result, err := work(ctx)
		if err != nil {
			return errorMsg{err: err}
		}
		return onDone(result)
	}
}

// StreamJob starts work in a background goroutine and returns the channel
// its messages arrive on.
//
// This is for jobs that report several updates over time — a download's
// progress, a sequence of files being copied — rather than Async's single
// result. A tea.Cmd can only ever produce one message, so the work is run
// here directly (not wrapped in a Cmd) and streamed back through a
// channel; pair this with WaitForActivity to turn arrivals into messages
// the Bubble Tea loop can react to (the "waitForActivity" pattern).
//
// work must call send for everything it wants delivered, including its
// own final result — StreamJob does not add one. Once work returns, the
// channel is closed.
//
// send always blocks until read, deliberately without a ctx.Done()
// escape: cancellation is exactly the case where a message — the final
// one, carrying whatever the job returns — absolutely must still arrive,
// so the screen can report "canceled" rather than hang showing
// "canceling…" forever. This is safe because whoever calls StreamJob is
// still listening: Bubble Tea starts the Cmd WaitForActivity returns the
// moment Init hands it back, and a screen re-issues WaitForActivity after
// every message except the last, at which point work has nothing left to
// send.
func StreamJob(ctx context.Context, work func(ctx context.Context, send func(tea.Msg))) chan tea.Msg {
	ch := make(chan tea.Msg)
	go func() {
		defer close(ch)
		work(ctx, func(msg tea.Msg) { ch <- msg })
	}()
	return ch
}

// WaitForActivity returns a Cmd that blocks for the next message on ch, or
// nil once the job has closed it. The screen handling a message from a
// stream must re-issue WaitForActivity(ch) to keep listening — the Cmd
// only ever waits for one arrival.
func WaitForActivity(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}
