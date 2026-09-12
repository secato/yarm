package app

import (
	"context"
	"fmt"
	"sync"

	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
)

// WizardData is everything the install wizard needs from the catalog and
// from the custom folder, gathered once when the wizard opens.
type WizardData struct {
	Versions      []catalog.Version
	Packages      []catalog.Package
	Addons        []catalog.Addon
	RenoDX        []catalog.RenoMod
	CustomShaders []catalog.Custom
	CustomAddons  []catalog.Custom
}

// empty reports whether a load produced nothing at all — the state
// LoadWizardData treats as a failure.
//
// RenoDX is deliberately not counted. It comes from a different project
// on a different host, and its being unreachable must not stop the wizard
// from installing ReShade.
func (d WizardData) empty() bool {
	return len(d.Versions) == 0 && len(d.Packages) == 0 && len(d.Addons) == 0
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
// The five sources are independent — versions, packages, add-ons, RenoDX
// and the custom folder — so they load concurrently rather than summing
// their latencies into one cold open. A failure in one source does not
// suppress the others — an expired GitHub rate limit should not stop the
// wizard from at least offering versions and packages it already has
// cached — but the wizard cannot proceed with nothing at all to offer.
func (l CatalogWizardData) LoadWizardData(ctx context.Context) (WizardData, error) {
	var data WizardData
	var vErr, pErr, aErr, rErr error

	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		data.Versions, vErr = l.Client.Versions(ctx)
	}()
	go func() {
		defer wg.Done()
		data.Packages, pErr = l.Client.Packages(ctx)
	}()
	go func() {
		defer wg.Done()
		data.Addons, aErr = l.Client.Addons(ctx)
	}()
	go func() {
		defer wg.Done()
		data.RenoDX, rErr = l.Client.RenoDX(ctx)
	}()
	wg.Wait()

	custom, cErr := catalog.ScanCustom(l.CustomDir)
	for _, c := range custom {
		switch c.Kind {
		case catalog.CustomShaders:
			data.CustomShaders = append(data.CustomShaders, c)
		case catalog.CustomAddons:
			data.CustomAddons = append(data.CustomAddons, c)
		}
	}

	if data.empty() {
		return data, firstErr(vErr, pErr, aErr, rErr, cErr)
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
// shader folders, the custom ones under a non-selectable header so the
// list says where each entry came from.
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
// folders. Manual-only add-ons are included but marked Disabled, greyed
// out with the URL to fetch them from by hand: hiding them would leave
// the user wondering why an add-on they know of is missing.
func (d WizardData) AddonsWithCustom(cache CacheStatus) []selectItem {
	items := make([]selectItem, 0, len(d.Addons)+len(d.CustomAddons)+1)
	for _, a := range d.Addons {
		items = append(items, selectItem{
			ID:           a.ID,
			Name:         a.Name,
			Description:  a.Description,
			Disabled:     !a.Installable(),
			DisabledNote: manualNote(a.RepositoryURL),
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

// RenoDXGames is the catalog's RenoDX mods narrowed to what the RenoDX
// step itself offers: a mod for one game, or Generic standing in for
// one. Never more than one of these installs at once — see
// install.Request.RenoDX's own comment. The utility mods (FPS Limiter,
// DLSS Fix) sit beside ordinary add-ons instead — see fullAddons.
func (d WizardData) RenoDXGames() []catalog.RenoMod {
	out := make([]catalog.RenoMod, 0, len(d.RenoDX))
	for _, m := range d.RenoDX {
		if !m.Utility {
			out = append(out, m)
		}
	}
	return out
}

// isRenoDXUtility reports whether id names one of RenoDX's utility mods,
// so a caller that only has an add-on id can tell a RenoDX-sourced one
// (cached under the RenoDX bucket) from a catalog one (cached under the
// add-on bucket) without carrying its own separate list.
func (d WizardData) isRenoDXUtility(id string) bool {
	for _, m := range d.RenoDX {
		if m.Utility && m.ID == id {
			return true
		}
	}
	return false
}

// manualNote is the note on an add-on yarm cannot install: that it is
// manual, and where to get it by hand when the catalog says where.
func manualNote(repoURL string) string {
	if repoURL == "" {
		return "manual install only"
	}
	return "manual install only: " + repoURL
}

// CacheStatus answers whether an artifact is already cached, so the
// wizard can show a badge without triggering a download. Implemented by
// *cache.Cache.
type CacheStatus interface {
	HasReShade(version string, addon bool) bool
	HasPackage(id string) bool
	HasAddon(id string) bool
	HasRenoDX(id string) bool
	HasD3DCompiler(arch game.Arch) bool
}

// describeMissing names what an install still needs to download, for the
// review step's warning list. needed is the demand-driven set from
// NeededArtifacts: an edit whose installed files are unchanged resolves
// from the manifest, so listing those groups as "to download" would be
// false even when the cache is empty. isRenoDXAddon reports whether an
// add-on id is actually one of RenoDX's utility mods, which are cached
// under a different bucket than a catalog add-on and so need HasRenoDX
// rather than HasAddon.
func describeMissing(cache CacheStatus, needed install.Needed, version string, addon bool, packages, addons []string,
	isRenoDXAddon func(string) bool, renodx string, arch game.Arch, needsD3D bool,
) []string {
	var missing []string
	if needed.ReShade && (cache == nil || !cache.HasReShade(version, addon)) {
		flavor := "normal"
		if addon {
			flavor = "addon"
		}
		missing = append(missing, fmt.Sprintf("ReShade %s (%s)", version, flavor))
	}
	for _, id := range packages {
		if needed.Packages[id] && (cache == nil || !cache.HasPackage(id)) {
			missing = append(missing, "package "+id)
		}
	}
	for _, id := range addons {
		if !needed.Addons[id] {
			continue
		}
		cached := cache != nil
		if cached {
			if isRenoDXAddon(id) {
				cached = cache.HasRenoDX(id)
			} else {
				cached = cache.HasAddon(id)
			}
		}
		if !cached {
			missing = append(missing, "add-on "+id)
		}
	}
	// Named with its size: a RenoDX mod is a couple of megabytes, which
	// is worth saying when the rest of the list is shader packs.
	if needed.RenoDX && renodx != "" && (cache == nil || !cache.HasRenoDX(renodx)) {
		missing = append(missing, "RenoDX "+renodx+" (~2.5 MB)")
	}
	if needed.D3DCompiler && (cache == nil || !cache.HasD3DCompiler(arch)) {
		missing = append(missing, "d3dcompiler_47.dll (~40 MB, once)")
	}
	return missing
}
