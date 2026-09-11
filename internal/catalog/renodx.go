package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/secato/yarm/internal/game"
)

// RenoDX is a ReShade add-on that replaces a game's shaders to upgrade its
// HDR handling. Unlike everything else in this package it is not described
// by crosire's catalog: Addons.ini lists RenoDX with no download URL at
// all, so it can only ever be shown there as "manual install only".
//
// RenoDX publishes its own machine-readable index instead, and installing
// it is a single file: per its own documentation, ReShade 6.8.0+ with
// add-on support, no shaders needed, copy one .addon64/.addon32 into the
// game folder. There is no core, runtime or framework — every mod is a
// self-contained ~2.5 MB add-on that statically links the framework.
const (
	// RenoDXTag is the release the mods come from. RenoDX publishes no
	// stable release at all: `snapshot` is a rolling prerelease that
	// always exists and always points at the current build, and it is
	// what RenoDX's own install instructions tell people to download.
	RenoDXTag = "snapshot"
	// RenoDXAssetBase is where every mod binary lives.
	RenoDXAssetBase = "https://github.com/clshortfuse/renodx/releases/download/" + RenoDXTag + "/"
	// RenoDXMetadataURL is the index describing the mods in that release.
	RenoDXMetadataURL = RenoDXAssetBase + "generated-metadata.json"
	// RenoDXRepoURL is shown wherever a mod cannot be installed.
	RenoDXRepoURL = "https://github.com/clshortfuse/renodx"
	// RenoDXMinReShade is the oldest ReShade that loads a RenoDX add-on,
	// per RenoDX's own installation instructions.
	RenoDXMinReShade = "6.8.0"
)

// renodxSchemaMajor is the metadata schema this parser understands.
const renodxSchemaMajor = 1

// RenoArtifact is one downloadable mod binary.
type RenoArtifact struct {
	// Name is the release asset's filename, e.g. renodx-cp2077.addon64.
	Name string
	Arch game.Arch
	Size int64
	// URL is the absolute download URL, derived from Name rather than
	// taken from the metadata — see resolveArtifact.
	URL string
}

// RenoMod is one RenoDX mod: a game, or an engine, or a utility.
type RenoMod struct {
	// ID is upstream's mod id, slugified and de-duplicated — see
	// ParseRenoDX for why it cannot be taken at face value.
	ID    string
	Title string
	// Status is upstream's own confidence in the mod: "stable", "beta",
	// or empty, which a third of them are. Empty is not a synonym for
	// broken, so it is reported as unverified rather than as a warning.
	Status string
	// SteamAppID is what makes this catalog worth having: yarm's game ids
	// are "steam:<appid>", so a mod can be matched to the game the wizard
	// is installing into rather than hunted for in a list of 200.
	// Zero when upstream does not say.
	SteamAppID int
	// API is deploy.api when upstream sets it. Exactly one mod declares
	// "vulkan", which yarm cannot install into at all.
	API string
	// Description is set only for the hand-kept extras below: upstream's
	// metadata carries no description for the mods it does describe.
	Description string
	Maintainers []string
	Artifacts   []RenoArtifact
	// Utility marks a hand-kept extra that does not replace a game's
	// shaders or tone mapping the way a per-game mod (or Generic,
	// standing in for one) does. FPS Limiter and DLSS Fix patch
	// something else entirely, so — unlike two mods that both hook the
	// swap chain — more than one can be active at once, and yarm treats
	// them as ordinary add-ons rather than folding them into RenoDX's
	// single choice.
	Utility bool
}

// Beta reports whether upstream flags this mod as still settling.
func (m RenoMod) Beta() bool { return m.Status == "beta" }

// Unverified reports whether upstream gives no status at all.
func (m RenoMod) Unverified() bool { return m.Status == "" }

// Unsupported names the reason yarm cannot install this mod regardless of
// the game, or "" when there is none.
func (m RenoMod) Unsupported() string {
	if strings.EqualFold(m.API, "vulkan") {
		return "Vulkan"
	}
	return ""
}

