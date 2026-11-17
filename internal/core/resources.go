package core

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/fsutil"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/state"
)

// Pane is one list of the Resources view.
type Pane int

// Panes, in the order the view shows them. ReShade's two builds are rows of
// one pane (each version is two downloads, one per build); RenoDX has its own
// because its ~200 mods would drown the add-ons.
const (
	PaneReShade Pane = iota
	PaneShaders
	PaneAddons
	PaneRenoDX
	PaneCount
)

// Label names a pane.
func (p Pane) Label() string {
	switch p {
	case PaneReShade:
		return "ReShade"
	case PaneShaders:
		return "Shaders"
	case PaneAddons:
		return "Add-ons"
	case PaneRenoDX:
		return "RenoDX"
	}
	return "?"
}

// ResourceRow is one browsable item: a ReShade version and build, a shader
// pack, an add-on or a RenoDX mod — from the catalog, from custom content,
// or left in the cache by something the catalog no longer offers.
type ResourceRow struct {
	ID   string
	Name string
	// Sub is the line under the name: the build for ReShade, the date for
	// anything downloaded.
	Sub    string
	Custom bool
	// Downloadable is false for custom content (already on disk) and for a
	// manual-only add-on.
	Downloadable bool
	Required     bool
	Cached       bool
	Size         int64
	DownloadedAt time.Time
	// UsedBy counts the recorded installs that use this exact item.
	UsedBy      int
	Description string

	pane           Pane
	reshadeVersion string
	reshadeAddon   bool
	pkg            catalog.Package
	addon          catalog.Addon
	renodx         catalog.RenoMod
}

// InUse reports whether any recorded install uses the item.
func (r ResourceRow) InUse() bool { return r.UsedBy > 0 }

// Resources is everything the Resources view shows: the rows of each pane
// and what the cache costs on disk.
type Resources struct {
	Panes   [PaneCount][]ResourceRow
	Total   int64
	Free    int64
	FreeErr error
}

// Items counts the cached downloads, and how many of them are in use.
func (r Resources) Items() (items, inUse int, size int64) {
	for _, rows := range r.Panes {
		for _, row := range rows {
			if !row.Cached || row.Custom {
				continue
			}
			items++
			size += row.Size
			if row.InUse() {
				inUse++
			}
		}
	}
	return items, inUse, size
}

// Visible is what a pane shows: everything with all set, or the curated
// shortlist plus whatever the user already has a relationship with —
// downloaded, in use or their own. ReShade is never filtered: its rows are
// versions, and there is no such thing as an obscure one.
func (r Resources) Visible(p Pane, all bool) []ResourceRow {
	rows := r.Panes[p]
	if all || p == PaneReShade {
		return rows
	}
	out := make([]ResourceRow, 0, len(rows))
	for _, row := range rows {
		own := row.Required || row.Cached || row.InUse() || row.Custom
		switch p {
		case PaneShaders:
			if keepInShortlist(row.ID, curatedPackages, own) {
				out = append(out, row)
			}
		case PaneAddons:
			if keepInShortlist(row.ID, curatedAddons, own) {
				out = append(out, row)
			}
		default:
			// RenoDX has no curated list: only the mods the user has used.
			if own {
				out = append(out, row)
			}
		}
	}
	return out
}

// LoadResources gathers everything the view shows: the catalog and custom
// content, the cache's own index, disk totals and every recorded install
// (to know what is in use). It does file I/O and may hit the network, so
// callers run it off the UI goroutine.
func LoadResources(ctx context.Context, deps Deps) (Resources, error) {
	var res Resources
	data, err := deps.WizardData.LoadWizardData(ctx)
	if err != nil && data.empty() {
		return res, err
	}
	entries, err := deps.Cache.List(cache.SortByName, false)
	if err != nil {
		return res, err
	}
	total, err := deps.Cache.Total()
	if err != nil {
		return res, err
	}
	reg, err := state.Load(deps.StateDir)
	if err != nil {
		return res, err
	}
	installs := reg.Installs()

	res.Panes[PaneReShade] = reshadeRows(data, deps.Cache, entries, installs)
	res.Panes[PaneShaders] = packageRows(data, deps.Cache, entries, installs)
	res.Panes[PaneAddons] = addonRows(data, deps.Cache, entries, installs)
	res.Panes[PaneRenoDX] = renodxRows(data, deps.Cache, entries, installs)
	// What is on disk comes first in the panes that list versions or a long
	// tail. Packs and add-ons keep catalog order, the order the tabs list
	// them in, so a pack sits in the same place on both.
	for _, p := range []Pane{PaneReShade, PaneRenoDX} {
		rows := res.Panes[p]
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Cached && !rows[j].Cached })
	}
	res.Total = total
	res.Free, res.FreeErr = fsutil.FreeSpace(deps.Cache.Root)
	return res, nil
}

