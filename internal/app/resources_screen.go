package app

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/fsutil"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/state"
)

// resourcePane indexes ResourcesScreen's four panes. ReShade is split into
// two — normal and add-on are separate downloads, so a version's cached
// state differs per flavor — rather than one pane repeating "(normal)"/
// "(addon)" on every row.
type resourcePane int

const (
	paneReShadeNormal resourcePane = iota
	paneReShadeAddon
	panePackages
	paneAddons
	paneCount
)

func (p resourcePane) label() string {
	switch p {
	case paneReShadeNormal:
		return "ReShade (normal)"
	case paneReShadeAddon:
		return "ReShade (addon)"
	case panePackages:
		return "Shaders"
	case paneAddons:
		return "Add-ons"
	default:
		return "?"
	}
}

// resourceRow is one browsable item: a ReShade version+flavor, an effect
// package, or an add-on — catalog-sourced, custom-provided, or left over
// in the cache from something the catalog no longer offers.
type resourceRow struct {
	ID     string
	Name   string
	Custom bool
	// Downloadable is false for custom content (already on disk — there
	// is nothing to fetch) and a manual-only add-on.
	Downloadable bool
	Cached       bool
	Size         int64
	DownloadedAt time.Time
	// InUse reports whether any recorded install, for any game, actually
	// references this exact item.
	InUse bool
	// Description is the catalog's own one-liner, shown for the focused
	// row. Empty for rows the catalog says nothing about.
	Description string

	// Exactly one of these is meaningful, matching which pane the row
	// belongs to — carried along so the download/refresh actions have
	// what they need without re-deriving it from the ID.
	reshadeVersion string
	reshadeAddon   bool
	pkg            catalog.Package
	addon          catalog.Addon
}

// resourcesLoadedMsg carries a full (re)load of every pane, success or
// not. Built as a plain tea.Cmd rather than through Async, so a failure
// still reaches this screen's own Update instead of leaving "loading…" (or
// "downloading…") showing forever once the shell's error dialog closed.
type resourcesLoadedMsg struct {
	panes   [paneCount][]resourceRow
	total   int64
	free    int64
	freeErr error
	err     error
}

// resourceActionMsg reports a download, delete or refresh failure — a
// success re-loads everything instead (a resourcesLoadedMsg), since it is
// simplest to just recompute what changed rather than patch one row.
type resourceActionMsg struct{ err error }

// loadResources gathers everything the screen shows, off the UI
// goroutine: the catalog/custom-content listing, the cache's own index,
// disk totals, and every recorded install (to know what is "in use").
func loadResources(deps Deps) resourcesLoadedMsg {
	data, err := deps.WizardData.LoadWizardData(context.Background())
	if err != nil && data.empty() {
		return resourcesLoadedMsg{err: err}
	}

	entries, err := deps.Cache.List(cache.SortByName, false)
	if err != nil {
		return resourcesLoadedMsg{err: err}
	}
	total, err := deps.Cache.Total()
	if err != nil {
		return resourcesLoadedMsg{err: err}
	}
	free, freeErr := fsutil.FreeSpace(deps.Cache.Root)

	reg, err := state.Load(deps.StateDir)
	if err != nil {
		return resourcesLoadedMsg{err: err}
	}
	installs := reg.Installs()

	var msg resourcesLoadedMsg
	msg.panes[paneReShadeNormal], msg.panes[paneReShadeAddon] = buildReShadeRows(data, deps.Cache, entries, installs)
	msg.panes[panePackages] = buildPackageRows(data, deps.Cache, entries, installs)
	msg.panes[paneAddons] = buildAddonRows(data, deps.Cache, entries, installs)
	for p := range msg.panes {
		sortCachedFirst(msg.panes[p])
	}
	msg.total, msg.free, msg.freeErr = total, free, freeErr
	return msg
}

// sortCachedFirst orders rows so anything already on disk (cached or
// custom — custom content is always Cached) comes before what still
// needs downloading, preserving each group's original relative order.
func sortCachedFirst(rows []resourceRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Cached && !rows[j].Cached
	})
}