// ArtifactFor returns the binary for a game of the given architecture.
//
// Deliberately an exact match, with none of the 64-bit fallback that
// Addon.SourceFor applies. An add-on of the wrong architecture is not
// degraded, it is inert: the loader rejects it and ReShade reports
// nothing yarm could surface. Twenty-one mods are 32-bit only, so this
// is a case that comes up rather than a theoretical one.
//
// An unknown architecture resolves to 64-bit, which is the right guess
// for anything modern and matches the rest of the package.
func (m RenoMod) ArtifactFor(arch game.Arch) (RenoArtifact, bool) {
	if arch == game.ArchUnknown {
		arch = game.ArchX64
	}
	for _, a := range m.Artifacts {
		if a.Arch == arch {
			return a, true
		}
	}
	return RenoArtifact{}, false
}

// UnsupportedSchemaError reports metadata this parser was not written
// against. Typed so the caller can log it and carry on with the rest of
// the catalog rather than failing the whole wizard.
type UnsupportedSchemaError struct{ Version string }

func (e UnsupportedSchemaError) Error() string {
	return "unsupported RenoDX metadata schema " + e.Version
}

// renodxDoc mirrors generated-metadata.json. Every field yarm does not
// use is left out deliberately: unknown keys must not break parsing,
// because upstream generates this file from a script in its own repo and
// adds to it.
type renodxDoc struct {
	SchemaVersion string `json:"schema_version"`
	Mods          []struct {
		ID          string   `json:"id"`
		Title       string   `json:"title"`
		Status      string   `json:"status"`
		Maintainers []string `json:"maintainers"`
		Deploy      struct {
			SteamAppID int    `json:"steam_appid"`
			API        string `json:"api"`
		} `json:"deploy"`
		Artifacts []struct {
			Name string `json:"name"`
			Arch string `json:"arch"`
			Size int64  `json:"size"`
		} `json:"artifacts"`
	} `json:"mods"`
}

// ParseRenoDX reads the RenoDX release metadata, newest schema first.
//
// The hand-kept extras below are appended, so callers get one list.
func ParseRenoDX(r io.Reader) ([]RenoMod, error) {
	var doc renodxDoc
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse RenoDX metadata: %w", err)
	}
	if !renodxSchemaSupported(doc.SchemaVersion) {
		return nil, UnsupportedSchemaError{Version: doc.SchemaVersion}
	}

	extras := renodxExtras()

	// The hand-kept ids are referenced by name elsewhere (curated.go,
	// the wizard's utility rows), so they claim their slugs first: an
	// upstream mod that collides with one is the one that gets suffixed.
	taken := make(map[string]bool, len(doc.Mods)+len(extras))
	for _, e := range extras {
		taken[e.ID] = true
	}

	out := make([]RenoMod, 0, len(doc.Mods)+len(extras))
	for _, m := range doc.Mods {
		if m.ID == "" {
			continue
		}
		mod := RenoMod{
			Title:       m.Title,
			Status:      m.Status,
			SteamAppID:  m.Deploy.SteamAppID,
			API:         m.Deploy.API,
			Maintainers: m.Maintainers,
		}
		if mod.Title == "" {
			mod.Title = m.ID
		}
		for _, a := range m.Artifacts {
			art, ok := resolveArtifact(a.Name, a.Arch, a.Size)
			if !ok {
				continue
			}
			mod.Artifacts = append(mod.Artifacts, art)
		}
		// A mod with nothing installable is not worth a row: it can only
		// ever be shown and refused.
		if len(mod.Artifacts) == 0 {
			continue
		}
		// Slugified for the same reason resolveArtifact rebuilds the URL
		// rather than trusting the one in the file: this id arrives in a
		// document yarm downloaded, and it becomes a directory name
		// under the cache root (cache.EnsureRenoDX) and a manifest
		// Origin. Slugify keeps nothing but letters and digits, so a
		// separator or a ".." cannot survive to choose where a download
		// lands, and uniqueSlug stops two mods from sharing one cache
		// directory — exactly what packages and add-ons already do.
		// Claimed after the skips above, so a mod with no installable
		// artifact does not take a slug a later one could use.
		mod.ID = uniqueSlug(Slugify(m.ID), taken)
		out = append(out, mod)
	}

	return append(out, extras...), nil
}

