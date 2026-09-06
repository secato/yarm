package app

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/config"
	"github.com/secato/yarm/internal/install"
)

// settingsRow is one editable field on the main settings view.
type settingsRow int

const (
	rowFlavor settingsRow = iota
	rowCacheDir
	rowTTL
	rowManualGames
	settingsRowCount
)

func (r settingsRow) label() string {
	switch r {
	case rowFlavor:
		return "Default flavor"
	case rowCacheDir:
		return "Cache directory"
	case rowTTL:
		return "Catalog TTL (hours)"
	case rowManualGames:
		return "Manual games"
	default:
		return "?"
	}
}

// settingsMode is which sub-view the screen is in.
type settingsMode int

const (
	modeRows        settingsMode = iota
	modeEditText                 // editing cache dir or TTL via s.input
	modeManualGames              // the manual-games sub-list
)

// SettingsScreen edits a working copy of config.Config and writes it back
// on request. Every change here takes effect on the next launch: the
// cache directory and the game providers are both resolved once at
// startup, and re-wiring either live is not worth the complexity it would
// add for a local, restart-anytime tool.
type SettingsScreen struct {
	keys      KeyMap
	configDir string

	saved   config.Config // what is on disk, to detect unsaved changes
	pending config.Config // the working copy this screen edits

	mode   settingsMode
	cursor int // row cursor in modeRows, or manual-games entry cursor

	input     textinput.Model
	editField settingsRow

	status string
}

// NewSettingsScreen returns the settings screen, editing a copy of cfg.
func NewSettingsScreen(configDir string, cfg config.Config) *SettingsScreen {
	return &SettingsScreen{
		keys:      DefaultKeyMap(),
		configDir: configDir,
		saved:     cfg,
		pending:   cfg,
		input:     textinput.New(),
	}
}

// Init implements Screen.
func (s *SettingsScreen) Init() tea.Cmd { return nil }

// Title implements Screen.
func (s *SettingsScreen) Title() string {
	if s.dirty() {
		return "settings — unsaved changes"
	}
	return "settings"
}

// dirty reports whether the working copy differs from what was last
// saved (or loaded).
func (s *SettingsScreen) dirty() bool {
	return !reflect.DeepEqual(s.saved, s.pending)
}

// settingsSaveBinding writes the working copy to config.yaml. Safe to
// reuse "s" here: which screen is active decides what "s" means, and
// while this screen is active it can only mean "save".
var (
	settingsSaveBinding   = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save"))
	settingsDeleteBinding = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
)

// KeyBindings implements Screen.
func (s *SettingsScreen) KeyBindings() []key.Binding {
	switch s.mode {
	case modeEditText:
		return []key.Binding{s.keys.Enter, s.keys.Back}
	case modeManualGames:
		return []key.Binding{s.keys.AddGame, settingsDeleteBinding, s.keys.Back}
	default:
		return []key.Binding{s.keys.Up, s.keys.Down, s.keys.Enter, settingsSaveBinding, s.keys.Back}
	}
}

// CapturesInput routes plain keys to the text field while editing.
func (s *SettingsScreen) CapturesInput() bool { return s.mode == modeEditText }

// HandleBack implements backHandler: a sub-mode backs out to row
// navigation; row navigation itself defers to the shell.
func (s *SettingsScreen) HandleBack() (Screen, tea.Cmd, bool) {
	if s.mode == modeRows {
		return s, nil, false
	}
	s.mode = modeRows
	return s, nil, true
}

// Update implements Screen.
func (s *SettingsScreen) Update(msg tea.Msg, env Env) (Screen, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}

	switch s.mode {
	case modeEditText:
		return s.updateEditText(km)
	case modeManualGames:
		return s.updateManualGames(km)
	default:
		return s.updateRows(km)
	}
}

func (s *SettingsScreen) updateRows(km tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(km, s.keys.Up):
		if s.cursor > 0 {
			s.cursor--
		}
	case key.Matches(km, s.keys.Down):
		if s.cursor < int(settingsRowCount)-1 {
			s.cursor++
		}
	case key.Matches(km, settingsSaveBinding):
		return s.save()
	case key.Matches(km, s.keys.Enter):
		return s.activateRow()
	}
	return s, nil
}

func (s *SettingsScreen) activateRow() (Screen, tea.Cmd) {
	switch settingsRow(s.cursor) {
	case rowFlavor:
		if s.pending.Defaults.ReshadeFlavor == string(install.FlavorAddon) {
			s.pending.Defaults.ReshadeFlavor = string(install.FlavorNormal)
		} else {
			s.pending.Defaults.ReshadeFlavor = string(install.FlavorAddon)
		}
	case rowCacheDir:
		s.beginEdit(rowCacheDir, s.pending.CacheDir)
	case rowTTL:
		s.beginEdit(rowTTL, strconv.Itoa(s.pending.CatalogTTLHours))
	case rowManualGames:
		s.mode = modeManualGames
		s.cursor = 0
	}
	return s, textinput.Blink
}

