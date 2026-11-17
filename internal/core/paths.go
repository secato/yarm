package core

import (
	"fmt"
	"strings"
)

// Most games are one folder with one ReShade install. The exception is a
// game whose executables live in several folders — ReShade attaches to a
// directory, so an install, edit or uninstall is not automatically about
// just one of them. Rather than asking which folder on a screen of its
// own before the real work even starts, every folder a game has is always
// on offer, as the first tab of its page: a card naming each path, what is
// already there, and its executables — checked or not, depending on what
// the game already has installed.
//
// A folder can itself be ambiguous: two executables of different
// architectures sharing one directory (a 32-bit launcher stub beside the
// real 64-bit game, which Battle.net titles ship routinely) mean two
// different answers for which ReShade build fits. FolderGroup.PrimaryExe
// guesses the more likely one so the common case needs no extra step, but
// the guess is shown — marked among the folder's listed executables — and
// tab (pathsList.cycleExe) switches it, rather than leaving a silent guess
// as the only way that decision ever gets made.

// FolderPicker is the folder-choice state behind a game's Folders tab: which
// of its folders are checked, which executable stands for each, and what
// was checked when the screen opened.
type FolderPicker struct {
	groups   []FolderGroup
	selected map[string]bool // by Dir
	// initial is a snapshot of selected as the picker opened — untouched by
	// Toggle — so Added and Removed can tell "checked from the start" apart
	// from "checked, then unchecked again", which nets out to no change.
	initial map[string]bool // by Dir
	// chosen is which executable's path stands for each folder, by Dir —
	// PrimaryExe's guess to start, CycleExe's answer from then on for
	// whichever folder NeedsExeChoice. Read through ExeFor rather than
	// directly: a folder that already has an install overrides it with
	// InstalledExe regardless of what is recorded here.
	chosen map[string]string
	// multi allows more than one folder checked at once. Uninstalling and
	// editing only ever act on what is already installed, several folders
	// at a time if several qualify; a fresh install defaults to the one
	// folder it was opened on, but nothing stops it from reaching into a
	// second folder in the same pass — the reason the batch executor
	// exists at all is a 32- and a 64-bit tree set up together.
	multi bool
}

// NewFolderPicker returns a picker over groups with preselected checked.
func NewFolderPicker(groups []FolderGroup, preselected []string, multi bool) FolderPicker {
	sel := make(map[string]bool, len(preselected))
	for _, dir := range preselected {
		sel[dir] = true
	}
	initial := make(map[string]bool, len(sel))
	for dir := range sel {
		initial[dir] = true
	}
	chosen := make(map[string]string, len(groups))
	for _, g := range groups {
		chosen[g.Dir] = g.PrimaryExe().Path
	}
	return FolderPicker{groups: groups, selected: sel, initial: initial, chosen: chosen, multi: multi}
}

// Groups returns every folder the picker offers.
func (p FolderPicker) Groups() []FolderGroup { return p.groups }

// Multi reports whether several folders can be checked at once.
func (p FolderPicker) Multi() bool { return p.multi }

// Checked reports whether the folder dir is checked.
func (p FolderPicker) Checked(dir string) bool { return p.selected[dir] }

// Toggle checks or unchecks the folder dir. In single-select mode it is a
// radio instead: checking one clears every other. At least one folder
// always stays checked — an edit or uninstall with nothing to apply to is
// not a smaller version of the action, it is not the action at all.
func (p *FolderPicker) Toggle(dir string) {
	if !p.hasGroup(dir) {
		return
	}
	if !p.multi {
		clear(p.selected)
		p.selected[dir] = true
		return
	}
	if p.selected[dir] && len(p.SelectedGroups()) <= 1 {
		return
	}
	p.selected[dir] = !p.selected[dir]
}

func (p FolderPicker) hasGroup(dir string) bool {
	for _, g := range p.groups {
		if g.Dir == dir {
			return true
		}
	}
	return false
}

// ExeFor is the executable g's install plan should use: whichever one it
// is already installed against, when it has an install, otherwise the
// chosen pick recorded for its directory (PrimaryExe's guess until
// CycleExe changes it), falling back to PrimaryExe itself if the chosen
// path no longer matches anything (a rescan changed the folder's exe list
// out from under a stale pick).
func (p FolderPicker) ExeFor(g FolderGroup) Executable {
	if installed, ok := g.InstalledExe(); ok {
		return installed
	}
	if path, ok := p.chosen[g.Dir]; ok {
		for _, e := range g.Exes {
			if e.Path == path {
				return e
			}
		}
	}
	return g.PrimaryExe()
}

