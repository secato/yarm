package catalog

import (
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/secato/yarm/internal/game"
)

// AddonKind summarizes how an add-on's files are obtained, for display.
type AddonKind string

// AddonKind values.
const (
	// AddonDirect ships the .addon32/.addon64 file at the URL itself.
	AddonDirect AddonKind = "direct"
	// AddonZip ships an archive that must be unpacked to find the addon
	// files.
	AddonZip AddonKind = "zip"
	// AddonManual has no usable download — it ships an installer, a set of
	// extra DLLs, or only source. Shown with its repository URL and not
	// installable in v1.
	AddonManual AddonKind = "manual"
)

// Source is how to obtain an add-on's binary for one architecture.
type Source struct {
	URL string
	// Archive is true when URL points at a zip that must be unpacked to
	// find *.addon32/*.addon64, rather than at the addon file itself.
	//
	// This cannot be inferred from which catalog key supplied the URL:
	// upstream puts zip links in DownloadUrl32/DownloadUrl64 for half of
	// its entries (ShaderToggler, IGCS Connector, seri14's add-ons, ...),
	// so only the URL's own extension is reliable.
	Archive bool
}

// Addon is one entry from Addons.ini.
type Addon struct {
	ID          string
	Name        string
	Description string
	// URL32 and URL64 are the per-architecture download links. URLAny is
	// the architecture-neutral fallback used by entries that publish a
	// single archive serving both.
	URL32         string
	URL64         string
	URLAny        string
	RepositoryURL string
	// EffectInstallPath is set by the few add-ons that also ship shaders
	// (REST, Xinp, 3DGameBridge), naming a subdirectory under the game's
	// shader directory. Undocumented upstream but present in the live
	// catalog.
	EffectInstallPath string
}

// SourceFor returns how to obtain this add-on for a game of the given
// architecture. ok is false when the add-on publishes nothing usable for
// that architecture — some entries ship a 64-bit build only.
//
// An unknown architecture resolves to the 64-bit build, which is the right
// guess for anything modern and is only ever a default the wizard can
// override.
func (a Addon) SourceFor(arch game.Arch) (s Source, ok bool) {
	candidate := a.URL64
	if arch == game.ArchX86 {
		candidate = a.URL32
	}
	if candidate == "" {
		candidate = a.URLAny
	}
	if candidate == "" {
		return Source{}, false
	}
	return Source{URL: candidate, Archive: isArchiveURL(candidate)}, true
}

// Kind classifies the add-on for display.
func (a Addon) Kind() AddonKind {
	switch {
	case a.URL32 == "" && a.URL64 == "" && a.URLAny == "":
		return AddonManual
	case isArchiveURL(cmpFirst(a.URL64, a.URL32, a.URLAny)):
		return AddonZip
	default:
		return AddonDirect
	}
}

// Installable reports whether YARM can install this add-on in v1.
func (a Addon) Installable() bool { return a.Kind() != AddonManual }

// ArchSpecific reports whether the add-on publishes different files per
// architecture, so the installer must know the game's arch to pick one.
func (a Addon) ArchSpecific() bool { return a.URL32 != "" || a.URL64 != "" }

// cmpFirst returns the first non-empty string.
func cmpFirst(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// isArchiveURL reports whether a download link points at a zip, ignoring
// any query string or fragment.
func isArchiveURL(raw string) bool {
	if u, err := url.Parse(raw); err == nil && u.Path != "" {
		raw = u.Path
	}
	return strings.EqualFold(path.Ext(raw), ".zip")
}

// ParseAddons reads Addons.ini and classifies each entry. Sections without
// a PackageName are skipped. Note that ParseINI drops commented-out
// blocks, which is what keeps upstream's retired entries out of the list.
func ParseAddons(r io.Reader) ([]Addon, error) {
	sections, err := ParseINI(r)
	if err != nil {
		return nil, err
	}

	taken := make(map[string]bool, len(sections))
	out := make([]Addon, 0, len(sections))

	for _, s := range sections {
		name := s.Get("PackageName")
		if name == "" {
			continue
		}

		out = append(out, Addon{
			ID:                uniqueSlug(Slugify(name), taken),
			Name:              name,
			Description:       s.Get("PackageDescription"),
			URL32:             s.Get("DownloadUrl32"),
			URL64:             s.Get("DownloadUrl64"),
			URLAny:            s.Get("DownloadUrl"),
			RepositoryURL:     s.Get("RepositoryUrl"),
			EffectInstallPath: s.Get("EffectInstallPath"),
		})
	}

	return out, nil
}
