package app

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

// GameDetailScreen shows one game's folders, what ReShade status applies
// to each, and its executables. ReShade applies to a folder as a whole —
// not to any one executable in it — so this screen's cursor moves between
// folders, not executables, and only appears at all when a game has more
// than one; i/u/a act on whichever folder is current (the only one, when
// there is just one).
type GameDetailScreen struct {
	entry  GameEntry
	deps   Deps
	keys   KeyMap
	cursor cursorList
}

// NewGameDetailScreen returns the detail view for a game.
func NewGameDetailScreen(entry GameEntry, deps Deps) *GameDetailScreen {
	return &GameDetailScreen{
		entry:  entry,
		deps:   deps,
		keys:   DefaultKeyMap(),
		cursor: newCursorList(len(entry.Groups), 0),
	}
}

// uninstallDoneMsg carries an uninstall run's result back to the screen
// that started it, so it can hand off to a result screen.
type uninstallDoneMsg struct {
	exePath string
	result  install.UninstallResult
}

// adoptDoneMsg carries an adopt run's result back to the screen that
// started it, so it can hand off to a result screen.
type adoptDoneMsg struct {
	exePath string
	install state.Install
}

// Init implements Screen.
func (s *GameDetailScreen) Init() tea.Cmd { return nil }

// Title implements Screen.
func (s *GameDetailScreen) Title() string { return "game — " + s.entry.Name }

// currentGroup returns the folder the cursor is on, or the game's only
// one when it has just a single folder (no cursor needed to pick it).
func (s *GameDetailScreen) currentGroup() (FolderGroup, bool) {
	i := s.cursor.Cursor()
	if i < 0 || i >= len(s.entry.Groups) {
		return FolderGroup{}, false
	}
	return s.entry.Groups[i], true
}

// multi reports whether this game has more than one folder — the only
// case where a folder cursor, or naming which folder something applies
// to, means anything at all.
func (s *GameDetailScreen) multi() bool { return len(s.entry.Groups) > 1 }

// editInstallBinding edits a folder's existing install — a distinct key
// from Install (which only ever means "there is nothing here yet"), so
// the shortcut always matches what it does rather than overloading one
// key with two different meanings depending on state.
var editInstallBinding = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit install"))

// KeyBindings implements Screen.
func (s *GameDetailScreen) KeyBindings() []key.Binding {
	bindings := []key.Binding{s.keys.Back}
	if s.multi() {
		bindings = append([]key.Binding{s.keys.Up, s.keys.Down}, bindings...)
	}

	grp, ok := s.currentGroup()
	if !ok {
		return bindings
	}
	switch {
	case grp.Unmanaged != nil:
		bindings = append([]key.Binding{s.keys.Adopt}, bindings...)
	case len(s.entry.PlayableExes()) > 0 && grp.Installed != nil:
		bindings = append([]key.Binding{editInstallBinding, s.keys.Uninstall}, bindings...)
	case len(s.entry.PlayableExes()) > 0:
		bindings = append([]key.Binding{s.keys.Install}, bindings...)
	}
	return bindings
}

// Update implements Screen.
func (s *GameDetailScreen) Update(msg tea.Msg, env Env) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case uninstallDoneMsg:
		return s, PushScreen(NewUninstallResultScreen(msg.exePath, msg.result, nil))

	case adoptDoneMsg:
		return s, PushScreen(NewAdoptResultScreen(msg.exePath, msg.install, nil))

	case tea.KeyPressMsg:
		switch {
		case s.multi() && key.Matches(msg, s.keys.Up):
			s.cursor.up()
			return s, nil
		case s.multi() && key.Matches(msg, s.keys.Down):
			s.cursor.down()
			return s, nil
		case key.Matches(msg, s.keys.Install) || key.Matches(msg, editInstallBinding):
			return s.startInstall()
		case key.Matches(msg, s.keys.Uninstall):
			return s.startUninstall()
		case key.Matches(msg, s.keys.Adopt):
			return s.startAdopt()
		}
	}
	return s, nil
}

// startInstall opens the wizard on, or adopts, the current folder — see
// startInstallForGroup.
func (s *GameDetailScreen) startInstall() (Screen, tea.Cmd) {
	grp, ok := s.currentGroup()
	if !ok {
		return s, nil
	}
	return s, startInstallForGroup(s.entry, grp, s.deps)
}

