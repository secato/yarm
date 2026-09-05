package app

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Env is everything a screen needs from the shell to render itself.
// Passing it in rather than letting screens hold their own copy keeps a
// single source of truth for size and theme after a resize.
type Env struct {
	Styles Styles
	Width  int
	Height int
}

// Screen is one full-window view. Screens are values: Update returns the
// next screen rather than mutating, matching Bubble Tea's model.
type Screen interface {
	// Init returns any command to run when the screen becomes active.
	Init() tea.Cmd
	// Update handles a message and returns the resulting screen.
	Update(msg tea.Msg, env Env) (Screen, tea.Cmd)
	// View renders the screen body, excluding the title and status bars
	// the shell draws.
	View(env Env) string
	// Title is shown in the header.
	Title() string
	// KeyBindings are the screen's own bindings, shown in help alongside
	// the global ones.
	KeyBindings() []key.Binding
}

// pushScreenMsg asks the shell to open a screen, keeping the current one
// on the stack.
type pushScreenMsg struct{ screen Screen }

// popScreenMsg asks the shell to return to the previous screen.
type popScreenMsg struct{}

// popToRootMsg asks the shell to discard the whole stack and return to the
// bottom-most screen, re-initializing it so it refreshes its own data (a
// game's install status, for instance) rather than showing whatever it last
// held.
type popToRootMsg struct{}

// backHandler lets a screen intercept the Back binding itself — a wizard
// stepping back one page rather than closing outright, or a running
// progress screen treating esc as "cancel" rather than "leave". Returning
// handled=false defers to the shell's normal pop.
type backHandler interface {
	HandleBack() (next Screen, cmd tea.Cmd, handled bool)
}

// errorMsg reports a failure that should be shown to the user rather than
// crashing the program.
type errorMsg struct{ err error }

// statusMsg sets a transient line in the status bar.
type statusMsg struct{ text string }

// PushScreen returns a command that opens a screen.
func PushScreen(s Screen) tea.Cmd {
	return func() tea.Msg { return pushScreenMsg{screen: s} }
}

// PopScreen returns a command that goes back one screen.
func PopScreen() tea.Cmd {
	return func() tea.Msg { return popScreenMsg{} }
}

// PopToRoot returns a command that discards the whole navigation stack and
// returns to (and reloads) the home screen. Used once a wizard or an
// uninstall has finished: the flow that led here no longer means anything,
// and the game list needs to reflect what just changed.
func PopToRoot() tea.Cmd {
	return func() tea.Msg { return popToRootMsg{} }
}

// ReportError returns a command that surfaces an error in the UI.
func ReportError(err error) tea.Cmd {
	if err == nil {
		return nil
	}
	return func() tea.Msg { return errorMsg{err: err} }
}

// SetStatus returns a command that puts text in the status bar.
func SetStatus(text string) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: text} }
}