func dateLine(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

func reshadeRows(data WizardData, c *cache.Cache, entries []cache.Entry, installs []state.GameInstall) []ResourceRow {
	// The newest ten, plus anything cached outside that window — otherwise an
	// older cached version would become invisible and undeletable just
	// because a newer one shipped.
	const shown = 10
	var versions []string
	seen := map[string]bool{}
	for i, v := range data.Versions {
		if i >= shown {
			break
		}
		versions = append(versions, v.Version)
		seen[v.Version] = true
	}
	for _, e := range entries {
		if e.Kind != cache.KindReShade {
			continue
		}
		parts := strings.SplitN(e.ID, ":", 3)
		if len(parts) != 3 || seen[parts[1]] {
			continue
		}
		seen[parts[1]] = true
		versions = append(versions, parts[1])
	}

	var rows []ResourceRow
	for _, v := range versions {
		for _, addon := range []bool{false, true} {
			flavor, desc := "normal", "Effects only, no add-on support"
			if addon {
				flavor, desc = "addon", "Loads add-ons; anti-cheat can detect it"
			}
			id := "reshade:" + v + ":" + flavor
			row := ResourceRow{
				ID: id, Name: "ReShade " + v, Sub: flavor + " build", Downloadable: true,
				Cached: c.HasReShade(v, addon), Description: desc,
				pane: PaneReShade, reshadeVersion: v, reshadeAddon: addon,
			}
			for _, in := range installs {
				if in.Install.ReShade.Version == v && in.Install.ReShade.Flavor == flavor {
					row.UsedBy++
				}
			}
			for _, e := range entries {
				if e.ID == id {
					row.Size, row.DownloadedAt = e.Size, e.DownloadedAt
				}
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func packageRows(data WizardData, c *cache.Cache, entries []cache.Entry, installs []state.GameInstall) []ResourceRow {
	rows := make([]ResourceRow, 0, len(data.Packages)+len(data.CustomShaders))
	for _, p := range data.Packages {
		m := entriesForPrefix(entries, "package:"+p.ID+":")
		rows = append(rows, ResourceRow{
			ID: p.ID, Name: p.Name, Downloadable: true, Required: p.Required,
			Cached: c.HasPackage(p.ID), Size: sumSize(m), DownloadedAt: latestDownload(m),
			UsedBy: usedByID(installs, p.ID), Description: p.Description,
			pane: PaneShaders, pkg: p,
		})
	}
	for _, cst := range data.CustomShaders {
		rows = append(rows, ResourceRow{
			ID: cst.ID, Name: cst.Name, Custom: true, Cached: true,
			UsedBy: usedByID(installs, cst.ID), Description: cst.Description, pane: PaneShaders,
		})
	}
	setSubs(rows)
	return rows
}

func addonRows(data WizardData, c *cache.Cache, entries []cache.Entry, installs []state.GameInstall) []ResourceRow {
	rows := make([]ResourceRow, 0, len(data.Addons)+len(data.CustomAddons))
	for _, a := range data.Addons {
		m := entriesForPrefix(entries, "addon:"+a.ID+":")
		rows = append(rows, ResourceRow{
			ID: a.ID, Name: a.Name, Downloadable: a.Installable(),
			Cached: c.HasAddon(a.ID), Size: sumSize(m), DownloadedAt: latestDownload(m),
			UsedBy: usedByID(installs, a.ID), Description: a.Description,
			pane: PaneAddons, addon: a,
		})
	}
	for _, cst := range data.CustomAddons {
		rows = append(rows, ResourceRow{
			ID: cst.ID, Name: cst.Name, Custom: true, Cached: true,
			UsedBy: usedByID(installs, cst.ID), Description: cst.Description, pane: PaneAddons,
		})
	}
	setSubs(rows)
	return rows
}

func renodxRows(data WizardData, c *cache.Cache, entries []cache.Entry, installs []state.GameInstall) []ResourceRow {
	rows := make([]ResourceRow, 0, len(data.RenoDX))
	for _, m := range data.RenoDX {
		e := entriesForPrefix(entries, "renodx:"+m.ID+":")
		used := 0
		for _, in := range installs {
			if in.Install.RenoDX == m.ID || slices.Contains(in.Install.Addons, m.ID) {
				used++
			}
		}
		rows = append(rows, ResourceRow{
			ID: m.ID, Name: m.Title, Downloadable: true,
			Cached: c.HasRenoDX(m.ID), Size: sumSize(e), DownloadedAt: latestDownload(e),
			UsedBy: used, Description: renodxDescription(m),
			pane: PaneRenoDX, renodx: m,
		})
	}
	setSubs(rows)
	return rows
}

// setSubs fills the line under each name: when it was downloaded, or what
// it is when it was not.
func setSubs(rows []ResourceRow) {
	for i := range rows {
		r := &rows[i]
		switch {
		case r.Custom:
			r.Sub = "Your own content"
		case !r.DownloadedAt.IsZero():
			r.Sub = dateLine(r.DownloadedAt)
		case !r.Downloadable:
			r.Sub = "Installed by hand"
		}
	}
}

// renodxDescription says what a RenoDX row is, since upstream publishes no
// per-mod description of its own.
func renodxDescription(m catalog.RenoMod) string {
	parts := []string{"RenoDX mod"}
	if m.Description != "" {
		parts = []string{m.Description}
	}
	if m.Beta() {
		parts = append(parts, "beta")
	}
	if len(m.Maintainers) > 0 {
		parts = append(parts, "by "+strings.Join(m.Maintainers, ", "))
	}
	return strings.Join(parts, " — ")
}

// usedByID counts the installs that reference id. Custom content shares a
// pack's or add-on's selection list, so its id can land in either field.
func usedByID(installs []state.GameInstall, id string) int {
	n := 0
	for _, gi := range installs {
		if slices.Contains(gi.Install.Packages, id) ||
			slices.Contains(gi.Install.Addons, id) ||
			slices.Contains(gi.Install.Custom, id) {
			n++
		}
	}
	return n
}

func entriesForPrefix(entries []cache.Entry, prefix string) []cache.Entry {
	var out []cache.Entry
	for _, e := range entries {
		if strings.HasPrefix(e.ID, prefix) {
			out = append(out, e)
		}
	}
	return out
}

func sumSize(entries []cache.Entry) int64 {
	var total int64
	for _, e := range entries {
		total += e.Size
	}
	return total
}

func latestDownload(entries []cache.Entry) time.Time {
	var latest time.Time
	for _, e := range entries {
		if e.DownloadedAt.After(latest) {
			latest = e.DownloadedAt
		}
	}
	return latest
}

// DownloadResource fetches an item ahead of any install. A no-op for what
// is already cached, custom content (nothing to fetch) and anything with
// nothing installable.
func DownloadResource(ctx context.Context, deps Deps, row ResourceRow) error {
	if row.Cached || row.Custom || !row.Downloadable {
		return nil
	}
	switch row.pane {
	case PaneReShade:
		_, err := deps.Cache.EnsureReShade(ctx, row.reshadeVersion, row.reshadeAddon, nil)
		return err
	case PaneShaders:
		_, err := deps.Cache.EnsurePackage(ctx, row.pkg, nil)
		return err
	case PaneAddons:
		return ensureAddonAllArches(ctx, deps.Cache, row.addon)
	case PaneRenoDX:
		return ensureRenoDXAllArches(ctx, deps.Cache, row.renodx)
	}
	return nil
}

// ensureRenoDXAllArches downloads every architecture a RenoDX mod
// publishes, so the mod is ready whichever game it is later installed into.
// Most publish exactly one.
func ensureRenoDXAllArches(ctx context.Context, c *cache.Cache, m catalog.RenoMod) error {
	tried := 0
	var lastErr error
	for _, arch := range []game.Arch{game.ArchX64, game.ArchX86} {
		if _, ok := m.ArtifactFor(arch); !ok {
			continue
		}
		tried++
		if _, err := c.EnsureRenoDX(ctx, m, arch, nil); err != nil {
			lastErr = err
		}
	}
	if tried == 0 {
		return fmt.Errorf("RenoDX %s publishes nothing installable", m.ID)
	}
	return lastErr
}

// ensureAddonAllArches downloads whichever architectures an add-on actually
// publishes. One that ships a single architecture-neutral build is fetched
// once, not duplicated per arch.
func ensureAddonAllArches(ctx context.Context, c *cache.Cache, a catalog.Addon) error {
	arches := []game.Arch{game.ArchX64}
	if a.ArchSpecific() {
		arches = append(arches, game.ArchX86)
	}
	tried := 0
	var lastErr error
	for _, arch := range arches {
		if _, ok := a.SourceFor(arch); !ok {
			continue
		}
		tried++
		if _, err := c.EnsureAddon(ctx, a, arch, nil); err != nil {
			lastErr = err
		}
	}
	if tried == 0 {
		return fmt.Errorf("add-on %s publishes nothing installable", a.ID)
	}
	return lastErr
}

// DeleteResource removes every cached entry of an item — there can be more
// than one for a pack or add-on, since package cache keys carry a download
// date and content hash, and re-fetching an updated branch keeps the old
// one around until it is cleaned up. An item a recorded install uses is
// refused: the install would keep working, but updating it would mean
// downloading again, and that is a choice for Clean unused, not a stray
// click.
func DeleteResource(deps Deps, row ResourceRow) error {
	if row.Custom || !row.Cached {
		return nil
	}
	if row.InUse() {
		return fmt.Errorf("%s is used by an install, so it is kept", row.Name)
	}
	var prefix string
	switch row.pane {
	case PaneReShade:
		prefix = row.ID
	case PaneShaders:
		prefix = "package:" + row.ID + ":"
	case PaneAddons:
		prefix = "addon:" + row.ID + ":"
	case PaneRenoDX:
		prefix = "renodx:" + row.ID + ":"
	}
	entries, err := deps.Cache.List(cache.SortByName, false)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.ID == prefix || strings.HasPrefix(e.ID, prefix) {
			if err := deps.Cache.Delete(e.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// CleanUnused clears every download no recorded install uses, and reports
// how much it freed and how many items stayed. Installs resolve from what
// they use when edited, so those stay.
func CleanUnused(deps Deps) (freed int64, kept int, err error) {
	reg, err := state.Load(deps.StateDir)
	if err != nil {
		return 0, 0, err
	}
	prefixes := inUsePrefixes(reg)
	keep := func(id string) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(id, p) {
				return true
			}
		}
		return false
	}
	// The index is read here, not earlier, so the policy was decided by the
	// user and the sweep sees current truth.
	idx, err := deps.Cache.List(cache.SortByName, false)
	if err != nil {
		return 0, 0, err
	}
	for _, e := range idx {
		if keep(e.ID) {
			kept++
		} else {
			freed += e.Size
		}
	}
	if kept == len(idx) {
		return 0, kept, nil
	}
	if _, err := deps.Cache.CleanExcept(keep); err != nil {
		return 0, kept, err
	}
	return freed, kept, nil
}

// inUsePrefixes lists the cache-entry id prefixes every recorded install
// depends on: the exact reshade and d3dcompiler ids plus one prefix per
// package, add-on and RenoDX mod id.
func inUsePrefixes(reg state.Registry) []string {
	var out []string
	for _, gi := range reg.Installs() {
		in := gi.Install
		out = append(out,
			"reshade:"+in.ReShade.Version+":"+in.ReShade.Flavor,
			"d3dcompiler:"+in.ReShade.Arch,
		)
		if in.RenoDX != "" {
			out = append(out, "renodx:"+in.RenoDX+":")
		}
		for _, id := range in.Packages {
			out = append(out, "package:"+id+":")
		}
		for _, id := range in.Addons {
			// Utility RenoDX mods cache under the renodx bucket; catalog
			// add-ons under the addon bucket. Keeping both prefixes for an
			// id only ever over-retains, never deletes in error.
			out = append(out, "addon:"+id+":", "renodx:"+id+":")
		}
	}
	return out
}

// RefreshResource re-fetches a cached shader pack, picking up any upstream
// change even within the same day. Only packs carry enough of their own
// metadata to do this without the live catalog.
func RefreshResource(ctx context.Context, deps Deps, row ResourceRow) error {
	if row.pane != PaneShaders || row.Custom || !row.Cached {
		return nil
	}
	if _, err := deps.Cache.EnsurePackage(ctx, row.pkg, nil); err != nil {
		return fmt.Errorf("refresh %s: %w", row.Name, err)
	}
	return nil
}

// CanRefresh reports whether RefreshResource does anything for the row.
func (r ResourceRow) CanRefresh() bool { return r.pane == PaneShaders && !r.Custom && r.Cached }
