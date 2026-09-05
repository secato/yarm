package app

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Model is the root Bubble Tea model: it owns the terminal size, the
// resolved theme, the screen stack and any overlay, and delegates
// everything else to the active screen.
type Model struct {
	keys   KeyMap
	styles Styles

	width, height int
	// ready is false until the first WindowSizeMsg arrives; rendering
	// before then would guess at a size and flash.
	ready bool

	screen  Screen
	stack   []Screen
	overlay overlay

	status   string
	quitting bool
}

// New returns a root model showing the given screen.
func New(initial Screen) Model {
	return Model{
		keys:   DefaultKeyMap(),
		styles: NewStyles(true), // replaced once the terminal answers
		screen: initial,
	}
}

// Init implements tea.Model. Requesting the background color is what
// lets NewStyles pick a palette that suits the user's terminal rather
// than guessing.
func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.screen.Init())
}

// sized gives a screen the current window dimensions, so it can lay out
// before its first render.
func (m Model) sized(s Screen) Screen {
	if !m.ready {
		return s
	}
	next, _ := s.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height}, m.env())
	return next
}

// env bundles what screens need to render.
func (m Model) env() Env {
	return Env{Styles: m.styles, Width: m.width, Height: m.bodyHeight()}
}

// chromeHeight is the number of lines the shell draws around a screen:
// title, blank, status.
const chromeHeight = 3

func (m Model) bodyHeight() int {
	h := m.height - chromeHeight
	if h < 1 {
		return 1
	}
	return h
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.styles = NewStyles(msg.IsDark())
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		// Screens lay themselves out from Env, but table-based ones need
		// the size pushed into their widgets too.
		next, cmd := m.screen.Update(msg, m.env())
		m.screen = next
		return m, cmd

	case showOverlayMsg:
		m.overlay = msg.overlay
		return m, nil

	case errorMsg:
		m.overlay = errorOverlay(msg)
		return m, nil

	case statusMsg:
		m.status = msg.text
		return m, nil

	case pushScreenMsg:
		m.stack = append(m.stack, m.screen)
		// A newly pushed screen has never seen a WindowSizeMsg, so its
		// widgets hold no columns, rows or height and would render blank.
		// Bubble Tea only sends that message on a real resize, so the
		// shell has to hand the current size over itself.
		m.screen = m.sized(msg.screen)
		return m, m.screen.Init()

	case popScreenMsg:
		if n := len(m.stack); n > 0 {
			m.screen = m.stack[n-1]
			m.stack = m.stack[:n-1]
			// The window may have been resized while this screen was
			// buried, so let it lay out again before it is drawn.
			m.screen = m.sized(m.screen)
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	next, cmd := m.screen.Update(msg, m.env())
	m.screen = next
	return m, cmd
}

// handleKey routes a key press: overlays first, then global bindings,
// then the active screen.
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// An overlay is modal: it consumes every key.
	if m.overlay != nil {
		next, cmd := m.overlay.update(msg)
		m.overlay = next
		return m, cmd
	}

	// ctrl+c always quits, whatever a screen is doing, because a user who
	// presses it expects out.
	if msg.String() == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}

	// A screen that is capturing text (a filter or a path input) must see
	// plain letters before any global single-key binding does.
	if c, ok := m.screen.(interface{ CapturesInput() bool }); ok && c.CapturesInput() {
		next, cmd := m.screen.Update(msg, m.env())
		m.screen = next
		return m, cmd
	}

	switch {
	case key.Matches(msg, m.keys.Help):
		m.overlay = newHelpOverlay(m.keys, m.screen.KeyBindings())
		return m, nil

	case key.Matches(msg, m.keys.Quit):
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, m.keys.Back) && len(m.stack) > 0:
		return m, PopScreen()
	}

	m.status = ""
	next, cmd := m.screen.Update(msg, m.env())
	m.screen = next
	return m, cmd
}

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "yarm"
	return v
}

func (m Model) render() string {
	if !m.ready {
		return "starting yarm…"
	}
	if m.quitting {
		return ""
	}

	env := m.env()
	body := m.screen.View(env)

	header := m.styles.Title.Render("yarm") + "  " +
		m.styles.Faint.Render(m.screen.Title())

	status := m.status
	if status == "" {
		status = m.helpLine()
	}

	frame := lipgloss.JoinVertical(lipgloss.Left,
		header,
		body,
		m.styles.StatusBar.Render(status),
	)

	if m.overlay == nil {
		return frame
	}

	// Draw the overlay centered over the frame. lipgloss.Place fills the
	// window, so the box lands in the middle of an otherwise blank field
	// rather than being composited over the body; that keeps the modal
	// unambiguous in a terminal with no transparency.
	box := m.styles.Overlay.Width(maxOverlayWidth(env)).Render(m.overlay.view(env))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// helpLine renders the short help for the global and screen bindings.
func (m Model) helpLine() string {
	parts := make([]string, 0, 6)
	for _, k := range m.screen.KeyBindings() {
		h := k.Help()
		if h.Key == "" {
			continue
		}
		parts = append(parts, h.Key+" "+h.Desc)
	}
	parts = append(parts, "? help", "q quit")
	return strings.Join(parts, "  •  ")
}

// Screen exposes the active screen, for tests.
func (m Model) Screen() Screen { return m.screen }
