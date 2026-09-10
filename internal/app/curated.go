package app

import "strings"

// Upstream's lists are catalogs, not recommendations: 43 effect packages
// and 24 add-ons, most of them niche (CRT filters, stereo-3D rigs, one
// flight-sim mod), arrive in whatever order crosire's ini happens to list
// them. Scrolling that to find SweetFX is the wrong first experience of
// installing ReShade, so the wizard shows a curated shortlist by default
// and keeps the rest one keypress away — nothing is removed, only demoted.
//
// The shortlists below are hand-kept. Add to them when a pack becomes
// something people actually ask for by name; do not try to derive them
// from the catalog, which carries no popularity signal at all.

// curatedPackages are the effect packages the wizard offers up front,
// keyed by catalog id.
var curatedPackages = map[string]bool{
	// Ships DisplayDepth, which half the catalog's effects assume is
	// present; upstream marks it Required and so do we.
	"standard-effects": true,
	// The originals nearly every preset still names: LumaSharpen,
	// Vibrance, Tonemap, SMAA, FXAA.
	"sweetfx-by-ceejay-dk": true,
	// Marty McFly's three generations, all still in active use: qUINT is
	// what older presets were written against, iMMERSE replaced it
	// (LAUNCHPAD, MXAO), METEOR is the extras.
	"immerse-by-marty-mcfly": true,
	"meteor-by-marty-mcfly":  true,
	"quint-by-marty-mcfly":   true,
	// Clarity.fx lives here, and it is the one effect from the classic
	// four that SweetFX never shipped.
	"astrayfx-by-blueskydefender": true,
	// The color-grading reference set.
	"color-effects-by-prod80": true,
	// Cinematic DOF and MultiLUT — the screenshot community's staples.
	"otisfx-by-otis-inf": true,
	// Also aimed at in-game photography (depth-masked effects).
	"cobrafx-by-sircobra": true,
	// Widely used utility effects, plus its own AO and TFAA.
	"insane-shaders-by-lord-of-lunacy": true,
	// MagicHDR / NeoBloom.
	"fxshaders-by-luluco250": true,
	// Recent, and asked for by name.
	"lumenitefx": true,
	// Screen-space AO, GI and reflections plus modern AA — XeGTAO, NeoSSAO,
	// DLAA-T, neural sharpening. What people install ReShade for now, where
	// most of the packs above are color grading. Careful with the id: the
	// catalog carries two packs named "reshade-shaders by <author>", and
	// this is Barbatos', not Daodan's (reshade-shaders-by-daodan).
	"reshade-shaders-by-barbatos": true,
	// TurboGI and ATA in its own right, and upstream calls it the "much
	// higher quality successor to QuarkFX and ZN_FX". It also has to be
	// here: its Zenteon_Framework.fx is the only thing in the catalog that
	// satisfies BFBFX's dependency (see requires.go), so the wizard could
	// tick it on the user's behalf while the pack itself stayed hidden
	// behind the show-all toggle.
	"zenteonfx-shaders-by-zenteon": true,
	// Kept because presets written for ReShade 3/4 reference effects that
	// only exist here (AmbientLight, MagicBloom).
	"legacy-effects": true,
}

// curatedAddons are the add-ons the wizard offers up front, keyed by
// catalog id.
var curatedAddons = map[string]bool{
	// Lets ReShade sit in front of a flip-model swap chain; the usual fix
	// when effects render but nothing appears.
	"swap-chain-override-by-crosire": true,
	// The two togglers, which is what most people install add-on support
	// for in the first place: hide the HUD, or apply effects to a subset
	// of the frame.
	"shadertoggler-by-otis-inf":                    true,
	"reshadeeffectshadertoggler-rest-by-4lex4nder": true,
	// HDR output, increasingly the reason to run add-on ReShade at all.
	"autohdr-by-endlesslyflowering-original-by-majorpainthecactus": true,
	// Freecam bridge, paired with OtisFX by the same author.
	"igcs-connector-by-otis-inf": true,
	// Window/display control (resolution, HDR, frame limiting).
	"display-commander-by-pmnoxx": true,
	// Feeds the post-processed frame to OBS.
	"obs-capture-by-crosire": true,
	// Not installable — upstream lists no download at all — but it is the
	// add-on people look for most, so it stays visible and greyed with its
	// repository URL rather than vanishing into the full list.
	"renodx-by-shortfuse": true,
	// RenoDX's own utility mods (see catalog.RenoMod.Utility): unlike a
	// per-game or Generic mod they do not replace shaders or tone
	// mapping, so more than one can run at once, and FPS Limiter
	// especially is exactly the kind of thing worth surfacing by name.
	"fpslimiter": true,
	"dlssfix":    true,
}

// curate narrows a step's rows to the curated shortlist. Rows are kept
// when the shortlist names them, when they are already selected (editing
// an install must never hide what it installed, however obscure), when
// they cannot be deselected, and when they are not from the catalog at all
// — custom content is the user's own, and its header goes with it.
//
// all short-circuits the whole thing, which is what the "show all" key
// does.
func curate(items []selectItem, shortlist map[string]bool, all bool, selected map[string]bool) []selectItem {
	if all {
		return items
	}
	out := make([]selectItem, 0, len(items))
	for _, it := range items {
		if it.Header || keepInShortlist(it.ID, shortlist, it.Required || selected[it.ID]) {
			out = append(out, it)
		}
	}
	return out
}

// keepInShortlist is the rule both the wizard and the resources browser
// filter by: the shortlist names it, it is the user's own content, or the
// row has a reason of its own to stay (sticky) — checked, required,
// already downloaded, in use by an install. Demoting a row the user has
// some relationship with would hide their own data, which is a different
// thing from hiding catalog noise.
func keepInShortlist(id string, shortlist map[string]bool, sticky bool) bool {
	return sticky || isCustomID(id) || shortlist[id]
}

// isCustomID reports whether an id came from the custom folder rather than the
// upstream catalog.
func isCustomID(id string) bool { return strings.HasPrefix(id, "custom:") }