// buildReShadeRows returns one row per shown version for each flavor —
// normal and add-on are separate downloads with independent cached state,
// so they are two parallel lists (one per pane) rather than one list with
// twice the rows.
func buildReShadeRows(data WizardData, c *cache.Cache, entries []cache.Entry, installs []state.GameInstall) (normal, addon []resourceRow) {
	versions := reshadeVersionsToShow(data, entries)
	normal = make([]resourceRow, 0, len(versions))
	addon = make([]resourceRow, 0, len(versions))
	for _, version := range versions {
		normal = append(normal, reshadeRow(version, false, c, entries, installs))
		addon = append(addon, reshadeRow(version, true, c, entries, installs))
	}
	return normal, addon
}

// reshadeVersionsToShow lists the top 10 catalog versions plus anything
// else still cached outside that window — otherwise an older cached
// version would become invisible and undeletable through this screen just
// because a newer one shipped.
func reshadeVersionsToShow(data WizardData, entries []cache.Entry) []string {
	const shown = 10
	versions := make([]string, 0, shown)
	seen := map[string]bool{}
	top := data.Versions
	if len(top) > shown {
		top = top[:shown]
	}
	for _, v := range top {
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
	return versions
}

func reshadeRow(version string, addon bool, c *cache.Cache, entries []cache.Entry, installs []state.GameInstall) resourceRow {
	flavor := "normal"
	if addon {
		flavor = "addon"
	}
	id := "reshade:" + version + ":" + flavor
	row := resourceRow{
		ID:             id,
		Name:           version,
		Downloadable:   true,
		Cached:         c.HasReShade(version, addon),
		InUse:          inUseReShade(installs, version, addon),
		reshadeVersion: version,
		reshadeAddon:   addon,
		Description:    reshadeFlavorDescription(addon),
	}
	for _, e := range entries {
		if e.ID == id {
			row.Size, row.DownloadedAt = e.Size, e.DownloadedAt
			break
		}
	}
	return row
}

func buildPackageRows(data WizardData, c *cache.Cache, entries []cache.Entry, installs []state.GameInstall) []resourceRow {
	rows := make([]resourceRow, 0, len(data.Packages)+len(data.CustomShaders))
	for _, p := range data.Packages {
		matches := entriesForPrefix(entries, "package:"+p.ID+":")
		rows = append(rows, resourceRow{
			ID:           p.ID,
			Name:         p.Name,
			Downloadable: true,
			Cached:       c.HasPackage(p.ID),
			Size:         sumSize(matches),
			DownloadedAt: latestDownload(matches),
			InUse:        inUseID(installs, p.ID),
			Description:  p.Description,
			pkg:          p,
		})
	}
	for _, cst := range data.CustomShaders {
		rows = append(rows, resourceRow{
			ID: cst.ID, Name: cst.Name,
			Custom: true, Cached: true,
			InUse:       inUseID(installs, cst.ID),
			Description: cst.Description,
		})
	}
	return rows
}

func buildAddonRows(data WizardData, c *cache.Cache, entries []cache.Entry, installs []state.GameInstall) []resourceRow {
	rows := make([]resourceRow, 0, len(data.Addons)+len(data.CustomAddons))
	for _, a := range data.Addons {
		matches := entriesForPrefix(entries, "addon:"+a.ID+":")
		rows = append(rows, resourceRow{
			ID:           a.ID,
			Name:         a.Name,
			Downloadable: a.Installable(),
			Cached:       c.HasAddon(a.ID),
			Size:         sumSize(matches),
			DownloadedAt: latestDownload(matches),
			InUse:        inUseID(installs, a.ID),
			Description:  a.Description,
			addon:        a,
		})
	}
	for _, cst := range data.CustomAddons {
		rows = append(rows, resourceRow{
			ID: cst.ID, Name: cst.Name,
			Custom: true, Cached: true,
			InUse:       inUseID(installs, cst.ID),
			Description: cst.Description,
		})
	}
	return rows
}

// reshadeFlavorDescription says what the two builds differ in, which is
// the whole reason the browser splits them into two panes.
func reshadeFlavorDescription(addon bool) string {
	if addon {
		return "add-on build: can load .addon32/.addon64 add-ons, and is detectable by anti-cheat"
	}
	return "standard build: effects only, no add-on support"
}

func inUseReShade(installs []state.GameInstall, version string, addon bool) bool {
	flavor := "normal"
	if addon {
		flavor = "addon"
	}
	for _, gi := range installs {
		if gi.Install.ReShade.Version == version && gi.Install.ReShade.Flavor == flavor {
			return true
		}
	}
	return false
}

// inUseID reports whether any recorded install references id. Custom
// content shares a package/add-on's own selection list in the wizard, so
// its id can land in either field; Custom is checked too for whatever
// records it.
func inUseID(installs []state.GameInstall, id string) bool {
	for _, gi := range installs {
		if slices.Contains(gi.Install.Packages, id) ||
			slices.Contains(gi.Install.Addons, id) ||
			slices.Contains(gi.Install.Custom, id) {
			return true
		}
	}
	return false
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

// ResourcesScreen browses everything ReShade needs — versions, effect
// packages and add-ons, including anything supplied as custom content —
// and shows which are already cached, their size, and whether any
// recorded install anywhere actually uses them. Replaces the plain cache
// list: browsing and managing what is cached are the same view now, since
// "is it cached" is just one property of "what's available", not a
// separate list to cross-reference by hand.
type ResourcesScreen struct {
	keys KeyMap
	deps Deps

	loading     bool
	loadErr     string
	downloading bool
	refreshing  bool

	// panes holds every row the catalog and cache offer; visible() is what
	// the current shortlist setting shows of it, so toggling the shortlist
	// never needs a reload.
	panes   [paneCount][]resourceRow
	cursors [paneCount]cursorList
	focus   resourcePane
	showAll bool

	total   int64
	free    int64
	freeErr error
}

// NewResourcesScreen returns the resources browser, which loads on Init.
func NewResourcesScreen(deps Deps) *ResourcesScreen {
	return &ResourcesScreen{keys: DefaultKeyMap(), deps: deps, loading: true}
}

// Init implements Screen.
func (s *ResourcesScreen) Init() tea.Cmd { return s.load() }

func (s *ResourcesScreen) load() tea.Cmd {
	deps := s.deps
	return func() tea.Msg { return loadResources(deps) }
}

// Title implements Screen.
func (s *ResourcesScreen) Title() string {
	if s.loading {
		return "resources — loading…"
	}
	return "resources — " + humanSize(s.total)
}

var (
	resourceDownloadBinding = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "download"))
	resourceDeleteBinding   = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete"))
	resourceCleanBinding    = key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "clear cache"))
	resourceRefreshBinding  = key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh package"))
	resourcePaneLeft        = key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "prev pane"))
	resourcePaneRight       = key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "next pane"))
	resourceShowAllBinding  = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "show all"))
)

