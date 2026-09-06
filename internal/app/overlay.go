package app

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// overlay is a modal drawn over the current screen. Only one is shown at a
// time, and it takes every key press until dismissed.
type overlay interface {
	// update handles a key press. Returning nil dismisses the overlay.
	update(msg tea.KeyPressMsg) (overlay, tea.Cmd)
	// view renders the overlay's box contents.
	view(env Env) string
}

// helpOverlay lists every binding, global and screen-specific.
type helpOverlay struct {
	model  help.Model
	keys   KeyMap
	screen []key.Binding
}

func newHelpOverlay(keys KeyMap, screenKeys []key.Binding) helpOverlay {
	h := help.New()
	h.ShowAll = true
	return helpOverlay{model: h, keys: keys, screen: screenKeys}
}

func (h helpOverlay) update(msg tea.KeyPressMsg) (overlay, tea.Cmd) {
	// Any key closes help.
	_ = msg
	return nil, nil
}

func (h helpOverlay) view(env Env) string {
	var b strings.Builder
	b.WriteString(env.Styles.Title.Render("Keys"))
	b.WriteString("\n\n")
	b.WriteString(h.model.View(h.keys))

	if len(h.screen) > 0 {
		b.WriteString("\n\n")
		b.WriteString(env.Styles.Subtitle.Render("This screen"))
		b.WriteString("\n")
		for _, k := range h.screen {
			hk := k.Help()
			b.WriteString("  " + env.Styles.Accent.Render(hk.Key) + "  " +
				env.Styles.Faint.Render(hk.Desc) + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(env.Styles.Faint.Render("press any key to close"))
	return b.String()
}

// confirmOverlay asks a yes/no question and runs a command on yes.
type confirmOverlay struct {
	question string
	detail   string
	keys     KeyMap
	onYes    tea.Cmd
}

// Confirm returns a command that opens a confirmation dialog.
func Confirm(question, detail string, onYes tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		return showOverlayMsg{overlay: confirmOverlay{
			question: question, detail: detail, keys: DefaultKeyMap(), onYes: onYes,
		}}
	}
}

func (c confirmOverlay) update(msg tea.KeyPressMsg) (overlay, tea.Cmd) {
	switch {
	case key.Matches(msg, c.keys.Confirm):
		return nil, c.onYes
	case key.Matches(msg, c.keys.Cancel):
		return nil, nil
	}
	return c, nil
}

func (c confirmOverlay) view(env Env) string {
	var b strings.Builder
	b.WriteString(env.Styles.Subtitle.Render(c.question))
	if c.detail != "" {
		b.WriteString("\n\n")
		b.WriteString(env.Styles.Faint.Render(c.detail))
	}
	b.WriteString("\n\n")
	b.WriteString(env.Styles.Good.Render("y") + env.Styles.Faint.Render(" yes") + "    " +
		env.Styles.Bad.Render("n/esc/enter") + env.Styles.Faint.Render(" no"))
	return b.String()
}

// errorOverlay shows a failure without tearing the program down.
type errorOverlay errorMsg

func (e errorOverlay) update(tea.KeyPressMsg) (overlay, tea.Cmd) { return nil, nil }

func (e errorOverlay) view(env Env) string {
	return env.Styles.Bad.Render("Something went wrong") + "\n\n" +
		wrap(friendlyError(e.err), maxOverlayWidth(env)) + "\n\n" +
		env.Styles.Faint.Render("press any key to continue")
}

// showOverlayMsg opens an overlay.
type showOverlayMsg struct{ overlay overlay }

// maxOverlayWidth keeps an overlay from spanning an ultra-wide terminal.
func maxOverlayWidth(env Env) int {
	const cap = 72
	w := env.Width - 12
	if w > cap {
		w = cap
	}
	if w < 20 {
		w = 20
	}
	return w
}

// wrap breaks text to a width without splitting words where it can help
// it.
func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out strings.Builder
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		col := 0
		for j, word := range strings.Fields(line) {
			switch {
			case j == 0:
				out.WriteString(word)
				col = lipgloss.Width(word)
			case col+1+lipgloss.Width(word) > width:
				out.WriteByte('\n')
				out.WriteString(word)
				col = lipgloss.Width(word)
			default:
				out.WriteByte(' ')
				out.WriteString(word)
				col += 1 + lipgloss.Width(word)
			}
		}
	}
	return out.String()
}
