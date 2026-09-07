package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Most games are one folder with one ReShade install, and for those every
// action runs straight from the games list. The exception is a game whose
// executables live in several folders — ReShade attaches to a directory,
// so "install into this game" is not yet a complete instruction. That is
// the only thing this screen exists for: it asks which folder, then gets
// out of the way.
//
// It replaces a whole game-detail screen that used to sit between the list
// and every action. The list's side panel already shows what that screen
// showed, so all it really contributed was this one question, asked
// whether or not it needed asking.

// folderAction is what to do once a folder has been chosen.
type folderAction func(entry GameEntry, grp FolderGroup, deps Deps) tea.Cmd

// FolderPickScreen asks which of a game's folders an action applies to.
type FolderPickScreen struct {
	entry  GameEntry
	groups []FolderGroup
	deps   Deps
	keys   KeyMap
	cursor cursorList
	// verb names the action in the heading ("Install ReShade into which
	// folder?"), so the screen never has to be read as a general-purpose
	// browser.
	verb string
	run  folderAction
}

// pickFolder returns the command to run action on a game's folder: the
// action itself when exactly one folder is eligible, and this screen when
// several are. Nothing at all happens when none is — the key that got here
// should not have been offered.
func pickFolder(entry GameEntry, deps Deps, verb string, eligible []FolderGroup, run folderAction) tea.Cmd {
	switch len(eligible) {
	case 0:
		return nil
	case 1:
		return run(entry, eligible[0], deps)
	}
	return PushScreen(&FolderPickScreen{
		entry:  entry,
		groups: eligible,
		deps:   deps,
		keys:   DefaultKeyMap(),
		cursor: newCursorList(len(eligible), 0),
		verb:   verb,
		run:    run,
	})
}

// Init implements Screen.
func (s *FolderPickScreen) Init() tea.Cmd { return nil }

// Title implements Screen.
func (s *FolderPickScreen) Title() string { return s.entry.Name + " — which folder?" }

// KeyBindings implements Screen.
func (s *FolderPickScreen) KeyBindings() []key.Binding {
	return []key.Binding{s.keys.Up, s.keys.Down, s.keys.Enter, s.keys.Back}
}

// Update implements Screen.
func (s *FolderPickScreen) Update(msg tea.Msg, env Env) (Screen, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}
	switch {
	case key.Matches(keyMsg, s.keys.Up):
		s.cursor.up()
	case key.Matches(keyMsg, s.keys.Down):
		s.cursor.down()
	case key.Matches(keyMsg, s.keys.Enter):
		if i := s.cursor.Cursor(); i >= 0 && i < len(s.groups) {
			return s, s.run(s.entry, s.groups[i], s.deps)
		}
	}
	return s, nil
}

// View implements Screen.
func (s *FolderPickScreen) View(env Env) string {
	var b strings.Builder
	b.WriteString(env.Styles.Subtitle.Render(clipTail(s.verb+" which folder?", env.Width)))
	b.WriteString("\n")
	b.WriteString(env.Styles.Faint.Render(truncate(s.entry.Root, env.Width-1)))
	b.WriteString("\n\n")

	// Two lines per folder: the folder itself, then what is in it — which
	// is the whole basis for choosing between them.
	const perRow = 2
	height := (env.Height - 5) / perRow
	if height < 2 {
		height = 2
	}
	writeWindow(&b, env, len(s.groups), s.cursor.Cursor(), height, "", func(i int) {
		grp := s.groups[i]
		name := grp.Dir + "/"
		if grp.Dir == "" {
			name = "(game root)/"
		}
		marker := "  "
		if i == s.cursor.Cursor() {
			marker = "▸ "
		}
		line := clipTail(marker+name, env.Width)
		if i == s.cursor.Cursor() {
			line = env.Styles.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render(clipTail("    "+folderSummary(grp), env.Width)))
		b.WriteString("\n")
	})

	b.WriteString("\n")
	b.WriteString(env.Styles.Accent.Render("enter picks"))
	b.WriteString(env.Styles.Faint.Render("  ·  ↑↓ move · esc back"))
	b.WriteString("\n")
	return b.String()
}

// folderSummary is one line saying what is in a folder: enough to choose
// between two of them without opening either.
func folderSummary(grp FolderGroup) string {
	what := fmt.Sprintf("%d executable(s)", grp.playableCount())
	switch {
	case grp.Installed != nil:
		in := grp.Installed
		return fmt.Sprintf("ReShade %s (%s) · %s", in.ReShade.Version, in.ReShade.Flavor, what)
	case grp.Unmanaged != nil:
		return fmt.Sprintf("ReShade found, untracked (%s) · %s", grp.Unmanaged.DLLName, what)
	default:
		return "no ReShade · " + what
	}
}

// groupsWithInstall, groupsWithUnmanaged and installableGroups are the
// eligibility rules behind each action's folder list: an action must only
// ever offer folders it can actually run on.
func groupsWithInstall(e GameEntry) []FolderGroup {
	return filterGroups(e, func(g FolderGroup) bool { return g.Installed != nil })
}

func groupsWithUnmanaged(e GameEntry) []FolderGroup {
	return filterGroups(e, func(g FolderGroup) bool { return g.Unmanaged != nil })
}

// installableGroups are folders yarm could install into: any folder with
// an executable. A folder that already has an install is included — that
// is an edit, which the wizard handles by opening on its summary — and so
// is one with an unmanaged install, which startInstallForGroup redirects
// to adopting.
func installableGroups(e GameEntry) []FolderGroup {
	return filterGroups(e, func(g FolderGroup) bool { return g.playableCount() > 0 })
}

func filterGroups(e GameEntry, keep func(FolderGroup) bool) []FolderGroup {
	var out []FolderGroup
	for _, g := range e.Groups {
		if keep(g) {
			out = append(out, g)
		}
	}
	return out
}

// verbs for the picker's heading.
const (
	verbInstall   = "Install ReShade into"
	verbEdit      = "Edit the install in"
	verbUninstall = "Uninstall ReShade from"
	verbAdopt     = "Adopt the ReShade install in"
)

// installOrEdit is what enter does on a game: edit the install it has,
// adopt the one found on disk, or make one. All three go through
// startInstallForGroup, which redirects an unmanaged folder to adopting —
// so only the picker's heading changes between them.
func installOrEdit(e GameEntry) (verb string, groups []FolderGroup) {
	if installed := groupsWithInstall(e); len(installed) > 0 {
		return verbEdit, installed
	}
	if unmanaged := groupsWithUnmanaged(e); len(unmanaged) > 0 {
		return verbAdopt, unmanaged
	}
	return verbInstall, installableGroups(e)
}