// KeyBindings implements Screen.
func (s *ResourcesScreen) KeyBindings() []key.Binding {
	return []key.Binding{
		s.keys.Up, s.keys.Down, resourcePaneLeft, resourcePaneRight,
		resourceDownloadBinding, resourceDeleteBinding, resourceCleanBinding,
		resourceRefreshBinding, resourceShowAllBinding, s.keys.Back,
	}
}

// HandleBack implements backHandler only so this screen can be reached
// from Games and returned from with the ordinary pop; it always defers.
func (s *ResourcesScreen) HandleBack() (Screen, tea.Cmd, bool) { return s, nil, false }

// Update implements Screen.
func (s *ResourcesScreen) Update(msg tea.Msg, env Env) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case resourcesLoadedMsg:
		s.loading = false
		s.downloading = false
		s.refreshing = false
		if msg.err != nil {
			s.loadErr = msg.err.Error()
			return s, ReportError(msg.err)
		}
		s.loadErr = ""
		s.panes = msg.panes
		s.syncCursors()
		s.total, s.free, s.freeErr = msg.total, msg.free, msg.freeErr
		return s, nil

	case resourceActionMsg:
		s.downloading = false
		s.refreshing = false
		return s, ReportError(msg.err)

	case tea.KeyPressMsg:
		return s.handleKey(msg, env)
	}
	return s, nil
}

