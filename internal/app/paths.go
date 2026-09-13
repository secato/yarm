package app

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Most games are one folder with one ReShade install. The exception is a
// game whose executables live in several folders — ReShade attaches to a
// directory, so an install, edit or uninstall is not automatically about
// just one of them. Rather than asking which folder on a screen of its
// own before the real work even starts, every folder a game has is always
// on offer, as the first thing the wizard shows: a pane naming each path,
// what is already there, and its executables — checked or not, depending
// on what brought the wizard up.
//
// A folder can itself be ambiguous: two executables of different
// architectures sharing one directory (a 32-bit launcher stub beside the
// real 64-bit game, which Battle.net titles ship routinely) mean two
// different answers for which ReShade build fits. FolderGroup.primaryExe
// guesses the more likely one so the common case needs no extra step, but
// the guess is shown — marked among the folder's listed executables — and
// tab (pathsList.cycleExe) switches it, rather than leaving a silent guess
// as the only way that decision ever gets made.

// pathsList is the folder-choice state behind that pane: a cursor over a
// game's folders, which of them are checked, and which have their
// executable list expanded.
type pathsList struct {
	groups   []FolderGroup
	selected map[string]bool // by Dir
	// initial is a snapshot of selected as the wizard opened it — untouched
	// by toggle — so addedFolders and removedInstalls can tell "checked
	// from the start" apart from "checked, then unchecked again", which
	// nets out to no change at all.
	initial  map[string]bool // by Dir
	expanded map[string]bool // by Dir
	// chosen is which executable's path stands for each folder, by Dir —
	// primaryExe's guess to start, cycleExe's answer from then on for
	// whichever folder needsExeChoice. Read through exeFor rather than
	// directly: a folder that already has an install overrides it with
	// installedExe regardless of what is recorded here.
	chosen map[string]string
	cursor cursorList
	// multi allows more than one folder checked at once. Uninstalling and
	// editing only ever act on what is already installed, several folders
	// at a time if several qualify; a fresh install defaults to the one
	// folder it was opened on, but nothing stops it from reaching into a
	// second folder in the same pass — the reason the batch executor
	// exists at all is a 32- and a 64-bit tree set up together.
	multi bool
}

// maxAutoExpandedGroups is how many folders a game can have before the
// paths pane falls back to a count instead of each one's full executable
// list by default. Executables are what tells two folders of the same
// game apart, so they earn their room up to a handful of folders; past
// that, showing every one in full would defeat the point of the pane,
// which is comparing folders at a glance.
const maxAutoExpandedGroups = 5

// newPathsList returns a pathsList over groups, with preselected checked
// and the cursor on it (or on the first group, if preselected is empty).
func newPathsList(groups []FolderGroup, preselected []string, multi bool) pathsList {
	sel := make(map[string]bool, len(preselected))
	for _, dir := range preselected {
		sel[dir] = true
	}
	start := 0
	for i, g := range groups {
		if sel[g.Dir] {
			start = i
			break
		}
	}
	expanded := map[string]bool{}
	if len(groups) <= maxAutoExpandedGroups {
		for _, g := range groups {
			expanded[g.Dir] = true
		}
	}
	initial := make(map[string]bool, len(sel))
	for dir := range sel {
		initial[dir] = true
	}
	chosen := make(map[string]string, len(groups))
	for _, g := range groups {
		chosen[g.Dir] = g.primaryExe().Path
	}
	return pathsList{
		groups:   groups,
		selected: sel,
		initial:  initial,
		expanded: expanded,
		chosen:   chosen,
		cursor:   newCursorList(len(groups), start),
		multi:    multi,
	}
}

func (p *pathsList) up()   { p.cursor.up() }
func (p *pathsList) down() { p.cursor.down() }

// current is the group under the cursor, or the zero value when there is
// nothing to point at.
func (p pathsList) current() (FolderGroup, bool) {
	i := p.cursor.Cursor()
	if i < 0 || i >= len(p.groups) {
		return FolderGroup{}, false
	}
	return p.groups[i], true
}