func (s *SettingsScreen) beginEdit(field settingsRow, value string) {
	s.mode = modeEditText
	s.editField = field
	s.input.SetValue(value)
	s.input.CursorEnd()
	s.input.Focus()
}

func (s *SettingsScreen) updateEditText(km tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch km.String() {
	case "esc":
		s.mode = modeRows
		s.input.Blur()
		return s, nil
	case "enter":
		return s.commitEdit()
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(km)
	return s, cmd
}

func (s *SettingsScreen) commitEdit() (Screen, tea.Cmd) {
	value := strings.TrimSpace(s.input.Value())

	switch s.editField {
	case rowCacheDir:
		s.pending.CacheDir = value
	case rowTTL:
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			s.status = "catalog TTL must be a positive number of hours"
			return s, nil
		}
		s.pending.CatalogTTLHours = n
	}

	s.status = ""
	s.mode = modeRows
	s.input.Blur()
	return s, nil
}

func (s *SettingsScreen) updateManualGames(km tea.KeyPressMsg) (Screen, tea.Cmd) {
	games := s.pending.ManualGames
	switch {
	case key.Matches(km, s.keys.Up):
		if s.cursor > 0 {
			s.cursor--
		}
	case key.Matches(km, s.keys.Down):
		if s.cursor < len(games)-1 {
			s.cursor++
		}
	case key.Matches(km, s.keys.AddGame):
		return s.startAddManualGame()
	case key.Matches(km, settingsDeleteBinding):
		if s.cursor >= 0 && s.cursor < len(games) {
			s.pending.ManualGames = append(games[:s.cursor:s.cursor], games[s.cursor+1:]...)
			if s.cursor >= len(s.pending.ManualGames) {
				s.cursor = len(s.pending.ManualGames) - 1
			}
		}
	}
	return s, nil
}

// startAddManualGame reuses AddFolderScreen's own path validation rather
// than duplicating it; its handler appends straight into this screen's
// pending config through the closure below, which is safe because s is a
// pointer restored unchanged when the pushed screen is popped back off.
func (s *SettingsScreen) startAddManualGame() (Screen, tea.Cmd) {
	af := NewAddFolderScreen().WithHandler(func(name, path string) tea.Cmd {
		s.pending.ManualGames = append(s.pending.ManualGames, config.ManualGame{Name: name, Path: path})
		return SetStatus(fmt.Sprintf("added %s — press s to save", name))
	})
	return s, PushScreen(af)
}

func (s *SettingsScreen) save() (Screen, tea.Cmd) {
	if err := s.pending.Validate(); err != nil {
		s.status = err.Error()
		return s, nil
	}
	if err := config.Save(s.configDir, s.pending); err != nil {
		s.status = err.Error()
		return s, ReportError(err)
	}
	s.saved = s.pending
	s.status = "saved — takes effect next launch"
	return s, nil
}

// View implements Screen.
func (s *SettingsScreen) View(env Env) string {
	if s.mode == modeManualGames {
		return s.viewManualGames(env)
	}

	var b strings.Builder
	for r := settingsRow(0); r < settingsRowCount; r++ {
		marker := "  "
		if int(r) == s.cursor && s.mode == modeRows {
			marker = "▸ "
		}
		line := marker + r.label() + ": " + s.rowValue(r)
		if int(r) == s.cursor && s.mode == modeRows {
			line = env.Styles.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	if s.mode == modeEditText {
		b.WriteString("\n")
		b.WriteString(s.editField.label() + ": ")
		b.WriteString(s.input.View())
	}

	if s.status != "" {
		b.WriteString("\n\n")
		b.WriteString(env.Styles.Faint.Render(s.status))
	}
	if s.dirty() {
		b.WriteString("\n")
		b.WriteString(env.Styles.Warn.Render("unsaved changes — press s to save"))
	}
	return b.String()
}

func (s *SettingsScreen) rowValue(r settingsRow) string {
	switch r {
	case rowFlavor:
		return s.pending.Defaults.ReshadeFlavor
	case rowCacheDir:
		if s.pending.CacheDir == "" {
			return "(default)"
		}
		return s.pending.CacheDir
	case rowTTL:
		return strconv.Itoa(s.pending.CatalogTTLHours)
	case rowManualGames:
		return fmt.Sprintf("%d folder(s)", len(s.pending.ManualGames))
	default:
		return ""
	}
}

func (s *SettingsScreen) viewManualGames(env Env) string {
	var b strings.Builder
	b.WriteString(env.Styles.Subtitle.Render("Manual games"))
	b.WriteString("\n\n")

	if len(s.pending.ManualGames) == 0 {
		b.WriteString(env.Styles.Faint.Render("none yet — press a to add a folder"))
		b.WriteString("\n")
	}
	for i, g := range s.pending.ManualGames {
		marker := "  "
		if i == s.cursor {
			marker = "▸ "
		}
		line := fmt.Sprintf("%s%s", marker, g.Name)
		if i == s.cursor {
			line = env.Styles.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render("    " + g.Path))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(env.Styles.Faint.Render("a add folder · d remove · esc back"))
	return b.String()
}