func (s *ResourcesScreen) handleKey(msg tea.KeyPressMsg, env Env) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, resourcePaneLeft):
		s.focus = (s.focus - 1 + paneCount) % paneCount
	case key.Matches(msg, resourcePaneRight):
		s.focus = (s.focus + 1) % paneCount
	case key.Matches(msg, s.keys.Up):
		s.cursors[s.focus].up()
	case key.Matches(msg, s.keys.Down):
		s.cursors[s.focus].down()
	case key.Matches(msg, resourceDownloadBinding):
		return s.startDownload()
	case key.Matches(msg, resourceDeleteBinding):
		return s.confirmDelete()
	case key.Matches(msg, resourceCleanBinding):
		return s.confirmClean()
	case key.Matches(msg, resourceRefreshBinding):
		return s.startRefresh()
	case key.Matches(msg, resourceShowAllBinding):
		s.showAll = !s.showAll
		s.syncCursors()
	}
	return s, nil
}

// visible is what pane p currently shows: everything, or the curated
// shortlist plus whatever the user has a relationship with already —
// downloaded, in use, or their own custom content. The ReShade panes are
// never filtered: their rows are versions, and there is no such thing as
// an obscure one.
func (s *ResourcesScreen) visible(p resourcePane) []resourceRow {
	shortlist := curatedPackages
	switch {
	case s.showAll, p == paneReShadeNormal, p == paneReShadeAddon:
		return s.panes[p]
	case p == paneAddons:
		shortlist = curatedAddons
	}

	out := make([]resourceRow, 0, len(s.panes[p]))
	for _, r := range s.panes[p] {
		if keepInShortlist(r.ID, shortlist, r.Cached || r.InUse || r.Custom) {
			out = append(out, r)
		}
	}
	return out
}

// syncCursors re-clamps every pane's cursor to what that pane now shows,
// after a load or a change of shortlist setting.
func (s *ResourcesScreen) syncCursors() {
	for p := resourcePane(0); p < paneCount; p++ {
		s.cursors[p].setCount(len(s.visible(p)))
	}
}

func (s *ResourcesScreen) selectedRow() (resourceRow, bool) {
	rows := s.visible(s.focus)
	i := s.cursors[s.focus].Cursor()
	if i < 0 || i >= len(rows) {
		return resourceRow{}, false
	}
	return rows[i], true
}

// startDownload fetches the highlighted item ahead of any install. A
// no-op when it is already cached, is custom content (nothing to fetch),
// or has nothing installable (a manual-only add-on).
func (s *ResourcesScreen) startDownload() (Screen, tea.Cmd) {
	row, ok := s.selectedRow()
	if !ok || row.Cached || row.Custom || !row.Downloadable || s.downloading {
		return s, nil
	}
	s.downloading = true
	pane := s.focus
	deps := s.deps

	return s, func() tea.Msg {
		var err error
		switch pane {
		case paneReShadeNormal, paneReShadeAddon:
			_, err = deps.Cache.EnsureReShade(context.Background(), row.reshadeVersion, row.reshadeAddon, nil)
		case panePackages:
			_, err = deps.Cache.EnsurePackage(context.Background(), row.pkg, nil)
		case paneAddons:
			err = ensureAddonAllArches(deps.Cache, row.addon)
		}
		if err != nil {
			return resourceActionMsg{err: err}
		}
		return loadResources(deps)
	}
}

