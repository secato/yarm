package app

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

// Actions a game's folders can be the target of — install, edit, uninstall,
// adopt — and the plumbing that carries their results back. They live
// apart from any one screen because the games list is now the only screen
// that starts them: which folder (or folders) a key press applies to is
// answered inside the wizard's own Paths step, or inline in the
// confirmation these open directly, never by a screen in between.

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

// editInstallBinding edits a folder's existing install — a distinct key
// from Install (which only ever means "there is nothing here yet"), so the
// shortcut always matches what it does rather than overloading one key
// with two meanings depending on state.
var editInstallBinding = key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit install"))

// adoptBinding is the same key as Install, with the help text that matches
// what it will actually do: a folder whose ReShade yarm did not put there
// cannot be installed into, so install redirects to adopting it. It is not
// a KeyMap entry because adopting has no key of its own — `a` already
// means "add folder" on the games list.
var adoptBinding = key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "adopt ReShade"))

// startInstallOrEdit opens the wizard on every wizard-eligible folder among
// targets — one Paths pane, checked by default on whichever of them
// already has an install, or on all of them when none does. A folder yarm
// has not adopted an unmanaged install in yet is not a fresh-install
// candidate: attempting one would collide with the files already there, so
// when every target turns out to be one of those, this hands off to the
// adopt confirmation instead of opening a wizard with nothing to show.
// Does nothing with no eligible folder at all.
func startInstallOrEdit(entry GameEntry, targets []FolderGroup, deps Deps) tea.Cmd {
	var wizardable, unmanaged []FolderGroup
	for _, g := range targets {
		if g.Unmanaged != nil {
			unmanaged = append(unmanaged, g)
		} else {
			wizardable = append(wizardable, g)
		}
	}
	switch {
	case len(wizardable) == 0:
		return startAdopt(entry, unmanaged, deps)
	case len(unmanaged) == 0:
		return openWizard(entry, wizardable, deps)
	default:
		// A folder yarm has not adopted an unmanaged install in yet is not
		// a fresh-install candidate — it gets its own confirmation rather
		// than silently sitting out of the wizard opened for the rest.
		return tea.Batch(openWizard(entry, wizardable, deps), startAdopt(entry, unmanaged, deps))
	}
}

// openWizard opens the wizard on wizardable, whose reference folder —
// whichever step-by-step answer the wizard's other steps start from — is
// the first of them with a recorded install, or simply the first when none
// has one.
func openWizard(entry GameEntry, wizardable []FolderGroup, deps Deps) tea.Cmd {
	reference := wizardable[0]
	for _, g := range wizardable {
		if g.Installed != nil {
			reference = g
			break
		}
	}
	exe := reference.primaryExe()
	if installed, ok := reference.installedExe(); ok {
		exe = installed
	}
	return PushScreen(NewWizardScreen(entry, exe, wizardable, deps))
}

// startUninstall confirms, then removes, every install covering targets —
// each resolved to whichever executable it is actually recorded against,
// which may not be the one a fresh install would default to. One
// confirmation, one batch run: an uninstall has no options to weigh
// per-folder the way an edit does, so there is nothing a folder-by-folder
// pass would let the user decide that listing every path up front does
// not already cover.
func startUninstall(entry GameEntry, targets []FolderGroup, deps Deps) tea.Cmd {
	if deps.Uninstaller == nil {
		return nil
	}
	var ops []folderOp
	var paths []string
	for _, g := range targets {
		target, ok := g.installedExe()
		if !ok {
			continue
		}
		paths = append(paths, target.Path)
		ops = append(ops, folderOp{
			Dir:       g.Dir,
			Uninstall: &install.UninstallRequest{GameID: entry.ID, Exe: target.Path},
		})
	}
	if len(ops) == 0 {
		return nil
	}

	run := PushScreen(NewProgressScreen(ops, deps.Installer, deps.Uninstaller))
	return Confirm(
		"Uninstall ReShade from "+strings.Join(paths, ", ")+"?",
		"This removes only the files yarm created; anything you edited afterward is kept.",
		run,
	)
}

// startAdopt confirms, then records, the unmanaged install found in every
// target — each tied to its own folder's primary executable, since ReShade
// intercepts by directory rather than by which executable a user might
// expect. One confirmation covers all of them: adopting changes nothing on
// disk, so there is no per-folder decision to make first.
func startAdopt(entry GameEntry, targets []FolderGroup, deps Deps) tea.Cmd {
	if deps.Adopter == nil {
		return nil
	}

	var paths []string
	var cmds []tea.Cmd
	for _, g := range targets {
		if g.Unmanaged == nil {
			continue
		}
		candidate := *g.Unmanaged
		target := g.primaryExe()
		exePath := target.Path
		paths = append(paths, exePath)

		gm, executable := entry.Game, target.Executable
		cmds = append(cmds, Async(context.Background(),
			func(ctx context.Context) (state.Install, error) {
				return deps.Adopter.Adopt(gm, executable, candidate)
			},
			func(in state.Install) tea.Msg {
				return adoptDoneMsg{exePath: exePath, install: in}
			},
		))
	}
	if len(cmds) == 0 {
		return nil
	}

	detail := "This records the ReShade already found in each folder — the proxy DLL, ReShade.ini, " +
		"and any shaders, textures or add-ons already there — as yarm's own. Nothing on disk changes " +
		"now; yarm can update or uninstall each from then on, the same as one it created itself."
	return Confirm(
		"Adopt the existing ReShade install in "+strings.Join(paths, ", ")+"?",
		detail,
		tea.Batch(cmds...),
	)
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