// startUninstall confirms, then removes, the install covering the current
// folder — see startUninstallForGroup.
func (s *GameDetailScreen) startUninstall() (Screen, tea.Cmd) {
	grp, ok := s.currentGroup()
	if !ok {
		return s, nil
	}
	return s, startUninstallForGroup(s.entry, grp, s.deps)
}

// startAdopt confirms, then records, the unmanaged install found in the
// current folder — see startAdoptForGroup.
func (s *GameDetailScreen) startAdopt() (Screen, tea.Cmd) {
	grp, ok := s.currentGroup()
	if !ok {
		return s, nil
	}
	return s, startAdoptForGroup(s.entry, grp, s.deps)
}

// startInstallForGroup opens the wizard on grp — or, when it already has
// an install, on whichever executable that install is actually recorded
// against, since it may not be the folder's usual "primary" one. A folder
// yarm has not adopted an unmanaged install in yet is not a fresh-install
// candidate: attempting one would collide with the files already there,
// so this hands off to the adopt confirmation instead, same as pressing
// the adopt binding directly. Shared between GameDetailScreen (any
// folder) and GamesScreen (a game with exactly one, acted on directly
// from the list without drilling in).
func startInstallForGroup(entry GameEntry, grp FolderGroup, deps Deps) tea.Cmd {
	if grp.Unmanaged != nil {
		return startAdoptForGroup(entry, grp, deps)
	}

	target := grp.primaryExe()
	if installed, ok := grp.installedExe(); ok {
		target = installed
	}
	return PushScreen(NewWizardScreen(entry, target, deps))
}

// startUninstallForGroup confirms, then removes, the install covering
// grp — resolved to whichever executable it is actually recorded against,
// which may not be the one a fresh install would default to.
func startUninstallForGroup(entry GameEntry, grp FolderGroup, deps Deps) tea.Cmd {
	if grp.Installed == nil || deps.Uninstaller == nil {
		return nil
	}
	target, ok := grp.installedExe()
	if !ok {
		return nil
	}

	exePath := target.Path
	action := Async(context.Background(),
		func(ctx context.Context) (install.UninstallResult, error) {
			return deps.Uninstaller.Uninstall(install.UninstallRequest{
				GameID: entry.ID,
				Exe:    exePath,
			})
		},
		func(res install.UninstallResult) tea.Msg {
			return uninstallDoneMsg{exePath: exePath, result: res}
		},
	)

	return Confirm(
		"Uninstall ReShade from "+exePath+"?",
		"This removes only the files yarm created; anything you edited afterward is kept.",
		action,
	)
}

// startAdoptForGroup confirms, then records, the unmanaged install found
// in grp. The install is tied to the folder's primary executable —
// ReShade intercepts by directory, so this is not necessarily whichever
// executable a user might expect, but it is the one yarm will report
// against from now on.
func startAdoptForGroup(entry GameEntry, grp FolderGroup, deps Deps) tea.Cmd {
	if grp.Unmanaged == nil || deps.Adopter == nil {
		return nil
	}
	candidate := *grp.Unmanaged
	target := grp.primaryExe()

	exePath := target.Path
	g, executable := entry.Game, target.Executable
	action := Async(context.Background(),
		func(ctx context.Context) (state.Install, error) {
			return deps.Adopter.Adopt(g, executable, candidate)
		},
		func(in state.Install) tea.Msg {
			return adoptDoneMsg{exePath: exePath, install: in}
		},
	)

	detail := fmt.Sprintf(
		"This folder already has ReShade (%s) installed, with %d file(s): the proxy DLL, "+
			"ReShade.ini, and any shaders, textures or add-ons already there. Adopting it records "+
			"those files as yarm's own — nothing on disk changes now — so yarm can update or "+
			"uninstall this install for you from then on, the same as one it created itself.",
		candidate.DLLName, candidate.FileCount())

	return Confirm("Adopt the existing ReShade install on "+exePath+"?", detail, action)
}