// ensureAddonAllArches downloads whichever architectures an add-on
// actually publishes, so it is ready for either kind of game regardless of
// which one is later installed into. An add-on that only ships one
// architecture-neutral build is fetched once, not duplicated per arch.
func ensureAddonAllArches(c *cache.Cache, a catalog.Addon) error {
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
		if _, err := c.EnsureAddon(context.Background(), a, arch, nil); err != nil {
			lastErr = err
		}
	}
	if tried == 0 {
		return fmt.Errorf("add-on %s publishes nothing installable", a.ID)
	}
	return lastErr
}

// confirmDelete removes every cached entry for the highlighted item —
// there can be more than one for a package or add-on (package cache
// keys carry a download date and content hash, so re-fetching an updated
// branch keeps the old one around until it is cleaned up).
func (s *ResourcesScreen) confirmDelete() (Screen, tea.Cmd) {
	row, ok := s.selectedRow()
	if !ok || !row.Cached || row.Custom {
		return s, nil
	}
	pane := s.focus
	deps := s.deps

	detail := "Removes it from disk. It will be downloaded again if something needs it."
	if row.InUse {
		detail = "⚠ At least one recorded install still uses this — deleting it will not touch that install, " +
			"but yarm would need to re-download it to update or reinstall from it later. " + detail
	}

	action := func() tea.Msg {
		var prefix string
		switch pane {
		case paneReShadeNormal, paneReShadeAddon:
			prefix = row.ID
		case panePackages:
			prefix = "package:" + row.ID + ":"
		case paneAddons:
			prefix = "addon:" + row.ID + ":"
		}
		entries, err := deps.Cache.List(cache.SortByName, false)
		if err != nil {
			return resourceActionMsg{err: err}
		}
		for _, e := range entries {
			if e.ID != prefix && !strings.HasPrefix(e.ID, prefix) {
				continue
			}
			if err := deps.Cache.Delete(e.ID); err != nil {
				return resourceActionMsg{err: err}
			}
		}
		return loadResources(deps)
	}

	return s, Confirm("Delete "+row.Name+"?", detail, action)
}

// confirmClean empties the download cache. Deliberately a separate
// binding from x on a single row rather than a "delete all" mode: this is
// the reclaim-the-disk action, it is the one thing on this screen that
// touches rows the cursor is nowhere near, and undoing it means
// downloading everything again.
func (s *ResourcesScreen) confirmClean() (Screen, tea.Cmd) {
	items, size, inUse := s.cachedTotals()
	if items == 0 {
		return s, SetStatus("nothing cached to clear")
	}
	deps := s.deps

	// What the user is actually risking is stated first, because the rest
	// of it is reassurance and reassurance read first is not read at all.
	detail := ""
	if inUse > 0 {
		detail = fmt.Sprintf(
			"⚠ %d of them back a recorded install — those installs keep working, but updating or "+
				"reinstalling from them means downloading again. ", inUse)
	}
	detail += "Frees about " + humanSize(size) + ". Games keep every file yarm copied into them; " +
		"only the downloads go, and they come back the next time something needs them. " +
		"Custom content lives outside the cache and is untouched."

	return s, Confirm(
		fmt.Sprintf("Clear the download cache — %d item(s), %s?", items, humanSize(size)),
		detail,
		func() tea.Msg {
			if _, err := deps.Cache.Clean(); err != nil {
				return resourceActionMsg{err: err}
			}
			return loadResources(deps)
		})
}

// cachedTotals counts what a clean would remove: every cached row across
// every pane, custom content excluded because the cache does not hold it.
// The figure is a floor — Clean also drops partial downloads, which are
// not indexed and so appear in no pane.
func (s *ResourcesScreen) cachedTotals() (items int, size int64, inUse int) {
	for p := resourcePane(0); p < paneCount; p++ {
		for _, r := range s.panes[p] {
			if !r.Cached || r.Custom {
				continue
			}
			items++
			size += r.Size
			if r.InUse {
				inUse++
			}
		}
	}
	return items, size, inUse
}

