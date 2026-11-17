package core

import (
	"fmt"
	"strings"

	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

// Actions a game's folders can be the target of — install, edit, uninstall,
// adopt. The window decides when to offer them; this decides what each one
// applies to.

// SplitTargets divides a game's folders into those an install or edit can
// run on and those holding a ReShade yarm did not put there. The second kind
// is not a fresh-install candidate: attempting one would collide with the
// files already there, so it is offered for adopting instead.
func SplitTargets(entry GameEntry) (installable, unmanaged []FolderGroup) {
	for _, g := range installableGroups(entry) {
		if g.Unmanaged != nil {
			unmanaged = append(unmanaged, g)
		} else {
			installable = append(installable, g)
		}
	}
	return installable, unmanaged
}

// Reference picks the folder every choice of an edit starts from: the first
// with a recorded install, or simply the first when none has one, and the
// executable that stands for it. ok is false with no folder at all.
func Reference(groups []FolderGroup) (Executable, bool) {
	if len(groups) == 0 {
		return Executable{}, false
	}
	reference := groups[0]
	for _, g := range groups {
		if g.Installed != nil {
			reference = g
			break
		}
	}
	if installed, ok := reference.InstalledExe(); ok {
		return installed, true
	}
	return reference.PrimaryExe(), true
}

// UninstallOps builds the uninstall of every install covering targets, each
// resolved to whichever executable it is recorded against.
func UninstallOps(entry GameEntry, targets []FolderGroup) []Op {
	var ops []Op
	for _, g := range targets {
		target, ok := g.InstalledExe()
		if !ok {
			continue
		}
		ops = append(ops, Op{
			Dir:       g.Dir,
			Uninstall: &install.UninstallRequest{GameID: entry.ID, Exe: target.Path},
		})
	}
	return ops
}

// InstalledGroups are the folders of entry that have an install of ours.
func InstalledGroups(entry GameEntry) []FolderGroup { return groupsWithInstall(entry) }

// maxUninstallListFiles caps the recorded files named in the uninstall
// confirmation: enough to recognize the install, not the whole manifest.
const maxUninstallListFiles = 6

// UninstallDetail says what confirming an uninstall will remove: the
// recorded file count and names, so "removes only the files yarm created" is
// checkable rather than taken on trust. A registry that cannot be read falls
// back to the generic promise.
func UninstallDetail(stateDir, gameID string, targets []FolderGroup) string {
	const generic = "This removes only the files yarm created; anything you edited afterward is kept."
	if stateDir == "" {
		return generic
	}
	reg, err := state.Load(stateDir)
	if err != nil {
		return generic
	}
	var names []string
	for _, g := range targets {
		target, ok := g.InstalledExe()
		if !ok {
			continue
		}
		in, ok := reg.FindInstall(gameID, target.Path)
		if !ok {
			continue
		}
		for _, f := range in.Files {
			names = append(names, f.Path)
		}
	}
	if len(names) == 0 {
		return generic
	}
	shown := names
	rest := ""
	if len(names) > maxUninstallListFiles {
		shown = names[:maxUninstallListFiles]
		rest = fmt.Sprintf(", and %d more", len(names)-maxUninstallListFiles)
	}
	return fmt.Sprintf("Removes %d recorded file(s) (%s%s); anything you edited afterward is kept.",
		len(names), strings.Join(shown, ", "), rest)
}

// AdoptAll records the unmanaged ReShade found in every target as yarm's
// own, each tied to its folder's primary executable. It stops at the first
// failure. Nothing on disk changes.
func AdoptAll(deps Deps, entry GameEntry, targets []FolderGroup) (adopted int, err error) {
	if deps.Adopter == nil {
		return 0, fmt.Errorf("adopting is not available")
	}
	for _, g := range targets {
		if g.Unmanaged == nil {
			continue
		}
		if _, err := deps.Adopter.Adopt(entry.Game, g.PrimaryExe().Executable, *g.Unmanaged); err != nil {
			return adopted, err
		}
		adopted++
	}
	return adopted, nil
}

// AdoptDetail is the sentence explaining what adopting does.
const AdoptDetail = "This records the ReShade already found in each folder — the proxy DLL, ReShade.ini, " +
	"and any shaders, textures or add-ons already there — as yarm's own. Nothing on disk changes " +
	"now; yarm can update or uninstall each from then on, the same as one it created itself."
