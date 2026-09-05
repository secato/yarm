package catalog

import "io"

// Package is one effect package from EffectPackages.ini.
type Package struct {
	// ID is the slugified PackageName, unique within a catalog load.
	ID          string
	Name        string
	Description string
	// InstallPath and TextureInstallPath are the upstream-suggested
	// destinations relative to the game dir, e.g.
	// `.\reshade-shaders\Shaders`. Kept verbatim; the install engine
	// normalizes separators.
	InstallPath        string
	TextureInstallPath string
	DownloadURL        string
	RepositoryURL      string
	// EffectFiles, when non-empty, restricts installation to these .fx
	// files (plus every .fxh header). Empty means install everything.
	EffectFiles []string
	// DenyEffectFiles are never installed, even if listed in EffectFiles.
	DenyEffectFiles []string
	// Enabled marks packages upstream preselects in its own installer.
	Enabled bool
	// Required marks packages that should not be deselectable (Standard
	// effects ships DisplayDepth, which other effects depend on).
	Required bool
}

// ParsePackages reads EffectPackages.ini. Sections without a PackageName
// or DownloadUrl are skipped: there is nothing installable to show.
func ParsePackages(r io.Reader) ([]Package, error) {
	sections, err := ParseINI(r)
	if err != nil {
		return nil, err
	}

	taken := make(map[string]bool, len(sections))
	out := make([]Package, 0, len(sections))

	for _, s := range sections {
		name := s.Get("PackageName")
		if name == "" || !s.Has("DownloadUrl") {
			continue
		}

		out = append(out, Package{
			ID:                 uniqueSlug(Slugify(name), taken),
			Name:               name,
			Description:        s.Get("PackageDescription"),
			InstallPath:        s.Get("InstallPath"),
			TextureInstallPath: s.Get("TextureInstallPath"),
			DownloadURL:        s.Get("DownloadUrl"),
			RepositoryURL:      s.Get("RepositoryUrl"),
			EffectFiles:        s.List("EffectFiles"),
			DenyEffectFiles:    s.List("DenyEffectFiles"),
			Enabled:            s.Flag("Enabled"),
			Required:           s.Flag("Required"),
		})
	}

	return out, nil
}
