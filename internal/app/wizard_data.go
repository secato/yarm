package app

import (
	"context"
	"fmt"

	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/game"
)

// WizardData is everything the install wizard needs from the catalog and
// from cache/custom, gathered once when the wizard opens.
type WizardData struct {
	Versions      []catalog.Version
	Packages      []catalog.Package
	Addons        []catalog.Addon
	CustomShaders []catalog.Custom
	CustomAddons  []catalog.Custom
}

// WizardDataLoader supplies WizardData. An interface so tests can hand the
// wizard a fixed fixture instead of reaching the network and disk.
type WizardDataLoader interface {
	LoadWizardData(ctx context.Context) (WizardData, error)
}

// CatalogWizardData loads WizardData from a real catalog client and the
// cache's custom-content directory.
type CatalogWizardData struct {
	Client    *catalog.Client
	CustomDir string
}

// LoadWizardData implements WizardDataLoader.
//
// A failure in one source does not suppress the others — an expired
// GitHub rate limit should not stop the wizard from at least offering
// versions and packages it already has cached — but the wizard cannot
// proceed with nothing at all to offer.
func (l CatalogWizardData) LoadWizardData(ctx context.Context) (WizardData, error) {
	var data WizardData

	versions, vErr := l.Client.Versions(ctx)
	data.Versions = versions

	packages, pErr := l.Client.Packages(ctx)
	data.Packages = packages

	addons, aErr := l.Client.Addons(ctx)
	data.Addons = addons

	custom, cErr := catalog.ScanCustom(l.CustomDir)
	for _, c := range custom {
		switch c.Kind {
		case catalog.CustomShaders:
			data.CustomShaders = append(data.CustomShaders, c)
		case catalog.CustomAddons:
			data.CustomAddons = append(data.CustomAddons, c)
		}
	}

	if len(data.Versions) == 0 && len(data.Packages) == 0 && len(data.Addons) == 0 {
		return data, firstErr(vErr, pErr, aErr, cErr)
	}
	return data, nil
}

// firstErr returns the first non-nil error, or nil.
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// PackagesWithCustom returns the catalog packages followed by any custom
// shader folders, the custom ones under a non-selectable header — the
// layout §6.1 describes for the packages step.
func (d WizardData) PackagesWithCustom(cache CacheStatus) []selectItem {
	items := make([]selectItem, 0, len(d.Packages)+len(d.CustomShaders)+1)
	for _, p := range d.Packages {
		items = append(items, selectItem{
			ID:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			Required:    p.Required,
			Cached:      cache != nil && cache.HasPackage(p.ID),
		})
	}
	if len(d.CustomShaders) > 0 {
		items = append(items, selectItem{Header: true, Name: "── Custom ──"})
		for _, c := range d.CustomShaders {
			items = append(items, selectItem{
				ID: c.ID, Name: c.Name, Description: c.Description, Cached: true,
			})
		}
	}
	return items
}

// AddonsWithCustom returns the catalog add-ons followed by custom add-on
// folders. Manual-only add-ons are included but marked Disabled, per §6.1
// ("manual entries greyed with URL").
func (d WizardData) AddonsWithCustom(cache CacheStatus) []selectItem {
	items := make([]selectItem, 0, len(d.Addons)+len(d.CustomAddons)+1)
	for _, a := range d.Addons {
		items = append(items, selectItem{
			ID:           a.ID,
			Name:         a.Name,
			Description:  a.Description,
			Disabled:     !a.Installable(),
			DisabledNote: a.RepositoryURL,
			Cached:       cache != nil && cache.HasAddon(a.ID),
		})
	}
	if len(d.CustomAddons) > 0 {
		items = append(items, selectItem{Header: true, Name: "── Custom ──"})
		for _, c := range d.CustomAddons {
			items = append(items, selectItem{
				ID: c.ID, Name: c.Name, Description: c.Description, Cached: true,
			})
		}
	}
	return items
}

// CacheStatus answers whether an artifact is already cached, so the
// wizard can show a badge without triggering a download. Implemented by
// *cache.Cache.
type CacheStatus interface {
	HasReShade(version string, addon bool) bool
	HasPackage(id string) bool
	HasAddon(id string) bool
	HasD3DCompiler(arch game.Arch) bool
}

// describeMissing names what an install still needs to download, for the
// review step's warning list.
func describeMissing(cache CacheStatus, version string, addon bool, packages, addons []string, arch game.Arch, needsD3D bool) []string {
	var missing []string
	if cache == nil || !cache.HasReShade(version, addon) {
		flavor := "normal"
		if addon {
			flavor = "addon"
		}
		missing = append(missing, fmt.Sprintf("ReShade %s (%s)", version, flavor))
	}
	for _, id := range packages {
		if cache == nil || !cache.HasPackage(id) {
			missing = append(missing, "package "+id)
		}
	}
	for _, id := range addons {
		if cache == nil || !cache.HasAddon(id) {
			missing = append(missing, "add-on "+id)
		}
	}
	if needsD3D && (cache == nil || !cache.HasD3DCompiler(arch)) {
		missing = append(missing, "d3dcompiler_47.dll (~40 MB, once)")
	}
	return missing
}
