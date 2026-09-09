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

// pathsList is the folder-choice state behind that pane: a cursor over a
// game's folders, which of them are checked, and which have their
// executable list expanded.
type pathsList struct {
	groups   []FolderGroup
	selected map[string]bool // by Dir
	expanded map[string]bool // by Dir
	cursor   cursorList
	// multi allows more than one folder checked at once. Uninstalling and
	// editing only ever act on what is already installed, several folders
	// at a time if several qualify; a fresh install defaults to the one
	// folder it was opened on, but nothing stops it from reaching into a
	// second folder in the same pass — the reason the batch executor
	// exists at all is a 32- and a 64-bit tree set up together.
	multi bool
}

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
	return pathsList{
		groups:   groups,
		selected: sel,
		expanded: map[string]bool{},
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

// writePathsList renders the full pane: one block per folder, a checkbox
// (multi-select only — single-select shows which one is picked without
// implying others could join it), the path, its ReShade status, and its
// executables, collapsed to a count unless expanded.
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

	shown, hidden := pickerExes(grp)
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
			b.WriteString(clipTail("        "+ex.name, env.Width))
			b.WriteString(env.Styles.Faint.Render(clipTail("  "+ex.detail, env.Width-lipgloss.Width(ex.name)-8)))
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

// pickerExe is one executable as the paths pane shows it: its name, and
// the architecture and graphics API that decide which ReShade build fits.
type pickerExe struct{ name, detail string }

// maxPickerExesPerFolder keeps one folder full of executables from
// pushing every other folder off the screen — the point of the list is to
// compare folders, so each one has to stay visible.
const maxPickerExesPerFolder = 3

// pickerExes lists a folder's offered executables, with the folder's own
// path trimmed off the front (it is already the heading), and says how
// many did not fit. Skipped ones — uninstallers, redistributables — are
// left out: ReShade would never attach to them.
func pickerExes(grp FolderGroup) (shown []pickerExe, hidden int) {
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
		shown, hidden := pickerExes(g)
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