// toggle checks or unchecks the folder under the cursor. In single-select
// mode it is a radio instead: checking one clears every other. At least
// one folder always stays checked — an edit or uninstall with nothing to
// apply to is not a smaller version of the action, it is not the action
// at all.
func (p *pathsList) toggle() {
	g, ok := p.current()
	if !ok {
		return
	}
	if !p.multi {
		clear(p.selected)
		p.selected[g.Dir] = true
		return
	}
	if p.selected[g.Dir] && len(p.selectedGroups()) <= 1 {
		return
	}
	p.selected[g.Dir] = !p.selected[g.Dir]
}

// toggleExpand shows or hides the executable list under the cursor's
// folder — the detail that tells two folders of the same game apart in
// the first place, kept collapsed by default so a game with many folders
// still fits.
func (p *pathsList) toggleExpand() {
	g, ok := p.current()
	if !ok {
		return
	}
	p.expanded[g.Dir] = !p.expanded[g.Dir]
}

// exeFor is the executable g's install plan should use: whichever one it
// is already installed against, when it has an install, otherwise the
// chosen pick recorded for its directory (primaryExe's guess until
// cycleExe changes it), falling back to primaryExe itself if the chosen
// path no longer matches anything (a rescan changed the folder's exe list
// out from under a stale pick).
func (p pathsList) exeFor(g FolderGroup) Executable {
	if installed, ok := g.installedExe(); ok {
		return installed
	}
	if path, ok := p.chosen[g.Dir]; ok {
		for _, e := range g.Exes {
			if e.Path == path {
				return e
			}
		}
	}
	return g.primaryExe()
}

