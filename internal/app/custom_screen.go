package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/catalog"
)

// customLoadedMsg carries a scan of the custom folder, success or failure.
//
// Built as a plain tea.Cmd rather than through the Async helper: Async
// would route a scan failure to the shell's generic error overlay,
// bypassing this screen's Update entirely and leaving "scanning…" on
// screen forever once the dialog closed.
type customLoadedMsg struct {
	shaders, addons []catalog.Custom
	err             error
}

// CustomScreen shows the two user-managed content folders — shaders and
// add-ons — and what yarm found in them. There is no editing here: the
// user manages the folders themselves, so this is a read-only view plus a
// way to see exactly where they are.
type CustomScreen struct {
	keys KeyMap
	dir  string

	loading bool
	shaders []catalog.Custom
	addons  []catalog.Custom

	// rows is the flattened, cursor-addressable list: two section
	// headers (which the cursor skips, same convention as multiSelect)
	// followed by their entries.
	rows   []customRow
	cursor int
}

// customPrintBinding shows the highlighted folder's full path in the
// status bar: the folder is the user's to manage, so the useful thing
// yarm can offer is where it is.
var customPrintBinding = key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "print path"))

type customRow struct {
	header bool
	title  string // header text, or the entry's name
	detail string // description, or the folder path for a header row
	path   string
}

// NewCustomScreen returns the custom-content screen, which scans
// the custom-content folder on Init.
func NewCustomScreen(customDir string) *CustomScreen {
	return &CustomScreen{keys: DefaultKeyMap(), dir: customDir, loading: true}
}

// Init implements Screen.
func (s *CustomScreen) Init() tea.Cmd {
	dir := s.dir
	return func() tea.Msg {
		found, err := catalog.ScanCustom(dir)
		if err != nil {
			return customLoadedMsg{err: err}
		}
		var shaders, addons []catalog.Custom
		for _, c := range found {
			switch c.Kind {
			case catalog.CustomShaders:
				shaders = append(shaders, c)
			case catalog.CustomAddons:
				addons = append(addons, c)
			}
		}
		return customLoadedMsg{shaders: shaders, addons: addons}
	}
}

// Title implements Screen.
func (s *CustomScreen) Title() string { return "custom content" }

// KeyBindings implements Screen.
func (s *CustomScreen) KeyBindings() []key.Binding {
	return []key.Binding{s.keys.Up, s.keys.Down, customPrintBinding, s.keys.Back}
}

// Update implements Screen.
func (s *CustomScreen) Update(msg tea.Msg, env Env) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case customLoadedMsg:
		s.loading = false
		if msg.err != nil {
			return s, ReportError(msg.err)
		}
		s.shaders, s.addons = msg.shaders, msg.addons
		s.buildRows()
		return s, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, s.keys.Up):
			s.moveCursor(-1)
		case key.Matches(msg, s.keys.Down):
			s.moveCursor(1)
		case key.Matches(msg, customPrintBinding):
			return s, s.printSelectedPath()
		}
	}
	return s, nil
}

// printSelectedPath puts the highlighted row's path in the status bar.
func (s *CustomScreen) printSelectedPath() tea.Cmd {
	if s.cursor < 0 || s.cursor >= len(s.rows) {
		return nil
	}
	row := s.rows[s.cursor]
	if row.path == "" {
		return nil
	}
	return SetStatus(row.path)
}

// buildRows flattens the two sections into one cursor-addressable list,
// always showing the section header (with its folder path) even when
// empty, so the user can find where to put things.
func (s *CustomScreen) buildRows() {
	s.rows = s.rows[:0]

	s.rows = append(s.rows, customRow{
		header: true, title: "Shaders",
		detail: filepath.Join(s.dir, catalog.CustomShadersDir),
		path:   filepath.Join(s.dir, catalog.CustomShadersDir),
	})
	for _, c := range s.shaders {
		s.rows = append(s.rows, customRow{title: c.Name, detail: c.Description, path: c.Path})
	}

	s.rows = append(s.rows, customRow{
		header: true, title: "Add-ons",
		detail: filepath.Join(s.dir, catalog.CustomAddonsDir),
		path:   filepath.Join(s.dir, catalog.CustomAddonsDir),
	})
	for _, c := range s.addons {
		s.rows = append(s.rows, customRow{title: c.Name, detail: c.Description, path: c.Path})
	}

	if s.cursor < 0 {
		s.cursor = 0
	}
	if s.cursor >= len(s.rows) {
		s.cursor = len(s.rows) - 1
	}
}

func (s *CustomScreen) moveCursor(delta int) {
	next := s.cursor + delta
	if next < 0 || next >= len(s.rows) {
		return
	}
	s.cursor = next
}

// View implements Screen.
func (s *CustomScreen) View(env Env) string {
	if s.loading {
		return env.Styles.Faint.Render("scanning the custom folder…")
	}

	// A header costs three lines and an entry with a description two, so
	// reserve room for the busiest case rather than assuming one line each;
	// the footer below takes two more.
	const perRow = 2
	const footer = 2
	visible := (env.Height - footer) / perRow
	if visible < 3 {
		visible = 3
	}

	var b strings.Builder
	writeWindow(&b, env, len(s.rows), s.cursor, visible, "", func(i int) {
		row := s.rows[i]
		switch {
		case row.header:
			b.WriteString("\n")
			b.WriteString(env.Styles.Subtitle.Render(row.title))
			b.WriteString("\n")
			b.WriteString(env.Styles.Faint.Render(clipTail(row.detail, env.Width)))
			b.WriteString("\n")
		default:
			marker := "  "
			if i == s.cursor {
				marker = "▸ "
			}
			line := clipTail(marker+row.title, env.Width)
			if i == s.cursor {
				line = env.Styles.Selected.Render(line)
			}
			b.WriteString(line)
			b.WriteString("\n")
			if row.detail != "" {
				b.WriteString(env.Styles.Faint.Render(clipTail("    "+row.detail, env.Width)))
				b.WriteString("\n")
			}
		}
	})

	if len(s.shaders) == 0 && len(s.addons) == 0 {
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render(
			"Nothing found yet. Drop a shader pack or add-on into one of the folders above."))
	}

	b.WriteString("\n")
	b.WriteString(env.Styles.Faint.Render(fmt.Sprintf(
		"%d shader pack(s), %d add-on(s)", len(s.shaders), len(s.addons))))
	return b.String()
}
