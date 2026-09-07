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

// Actions a game's folder can be the target of — install, uninstall,
// adopt — and the plumbing that carries their results back. They live
// apart from any one screen because the games list is now the only screen
// that starts them, and the folder picker is the only thing that stands
// between a key press and a folder when a game has more than one.

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