// cycleExe advances the cursor's folder to its next non-skipped
// executable — the only choice worth exposing once a folder mixes
// architectures, since ReShade still attaches to the folder as a whole
// but the build it needs depends on whichever executable actually
// renders, and primaryExe's guess is not always the right one. A no-op
// outside needsExeChoice: an installed folder's exe is already fixed, and
// a folder with only one candidate has nothing to cycle to.
func (p *pathsList) cycleExe() {
	g, ok := p.current()
	if !ok || !g.needsExeChoice() {
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
	// Cycling expresses "let me see the choice" as much as "change it" —
	// a collapsed folder switching its pick with nothing on screen to show
	// for it would look like the key did nothing.
	p.expanded[g.Dir] = true
}

// selectedGroups returns every checked folder, in the order groups lists
// them.
func (p pathsList) selectedGroups() []FolderGroup {
	var out []FolderGroup
	for _, g := range p.groups {
		if p.selected[g.Dir] {
			out = append(out, g)
		}
	}
	return out
}

// addedFolders is every folder checked now that was not checked when the
// wizard opened — a new folder this edit is extending the install into.
func (p pathsList) addedFolders() []FolderGroup {
	var out []FolderGroup
	for _, g := range p.groups {
		if p.selected[g.Dir] && !p.initial[g.Dir] {
			out = append(out, g)
		}
	}
	return out
}

// removedInstalls is every folder that already has a recorded install and
// was checked when the wizard opened, but is not checked now. Unchecking a
// folder is how an edit moves an install out of it — folderOp supports an
// Install and an Uninstall in the same batch for exactly this — so applying
// uninstalls it rather than silently leaving it out of the batch untouched.
func (p pathsList) removedInstalls() []FolderGroup {
	var out []FolderGroup
	for _, g := range p.groups {
		if g.Installed != nil && p.initial[g.Dir] && !p.selected[g.Dir] {
			out = append(out, g)
		}
	}
	return out
}

// writePathsList renders the full pane: one block per folder, a checkbox
// (multi-select only — single-select shows which one is picked without
// implying others could join it), the path, its ReShade status, and its
// executables — shown in full up to maxAutoExpandedGroups folders,
// collapsed to a count past that, either way overridable per folder by
// toggleExpand.
func writePathsList(b *strings.Builder, env Env, p pathsList, height int) {
	perRow := 2
	if _, ok := p.current(); ok {
		perRow += maxPickerExes(p.groups)
	}
	rows := height / perRow
	if rows < 1 {
		rows = 1
	}

	writeWindow(b, env, len(p.groups), p.cursor.Cursor(), rows, "", func(i int) {
		writePathRow(b, env, p, i)
	})
}

func writePathRow(b *strings.Builder, env Env, p pathsList, i int) {
	grp := p.groups[i]
	focused := i == p.cursor.Cursor()

	box := "  "
	if p.multi {
		box = "[ ]"
		if p.selected[grp.Dir] {
			box = "[x]"
		}
	} else if p.selected[grp.Dir] {
		box = "▪"
	}
	marker := "  "
	if focused {
		marker = "▸ "
	}
	line := clipTail(fmt.Sprintf("%s%s %s", marker, box, folderLabel(grp.Dir)), env.Width)
	if focused {
		line = env.Styles.Selected.Render(line)
	}
	b.WriteString(line)
	b.WriteString("\n")
	b.WriteString(env.Styles.Faint.Render(clipTail("      "+folderSummary(grp), env.Width)))
	b.WriteString("\n")

	ambiguous := grp.needsExeChoice()
	shown, hidden := pickerExes(grp, p.chosen[grp.Dir])
	switch {
	case !p.expanded[grp.Dir] && len(shown)+hidden > 0:
		n := len(shown) + hidden
		noun := "executable"
		if n != 1 {
			noun = "executables"
		}
		hint := ""
		if focused {
			hint = "  — → to list them"
		}
		b.WriteString(env.Styles.Faint.Render(clipTail(fmt.Sprintf("        %d %s%s", n, noun, hint), env.Width)))
		b.WriteString("\n")
	default:
		for _, ex := range shown {
			name := ex.name
			if ambiguous {
				marker := "○ "
				if ex.chosen {
					marker = "● "
				}
				name = marker + name
			}
			b.WriteString(clipTail("        "+name, env.Width))
			b.WriteString(env.Styles.Faint.Render(clipTail("  "+ex.detail, env.Width-lipgloss.Width(name)-8)))
			b.WriteString("\n")
		}
		if hidden > 0 {
			b.WriteString(env.Styles.Faint.Render(clipTail(fmt.Sprintf("        +%d more", hidden), env.Width)))
			b.WriteString("\n")
		}
	}
}

// pathsHubLines is the hub's compact summary of the paths pane: the
// checked folders, named the way every other pane names its own contents.
func pathsHubLines(p pathsList, width int) []string {
	var lines []string
	for _, g := range p.selectedGroups() {
		lines = append(lines, clipTail("  "+folderLabel(g.Dir), width))
	}
	if len(lines) == 0 {
		lines = []string{clipTail("  none selected", width)}
	}
	return lines
}

// folderSummary is the one line under a folder's name: what ReShade is
// already doing there. What is *in* the folder is listed under it rather
// than counted here.
func folderSummary(grp FolderGroup) string {
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
			detail: fmt.Sprintf("%s · %s", ex.Arch, apiLabel(ex.API)),
			chosen: chosenPath != "" && ex.Path == chosenPath,
		})
	}
	return shown, hidden
}

// maxPickerExes is how many executable lines the tallest folder in the
// list needs, so a window can size a row for the worst case rather than
// scrolling by a different amount depending on where the cursor is.
func maxPickerExes(groups []FolderGroup) int {
	most := 0
	for _, g := range groups {
		shown, hidden := pickerExes(g, "")
		n := len(shown)
		if hidden > 0 {
			n++
		}
		if n > most {
			most = n
		}
	}
	return most
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
// is one with an unmanaged install, which the wizard redirects to
// adopting.
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
