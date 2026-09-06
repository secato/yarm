package catalog

import (
	"fmt"
	"strings"
	"unicode"
)

// Slugify converts a catalog PackageName into a stable id:
// "SweetFX by CeeJay.dk" -> "sweetfx-by-ceejay-dk".
func Slugify(name string) string {
	var b strings.Builder
	lastDash := true // leading dashes are suppressed

	for _, r := range strings.ToLower(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}

	return strings.Trim(b.String(), "-")
}

// uniqueSlug returns slug, or slug with a numeric suffix when taken. The
// upstream catalog has two packages both named "reshade-shaders" (by
// different authors); their full slugs differ, but nothing guarantees that
// stays true, and duplicate ids would silently shadow a package.
func uniqueSlug(slug string, taken map[string]bool) string {
	if slug == "" {
		slug = "unnamed"
	}
	candidate := slug
	for n := 2; taken[candidate]; n++ {
		candidate = fmt.Sprintf("%s-%d", slug, n)
	}
	taken[candidate] = true
	return candidate
}

// aliases are short, hand-kept names for the packages users ask for by
// name, so config files and the CLI can say "sweetfx" instead of
// "sweetfx-by-ceejay-dk". Keys must stay stable: they appear in user
// config (defaults.packages).
var aliases = map[string]string{
	"standard":  "standard-effects",
	"sweetfx":   "sweetfx-by-ceejay-dk",
	"legacy":    "legacy-effects",
	"quint":     "quint-by-marty-mcfly",
	"immerse":   "immerse-by-marty-mcfly",
	"meteor":    "meteor-by-marty-mcfly",
	"otisfx":    "otisfx-by-otis-inf",
	"depth3d":   "depth3d-by-blueskydefender",
	"fxshaders": "fxshaders-by-luluco250",
	"prod80":    "color-effects-by-prod80",
	"astrayfx":  "astrayfx-by-blueskydefender",
	"cobrafx":   "cobrafx-by-sircobra",
	"corgifx":   "corgifx-by-originalnicodr",
	"cshade":    "cshade-by-papadanku",
	"insane":    "insane-shaders-by-lord-of-lunacy",
	"lumenite":  "lumenitefx",
}

// ResolveAlias maps a short alias to its full package id. Anything that is
// not an alias is returned unchanged, so callers can pass user input
// straight through.
func ResolveAlias(id string) string {
	if full, ok := aliases[id]; ok {
		return full
	}
	return id
}

// Aliases returns a copy of the alias map, for help text and tests.
func Aliases() map[string]string {
	out := make(map[string]string, len(aliases))
	for k, v := range aliases {
		out[k] = v
	}
	return out
}
