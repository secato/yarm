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
		switch {
		case it.Header, it.Required, selected[it.ID], shortlist[it.ID], isCustomID(it.ID):
			out = append(out, it)
		}
	}
	return out
}

// isCustomID reports whether an id came from cache/custom rather than the
// upstream catalog.
func isCustomID(id string) bool { return strings.HasPrefix(id, "custom:") }