// View implements Screen.
func (s *GameDetailScreen) View(env Env) string {
	var b strings.Builder
	b.WriteString(env.Styles.Faint.Render(s.entry.Root))
	b.WriteString("\n\n")

	switch {
	case s.entry.ScanErr != nil:
		b.WriteString(env.Styles.Bad.Render("Could not scan this folder"))
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render(friendlyError(s.entry.ScanErr)))
		return b.String()
	case s.entry.NativeBuild:
		b.WriteString(env.Styles.Warn.Render("Native build"))
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render(
			"This game ships a native Linux or macOS binary. ReShade proxies a DLL\n" +
				"through the Windows loader, so it does not apply here."))
		return b.String()
	case len(s.entry.Exes) == 0:
		b.WriteString(env.Styles.Faint.Render("No executables were found in this folder."))
		return b.String()
	}

	multi := s.multi()
	blocks := make([]string, len(s.entry.Groups))
	for i, grp := range s.entry.Groups {
		var gb strings.Builder
		s.writeGroup(&gb, grp, i, multi, env)
		blocks[i] = gb.String()
	}

	// A game with several folders, each with several executables, runs past
	// the window long before it runs out of folders. Scroll it by folder —
	// the unit the cursor and every action here already work in — showing
	// as many as fit around the current one and counting the rest, rather
	// than printing off the bottom with no sign of it.
	height := env.Height - countLines(b.String()) - 2 // room for both markers
	start, end := fitBlocks(blocks, s.cursor.Cursor(), height)
	if start > 0 {
		b.WriteString(env.Styles.Faint.Render(fmt.Sprintf("↑ %d more folder(s) above", start)))
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		if i > start {
			b.WriteString("\n")
		}
		b.WriteString(blocks[i])
	}
	if end < len(blocks) {
		b.WriteString(env.Styles.Faint.Render(fmt.Sprintf("↓ %d more folder(s) below", len(blocks)-end)))
		b.WriteString("\n")
	}
	return b.String()
}

// writeGroup renders one folder: its ReShade status, then its
// executables (each on one line — the folder header already says which
// directory, so an executable's own path need not repeat it). Indented
// one level, with its own "<dir>/" header, only when the game has more
// than one folder; a single-folder game (by far the common case) renders
// exactly as it always did, at the left margin.
func (s *GameDetailScreen) writeGroup(b *strings.Builder, grp FolderGroup, i int, multi bool, env Env) {
	indent := ""
	if multi {
		marker := "  "
		style := env.Styles.Subtitle
		if i == s.cursor.Cursor() {
			marker = "▸ "
			style = env.Styles.Selected
		}
		header := grp.Dir + "/"
		if grp.Dir == "" {
			header = "(game root)/"
		}
		b.WriteString(style.Render(marker + header))
		b.WriteString("\n")
		indent = "  "
	}

	b.WriteString(indentLines(reshadeStatusText(grp, env, "press a to adopt it", env.Width-1-len(indent)), indent))

	stripPrefix := ""
	if grp.Dir != "" {
		stripPrefix = grp.Dir + "/"
	}
	// Executables are context, not something to act on — ReShade covers the
	// whole folder either way — so a folder with a dozen of them lists the
	// first few and counts the rest rather than filling the window.
	const maxExes = 6
	var head strings.Builder
	writeSectionHeader(&head, env, "Executables", grp.playableCount())
	b.WriteString(indentLines(head.String(), indent))
	shown := 0
	hidden := 0
	for _, e := range grp.Exes {
		if e.Skipped {
			continue
		}
		if shown >= maxExes {
			hidden++
			continue
		}
		shown++
		name := strings.TrimPrefix(e.Path, stripPrefix)
		_, _ = fmt.Fprintf(b, "%s  %s · %s · %s\n", indent, name, e.Arch, apiLabel(e.API))
	}
	if hidden > 0 {
		b.WriteString(env.Styles.Faint.Render(fmt.Sprintf("%s  +%d more", indent, hidden)))
		b.WriteString("\n")
	}

	// Last, below everything else in this folder's block: a real safety
	// warning belongs at the bottom of the pane, not sandwiched between
	// the ReShade status and the executables list.
	if groupIsAddon(grp) {
		var warn strings.Builder
		writeAnticheatWarning(&warn, env, env.Width-1-len(indent))
		b.WriteString(indentLines(warn.String(), indent))
	}
}

// indentLines prefixes every line of s with prefix, including the first —
// used to nest a folder's ReShade status under its own header only when
// there is more than one folder to distinguish (prefix is "" otherwise, a
// no-op).
func indentLines(s, prefix string) string {
	if prefix == "" || s == "" {
		return s
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	for i, line := range lines {
		// A blank line stays blank: indenting it would leave trailing
		// spaces that show up as a stripe under a highlighted row.
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n") + "\n"
}