// CycleExe advances the folder dir to its next non-skipped executable — the
// only choice worth exposing once a folder mixes architectures, since
// ReShade still attaches to the folder as a whole but the build it needs
// depends on whichever executable actually renders, and PrimaryExe's guess
// is not always the right one. A no-op outside NeedsExeChoice: an
// installed folder's exe is already fixed, and a folder with only one
// candidate has nothing to cycle to.
func (p *FolderPicker) CycleExe(dir string) {
	var g FolderGroup
	found := false
	for _, cand := range p.groups {
		if cand.Dir == dir {
			g, found = cand, true
			break
		}
	}
	if !found || !g.NeedsExeChoice() {
		return
	}
	var candidates []string
	for _, e := range g.Exes {
		if !e.Skipped {
			candidates = append(candidates, e.Path)
		}
	}
	idx := 0
	for i, path := range candidates {
		if path == p.chosen[g.Dir] {
			idx = i
			break
		}
	}
	p.chosen[g.Dir] = candidates[(idx+1)%len(candidates)]
}

// SelectedGroups returns every checked folder, in the order groups lists
// them.
func (p FolderPicker) SelectedGroups() []FolderGroup {
	var out []FolderGroup
	for _, g := range p.groups {
		if p.selected[g.Dir] {
			out = append(out, g)
		}
	}
	return out
}

// Added is every folder checked now that was not checked when the picker
// opened — a new folder this edit is extending the install into.
func (p FolderPicker) Added() []FolderGroup {
	var out []FolderGroup
	for _, g := range p.groups {
		if p.selected[g.Dir] && !p.initial[g.Dir] {
			out = append(out, g)
		}
	}
	return out
}

// Removed is every folder that already has a recorded install and was
// checked when the picker opened, but is not checked now. Unchecking a
// folder is how an edit moves an install out of it, so applying uninstalls
// it rather than silently leaving it out of the batch untouched.
func (p FolderPicker) Removed() []FolderGroup {
	var out []FolderGroup
	for _, g := range p.groups {
		if g.Installed != nil && p.initial[g.Dir] && !p.selected[g.Dir] {
			out = append(out, g)
		}
	}
	return out
}

// FolderSummary is the one line under a folder's name: what ReShade is
// already doing there. What is *in* the folder is listed under it rather
// than counted here.
func FolderSummary(grp FolderGroup) string {
	switch {
	case grp.Installed != nil:
		in := grp.Installed
		return fmt.Sprintf("ReShade %s (%s)", in.ReShade.Version, in.ReShade.Flavor)
	case grp.Unmanaged != nil:
		return fmt.Sprintf("ReShade found, untracked (%s)", grp.Unmanaged.DLLName)
	default:
		return "no ReShade"
	}
}

// pickerExe is one executable as the paths pane shows it: its name, the
// architecture and graphics API that decide which ReShade build fits, and
// whether it is the folder's chosen exe when more than one is on offer.
type pickerExe struct {
	name, detail string
	chosen       bool
}

// maxPickerExesPerFolder keeps one folder full of executables from
// pushing every other folder off the screen — the point of the list is to
// compare folders, so each one has to stay visible.
const maxPickerExesPerFolder = 3

// pickerExes lists a folder's offered executables, with the folder's own
// path trimmed off the front (it is already the heading), and says how
// many did not fit. Skipped ones — uninstallers, redistributables — are
// left out: ReShade would never attach to them. chosenPath marks which one
// is currently picked; pass "" when the caller only needs the count.
func pickerExes(grp FolderGroup, chosenPath string) (shown []pickerExe, hidden int) {
	stripPrefix := ""
	if grp.Dir != "" {
		stripPrefix = grp.Dir + "/"
	}
	for _, ex := range grp.Exes {
		if ex.Skipped {
			continue
		}
		if len(shown) >= maxPickerExesPerFolder {
			hidden++
			continue
		}
		shown = append(shown, pickerExe{
			name:   strings.TrimPrefix(ex.Path, stripPrefix),
			detail: fmt.Sprintf("%s · %s", ex.Arch, APILabel(ex.API)),
			chosen: chosenPath != "" && ex.Path == chosenPath,
		})
	}
	return shown, hidden
}

// groupsWithInstall and installableGroups are the
// eligibility rules behind each action's folder list: an action must only
// ever offer folders it can actually run on.
func groupsWithInstall(e GameEntry) []FolderGroup {
	return filterGroups(e, func(g FolderGroup) bool { return g.Installed != nil })
}

// installableGroups are folders yarm could install into: any folder with
// an executable. A folder that already has an install is included — that
// is an edit — and so is one with an unmanaged install, which the window
// offers to adopt instead.
func installableGroups(e GameEntry) []FolderGroup {
	return filterGroups(e, func(g FolderGroup) bool { return g.PlayableCount() > 0 })
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

// ExeLine is one executable of a folder, as the Folders tab lists it.
type ExeLine struct {
	Name, Detail string
	// Chosen marks the executable standing for the folder when several are
	// on offer.
	Chosen bool
}

// FolderExes lists a folder's offered executables — playable ones only —
// with the folder's own path trimmed off the front, and says how many did
// not fit. chosenPath marks the current pick.
func FolderExes(grp FolderGroup, chosenPath string) (shown []ExeLine, hidden int) {
	exes, hidden := pickerExes(grp, chosenPath)
	for _, e := range exes {
		shown = append(shown, ExeLine{Name: e.name, Detail: e.detail, Chosen: e.chosen})
	}
	return shown, hidden
}