// startRefresh re-fetches a cached package, picking up any upstream
// change even within the same day — only packages carry enough of their
// own metadata (package.json) to do this without the live catalog.
func (s *ResourcesScreen) startRefresh() (Screen, tea.Cmd) {
	if s.focus != panePackages || s.refreshing {
		return s, nil
	}
	row, ok := s.selectedRow()
	if !ok || row.Custom || !row.Cached {
		return s, nil
	}
	s.refreshing = true
	deps := s.deps
	pkg := row.pkg

	return s, func() tea.Msg {
		if _, err := deps.Cache.EnsurePackage(context.Background(), pkg, nil); err != nil {
			return resourceActionMsg{err: fmt.Errorf("refresh %s: %w", pkg.Name, err)}
		}
		return loadResources(deps)
	}
}

// View implements Screen.
func (s *ResourcesScreen) View(env Env) string {
	if s.loading {
		return env.Styles.Faint.Render("loading catalog and cache data…")
	}
	if s.loadErr != "" && s.total == 0 {
		return env.Styles.Bad.Render(s.loadErr)
	}

	header := "total: " + humanSize(s.total)
	if s.freeErr == nil {
		header += "   free: " + humanSize(s.free)
	}
	if s.downloading {
		header += "   downloading…"
	}
	if s.refreshing {
		header += "   refreshing…"
	}

	// The focused row's own description, which the panes are far too
	// narrow to carry: at four columns a pane has about twenty characters,
	// which is a name and nothing else.
	detail := s.renderDetail(env)

	height := env.Height - 5 - countLines(detail)
	if height < 5 {
		height = 5
	}

	// Four panes need at least a usable 20 columns each, or they wrap and
	// overlap rather than read as columns — a 60-column split, exactly
	// what the terminal itself offers as `tmux split-window -h`, doesn't
	// have that. Below the point where every pane can still have its
	// floor, show only the focused one, full width, with a tab strip so
	// the other three stay reachable instead of just disappearing.
	const minPaneWidth = 20
	const minGutter = 2
	if env.Width < int(paneCount)*(minPaneWidth+minGutter) {
		return env.Styles.Faint.Render(header) + "\n\n" +
			s.renderTabStrip(env) + "\n" +
			s.renderPane(s.focus, env.Width-2, height, env) + "\n" + detail
	}

	paneWidth := env.Width/int(paneCount) - minGutter
	cols := make([]string, 0, paneCount)
	for p := resourcePane(0); p < paneCount; p++ {
		cols = append(cols, s.renderPane(p, paneWidth, height, env))
	}

	return env.Styles.Faint.Render(header) + "\n\n" +
		lipgloss.JoinHorizontal(lipgloss.Top, cols...) + "\n" + detail
}

// renderDetail is the block under the panes describing the focused row:
// what it is, and anything about it the row itself could not fit. Empty
// when there is no row to describe, so an empty pane costs no lines.
func (s *ResourcesScreen) renderDetail(env Env) string {
	r, ok := s.selectedRow()
	if !ok {
		return ""
	}

	var b strings.Builder
	b.WriteString(env.Styles.Subtitle.Render(clipTail(r.Name, env.Width)))
	b.WriteString("\n")
	if r.Description != "" {
		b.WriteString(env.Styles.Faint.Render(wrap(r.Description, env.Width-1)))
		b.WriteString("\n")
	}
	if note := resourceSource(r); note != "" {
		b.WriteString(env.Styles.Faint.Render(clipTail(note, env.Width)))
		b.WriteString("\n")
	}
	return b.String()
}

// resourceSource is the line under a description saying where the thing
// comes from, or why yarm cannot fetch it.
func resourceSource(r resourceRow) string {
	switch {
	case r.Custom:
		return "your own content, from the custom folder"
	case r.addon.ID != "" && !r.addon.Installable():
		// The reason a row is greyed here is the same as in the wizard:
		// upstream lists no download for it at all.
		if r.addon.RepositoryURL != "" {
			return "manual install only: " + r.addon.RepositoryURL
		}
		return "manual install only"
	case r.addon.RepositoryURL != "":
		return r.addon.RepositoryURL
	case r.pkg.RepositoryURL != "":
		return r.pkg.RepositoryURL
	default:
		return ""
	}
}