// renodxSchemaSupported accepts any minor of the major this parser was
// written against. An empty version is accepted too: the field is young
// and its absence is likelier to mean an older generator than a
// different format.
func renodxSchemaSupported(v string) bool {
	if v == "" {
		return true
	}
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	return err == nil && n == renodxSchemaMajor
}

// resolveArtifact builds the download URL from the asset's own filename.
//
// The metadata also carries a "url" field, but it is a relative path
// inside a file yarm downloaded, and a downloaded file must not get to
// choose a download URL. Building from the name and validating it means
// the only thing the metadata can influence is which asset of the
// snapshot release is fetched — never which host, and never which path.
//
// The extension allowlist is the other half of that. The same release
// carries 230 .pdb debug files at about 13 MB each, and four developer
// .exe tools. None of them appear in the artifacts arrays today; this is
// what keeps it that way if upstream's generator ever changes.
func resolveArtifact(name, arch string, size int64) (RenoArtifact, bool) {
	if name == "" || name != baseName(name) {
		return RenoArtifact{}, false
	}
	switch strings.ToLower(extOf(name)) {
	case ".addon32", ".addon64":
	default:
		return RenoArtifact{}, false
	}

	var a game.Arch
	switch arch {
	case "x64":
		a = game.ArchX64
	case "x86":
		a = game.ArchX86
	default:
		return RenoArtifact{}, false
	}

	return RenoArtifact{Name: name, Arch: a, Size: size, URL: RenoDXAssetBase + name}, true
}

// baseName returns the last path element of name under either separator,
// so a name carrying any path at all fails the equality check above.
func baseName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		return name[i+1:]
	}
	return name
}

func extOf(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i:]
	}
	return ""
}

// renodxExtras are mods the release builds but the metadata does not
// describe, added by hand.
//
// Upstream's index covers the mods that map to a game. These three do
// not — they apply to anything — so they are absent from it while being
// exactly the ones worth reaching for when no game-specific mod exists.
// Kept here rather than derived, for the same reason requires.go is
// hand-kept: there is nothing upstream to derive them from.
//
// Generic is a stand-in for a per-game mod — the same kind of shader and
// tone-mapping replacement, just not tied to one game — so it stays
// exclusive with everything else RenoDX offers. DLSS Fix and FPS Limiter
// patch something unrelated to that and are marked Utility.
//
// Deliberately not included: renodx-devkit, which is a development tool,
// and the emulator titles, which are absent from the index because they
// are not Steam games and which yarm's discovery would never surface.
func renodxExtras() []RenoMod {
	extras := []struct {
		id, title, desc string
		utility         bool
	}{
		{"generic", "RenoDX Generic", "Works with many games that have no mod of their own", false},
		{"dlssfix", "DLSS Fix", "Corrects DLSS rendering issues", true},
		{"fpslimiter", "FPS Limiter", "Frame rate limiter", true},
	}

	out := make([]RenoMod, 0, len(extras))
	for _, e := range extras {
		mod := RenoMod{
			ID: e.id, Title: e.title, Description: e.desc,
			Maintainers: []string{"RenoDX"}, Utility: e.utility,
		}
		for _, arch := range []game.Arch{game.ArchX64, game.ArchX86} {
			suffix := ".addon64"
			if arch == game.ArchX86 {
				suffix = ".addon32"
			}
			name := "renodx-" + e.id + suffix
			mod.Artifacts = append(mod.Artifacts, RenoArtifact{
				Name: name, Arch: arch, URL: RenoDXAssetBase + name,
			})
		}
		out = append(out, mod)
	}
	return out
}
