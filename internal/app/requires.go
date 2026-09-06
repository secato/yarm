package app

import "strings"

// Some catalog entries only work if something else is installed too. The
// AutoHDR add-on needs a tone-mapping shader to actually re-tonemap;
// Shades' TFAA needs iMMERSE's LAUNCHPAD; BFBFX needs the Zenteon
// framework. Upstream records none of this: `EffectPackages.ini` and
// `Addons.ini` have no dependency key at all — every one of these is a
// sentence inside PackageDescription, phrased differently each time
// ("requires ...", "Depends on ...!", "requires X or Y"). Five entries in
// the whole catalog say it, so this is a hand-kept table rather than a
// prose parser: parsing English to get five edges wrong in new ways every
// time upstream edits a sentence is a worse trade than editing this file.

// requirement is one thing an entry needs before it will work.
type requirement struct {
	// AnyOf are the catalog package ids that satisfy this, in preference
	// order — the first is the one yarm selects on the user's behalf.
	// Empty means nothing in the catalog satisfies it, so it can only ever
	// be reported (see Frame Capture, which wants a loose .fx file from a
	// repository upstream does not list).
	AnyOf []string
	// Label names the requirement the way a person would say it, for the
	// note under the row.
	Label string
}

// available returns the alternative to select, which is the first one the
// loaded catalog actually offers.
func (r requirement) available(known map[string]bool) (string, bool) {
	for _, id := range r.AnyOf {
		if known[id] {
			return id, true
		}
	}
	return "", false
}

// metBy reports whether any of the alternatives is selected.
func (r requirement) metBy(selected map[string]bool) bool {
	for _, id := range r.AnyOf {
		if selected[id] {
			return true
		}
	}
	return false
}

// requires maps a catalog id — package or add-on, the two id spaces do not
// collide — to what it needs. Values are always effect-package ids: every
// dependency in the catalog runs add-on → shaders or shaders → shaders.
var requires = map[string][]requirement{
	// "requires lilium__inverse_tone_mapping.fx from ReShade_HDR_shaders
	// or AdvancedAutoHDR.fx to fix gamma and actually re-tonemap to HDR".
	// Lilium's is listed first because it is the one the add-on's own
	// author maintains alongside it.
	"autohdr-by-endlesslyflowering-original-by-majorpainthecactus": {{
		AnyOf: []string{"reshade-hdr-shaders-by-lilium", "advancedautohdr-by-pumbo"},
		Label: "a tone-mapping shader (ReShade_HDR_shaders or AdvancedAutoHDR)",
	}},
	// "Currently only the TFAA.fx shader. Requires depth buffer and
	// iMMERSE LAUNCHPAD." The depth buffer is a runtime condition, not
	// something to install.
	"shades-by-jakobpcoder": {{
		AnyOf: []string{"immerse-by-marty-mcfly"},
		Label: "iMMERSE (LAUNCHPAD)",
	}},
	// "requires CShade effects"
	"ann-reshade-by-anastasia-bouwsma": {{
		AnyOf: []string{"cshade-by-papadanku"},
		Label: "CShade",
	}},
	// "Depends on Zenteon Framework!"
	"bfbfx-by-yaboi-bfb": {{
		AnyOf: []string{"zenteonfx-shaders-by-zenteon"},
		Label: "ZenteonFX",
	}},
	// "requires https://github.com/murchalloo/murchFX/.../DepthToAddon.fx"
	// — a single file from a repository the catalog does not carry, so
	// there is nothing to tick. Recorded anyway: an add-on that silently
	// does nothing is worse than one that says what it is waiting for.
	"frame-capture-by-murchalloo": {{
		Label: "DepthToAddon.fx from murchFX, installed by hand",
	}},
}

// requirementsFor returns what id needs, or nil.
func requirementsFor(id string) []requirement { return requires[id] }

// satisfy selects the first available alternative for every unmet
// requirement of id, reporting what it turned on. It only ever adds: a
// requirement the user has already met another way is left alone.
//
// known is the set of ids the loaded catalog actually offers. A
// requirement naming something absent — this table is hand-kept against a
// catalog that is fetched, so upstream can rename or drop an entry
// underneath it — is left unmet and reported rather than selected, since
// selecting an id nothing can download would only fail mid-install.
func satisfy(id string, selected, known map[string]bool) []string {
	var added []string
	for _, r := range requirementsFor(id) {
		if r.metBy(selected) {
			continue
		}
		if pick, ok := r.available(known); ok {
			selected[pick] = true
			added = append(added, pick)
		}
	}
	return added
}

// unmetRequirements lists, for everything currently selected, the
// requirements still not met — the ones the user unchecked by hand, and
// the ones no catalog entry can meet. Returned as display lines, in the
// order the ids were given, so the review page can print them verbatim.
func unmetRequirements(ids []string, names map[string]string, selected map[string]bool) []string {
	var out []string
	for _, id := range ids {
		for _, r := range requirementsFor(id) {
			if r.metBy(selected) {
				continue
			}
			name := names[id]
			if name == "" {
				name = id
			}
			out = append(out, name+" needs "+r.Label)
		}
	}
	return out
}

// requirementNote is the line shown under a row: what it needs, and
// whether that is currently met. Empty for the great majority of rows,
// which need nothing.
func requirementNote(id string, selected map[string]bool) (note string, unmet bool) {
	var parts []string
	for _, r := range requirementsFor(id) {
		if r.metBy(selected) {
			continue
		}
		parts = append(parts, r.Label)
		unmet = true
	}
	if !unmet {
		return "", false
	}
	return "needs " + strings.Join(parts, "; "), true
}