// renderTabStrip lists every pane so the narrow, single-pane layout does
// not hide that the other three still exist and are one ←/→ away.
func (s *ResourcesScreen) renderTabStrip(env Env) string {
	var parts []string
	for p := resourcePane(0); p < paneCount; p++ {
		label := p.label()
		if p == s.focus {
			parts = append(parts, env.Styles.Selected.Render("▸ "+label))
		} else {
			parts = append(parts, env.Styles.Faint.Render(label))
		}
	}
	return strings.Join(parts, "   ")
}

func (s *ResourcesScreen) renderPane(p resourcePane, width, height int, env Env) string {
	var b strings.Builder
	rows := s.visible(p)

	// Styles.Panel spends four columns on its own border and padding, and
	// lipgloss wraps rather than clips what does not fit — which breaks
	// the box open rather than shortening a name.
	inner := width - 4
	if inner < 8 {
		inner = 8
	}

	// A pane showing less than it holds has to say so, or a missing pack
	// reads as a broken catalog rather than a filtered list.
	label := p.label()
	if hidden := len(s.panes[p]) - len(rows); hidden > 0 {
		label += fmt.Sprintf(" (%d of %d)", len(rows), len(s.panes[p]))
	}
	if p == s.focus {
		b.WriteString(env.Styles.Selected.Render(clipTail("▸ "+label, inner)))
	} else {
		b.WriteString(env.Styles.Subtitle.Render(clipTail("  "+label, inner)))
	}
	b.WriteString("\n")

	if len(rows) == 0 {
		b.WriteString(env.Styles.Faint.Render("  (none)"))
	}

	cursor := s.cursors[p].Cursor()
	visible := height - 1
	start, end := scrollWindow(len(rows), cursor, visible)
	for i := start; i < end; i++ {
		b.WriteString(s.renderRow(rows[i], i == cursor && p == s.focus, inner, env))
		b.WriteString("\n")
	}

	return env.Styles.Panel.Width(width).Height(height).Render(b.String())
}

func (s *ResourcesScreen) renderRow(r resourceRow, selected bool, width int, env Env) string {
	marker := "  "
	switch {
	case r.Custom:
		marker = "★ "
	case r.Cached:
		marker = "✓ "
	}

	// The marker and color already say cached/custom/not-yet — "not
	// downloaded" text on every unfetched row was just repeating what the
	// dimmed color already conveys. Only facts the color can't carry
	// (size, custom, in use) get a tag.
	var tags []string
	switch {
	case r.Custom:
		tags = append(tags, "custom")
	case r.Cached:
		tags = append(tags, humanSize(r.Size))
	}
	if r.InUse {
		tags = append(tags, "in use")
	}

	tagsText := ""
	if len(tags) > 0 {
		tagsText = "  (" + strings.Join(tags, ", ") + ")"
	}
	nameWidth := width - 2 - lipgloss.Width(tagsText)
	if nameWidth < 6 {
		nameWidth = 6
	}
	// clipTail, not truncate: truncate keeps a string's tail, which is
	// right for a path and wrong for a name — "…tFX by CeeJay.dk" hides
	// the half that identifies it.
	line := marker + clipTail(r.Name, nameWidth) + tagsText

	switch {
	case selected:
		return env.Styles.Selected.Render(line)
	case r.Custom:
		return env.Styles.Accent.Render(line)
	case r.Cached:
		return env.Styles.Good.Render(line)
	default:
		return env.Styles.Faint.Render(line)
	}
}

// humanSize formats a byte count for display.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// scrollWindow returns the [start, end) slice of a height-sized list that
// keeps cursor visible, centering it when the full list does not fit.
func scrollWindow(count, cursor, height int) (start, end int) {
	if height <= 0 {
		return 0, 0
	}
	if count <= height {
		return 0, count
	}
	if cursor < 0 {
		cursor = 0
	}
	start = cursor - height/2
	if start < 0 {
		start = 0
	}
	if start+height > count {
		start = count - height
	}
	return start, start + height
}
