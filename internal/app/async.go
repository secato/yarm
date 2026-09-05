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
