package install

import (
	"path/filepath"
	"slices"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/state"
)

// Needed is the set of artifact groups a request actually has to resolve.
// An edit usually changes one thing; everything the recorded install
// already put on disk, byte-for-byte as recorded, needs no source at all
// — the planner classifies it Skip, and Skip never touches a source.
// Skipping those groups keeps an edit from re-downloading the world (and
// lets it run offline), at the price of not picking up upstream changes
// to content that is already installed: an edit changes what you asked
// it to, nothing else.
type Needed struct {
	// ReShade and D3DCompiler are single artifacts; the maps are keyed by
	// catalog id. Custom content has no entry here: resolving it is a
	// directory scan, not a download.
	ReShade     bool
	D3DCompiler bool
	Packages    map[string]bool
	Addons      map[string]bool // catalog add-ons and RenoDX utility mods
	RenoDX      bool
}

// NeededArtifacts works out which artifact groups resolving req can skip,
// given the install already recorded for the target folder. prev nil (a
// fresh install) needs everything. Deciding needs the installed files'
// hashes, so this stats and hashes the game folder — cheap next to a
// download, and the caller decides how often that cost is worth paying.
func NeededArtifacts(req Request, prev *state.Install) Needed {
	n := Needed{Packages: map[string]bool{}, Addons: map[string]bool{}}

	if prev == nil {
		n.ReShade = true
		n.D3DCompiler = artifacts.NeedsD3DCompiler(req.TargetOS)
		for _, id := range req.Packages {
			n.Packages[id] = true
		}
		for _, id := range addonsFor(req) {
			n.Addons[id] = true
		}
		n.RenoDX = req.RenoDX != ""
		return n
	}

	// The build itself: same version, flavor and proxy name means the
	// installed DLL is the one this request would write.
	n.ReShade = req.Version != prev.ReShade.Version ||
		string(req.Flavor) != prev.ReShade.Flavor ||
		req.DLLName != prev.ReShade.DLL ||
		groupModified(req.Game.Root, prev, state.OriginReShade)

	if artifacts.NeedsD3DCompiler(req.TargetOS) {
		n.D3DCompiler = groupModified(req.Game.Root, prev, state.OriginD3DCompiler)
	}

	for _, id := range req.Packages {
		n.Packages[id] = !slices.Contains(prev.Packages, id) ||
			groupModified(req.Game.Root, prev, state.PackageOrigin(id))
	}
	for _, id := range addonsFor(req) {
		n.Addons[id] = !slices.Contains(prev.Addons, id) ||
			groupModified(req.Game.Root, prev, addonOrigin(prev, id))
	}

	// A RenoDX mod is needed when a different one was chosen, or when
	// this one's files no longer match what was recorded. An empty
	// choice needs nothing: the recorded mod, if any, is removed by the
	// plan's own upgrade logic.
	switch {
	case req.RenoDX == "":
		n.RenoDX = false
	case !prevHasFiles(prev, state.RenoDXOrigin(req.RenoDX)):
		n.RenoDX = true
	default:
		n.RenoDX = groupModified(req.Game.Root, prev, state.RenoDXOrigin(req.RenoDX))
	}
	return n
}

// addonsFor is the add-on ids a request would install: the selected ones
// when the build can load add-ons, nothing otherwise. Mirrors
// addonsForDownload at the app layer, which cannot be imported here.
func addonsFor(req Request) []string {
	if !req.Flavor.Addon() {
		return nil
	}
	return req.Addons
}

// addonOrigin guesses whether id names a RenoDX utility mod (cached under
// the renodx bucket) or a catalog add-on: whichever bucket the recorded
// install actually used. A fresh selection that matches neither records
// as a catalog add-on, the common case.
func addonOrigin(prev *state.Install, id string) state.Origin {
	for _, f := range prev.Files {
		if string(f.Origin) == "renodx:"+id {
			return f.Origin
		}
	}
	return state.AddonOrigin(id)
}

// prevHasFiles reports whether the recorded install holds at least one
// file of the given origin. A selection without files (a partial or
// hand-edited manifest) is treated as never installed: conservative, so
// it re-downloads rather than trusting an entry that cannot be verified.
func prevHasFiles(prev *state.Install, origin state.Origin) bool {
	for _, f := range prev.Files {
		if f.Origin == origin {
			return true
		}
	}
	return false
}

// groupModified reports whether any file of the given origin no longer
// matches what was recorded — the case where resolving is pointless
// because the bytes will be rewritten from a fresh source anyway.
func groupModified(root string, prev *state.Install, origin state.Origin) bool {
	for _, f := range prev.Files {
		if f.Origin != origin {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(f.Path))
		sum, err := hashFile(abs)
		if err != nil || sum != f.SHA256 {
			return true
		}
	}
	return false
}
