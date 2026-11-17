package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

// renodxCatalogAddonID is RenoDX's own entry in crosire's add-on catalog,
// which carries no download URL and so renders as manual-only.
const renodxCatalogAddonID = "renodx-by-shortfuse"

// autoHDRAddonID is the catalog add-on that conflicts with RenoDX.
const autoHDRAddonID = "autohdr-by-endlesslyflowering-original-by-majorpainthecactus"

// maxVersions is how many ReShade versions are offered.
const maxVersions = 10

// Row is one selectable line of a list, with whether it is ticked.
type Row struct {
	Item
	Selected bool
}

// Session is one game's edit: everything the user has chosen for an
// install or an edit — folders, ReShade version and build, proxy DLL,
// shader packs, add-ons and the RenoDX mod — and what follows from it: the
// install requests, what still has to be downloaded, what is in the way and
// how the result differs from what is installed.
//
// It holds no UI state. A window reads and changes it through its methods,
// so the tabs and the dock can never disagree about what will happen.
type Session struct {
	Entry GameEntry
	Data  WizardData
	Deps  Deps

	// Folders is every folder this run could apply to, and which of them
	// are checked.
	Folders FolderPicker
	// Exe is the reference folder's own executable: the one every other
	// choice (build, version, DLL, packages) is shown and diffed against. A
	// folder added alongside it gets the same answers, just its own
	// executable in the request the batch runs.
	Exe Executable
	// Existing is the install already recorded for the reference folder, if
	// any. Every choice starts from it instead of the configured defaults.
	Existing *state.Install

	Flavor  install.Flavor
	Version string
	DLL     string
	// RenoDX is the chosen mod id, "" for none.
	RenoDX string
	// Overwrite and Backup are the answers to what is already in the way.
	// Both start on: ReShade will not load behind another file holding its
	// DLL name, and backups being on is what makes replacing safe.
	Overwrite bool
	Backup    bool

	packages map[string]bool
	addons   map[string]bool

	pkgsFull   []Item
	addonsFull []Item
	renodxFull []Item

	versions []catalog.Version

	gen     uint64
	missing struct {
		gen uint64
		val []string
		ok  bool
	}
	needed struct {
		gen uint64
		val install.Needed
		ok  bool
	}
	managedSigs map[string]managedSig
}

// NewSession starts an edit of exe's folder. targets is every folder the
// calling action found eligible; exe's own folder starts out checked and
// everything else about the request is free to reach into the rest. data is
// the loaded catalog.
func NewSession(entry GameEntry, exe Executable, targets []FolderGroup, deps Deps, data WizardData) *Session {
	existing := exe.Installed

	// The normal build unless something says otherwise: the add-on build is
	// the one anti-cheat can detect, so it is opted into, never defaulted
	// into.
	flavor := install.FlavorNormal
	switch {
	case existing != nil && existing.ReShade.Flavor != "":
		flavor = install.Flavor(existing.ReShade.Flavor)
	case existing == nil && deps.Defaults.ReshadeFlavor == string(install.FlavorAddon):
		flavor = install.FlavorAddon
	}

	// exe may not be the reference folder's PrimaryExe guess — an existing
	// install names its own exe. Recording it keeps Ops (which reads the
	// same map through ExeFor) from disagreeing with the answers built
	// around exe.
	folders := NewFolderPicker(targets, []string{install.ExeDir(exe.Path)}, true)
	folders.chosen[install.ExeDir(exe.Path)] = exe.Path

	s := &Session{
		Entry:     entry,
		Data:      data,
		Deps:      deps,
		Folders:   folders,
		Exe:       exe,
		Existing:  existing,
		Flavor:    flavor,
		Overwrite: true,
		Backup:    true,
		packages:  map[string]bool{},
		addons:    map[string]bool{},
	}

	existingVersion := ""
	if existing != nil {
		existingVersion = existing.ReShade.Version
	}
	s.versions = capVersions(data.Versions, existingVersion)

	for _, it := range s.fullPackages() {
		if it.Required {
			s.packages[it.ID] = true
		}
	}
	switch {
	case existing != nil:
		for _, id := range slices.Concat(existing.Packages, existing.Custom) {
			if s.hasPackage(id) {
				s.packages[id] = true
			}
		}
		for _, id := range slices.Concat(existing.Addons, existing.Custom) {
			if s.hasAddon(id) {
				s.addons[id] = true
			}
		}
		s.RenoDX = existing.RenoDX
		s.Version = s.versionOrLatest(existing.ReShade.Version)
	default:
		for _, id := range deps.Defaults.Packages {
			if resolved := catalog.ResolveAlias(id); resolved != "" && s.hasPackage(resolved) {
				s.packages[resolved] = true
			}
		}
		s.Version = s.latestVersion()
	}
	s.DLL = s.recommendedDLL()
	return s
}

// capVersions limits the offered list to the most recent ten, plus the
// existing install's version if editing one that has since aged out of that
// window — otherwise editing an old install would silently jump to latest
// instead of showing what is actually there.
func capVersions(versions []catalog.Version, existing string) []catalog.Version {
	if len(versions) > maxVersions {
		// A full slice expression caps capacity too, so the append below
		// allocates a new backing array instead of overwriting versions the
		// caller's slice still holds beyond the cutoff.
		versions = versions[:maxVersions:maxVersions]
	}
	if existing == "" || existing == install.AdoptedVersion {
		return versions
	}
	for _, v := range versions {
		if v.Version == existing {
			return versions
		}
	}
	return append(versions, catalog.Version{Version: existing})
}

// Versions is the list of ReShade versions on offer, newest first.
func (s *Session) Versions() []catalog.Version { return s.versions }

func (s *Session) latestVersion() string {
	for _, v := range s.versions {
		if v.Latest {
			return v.Version
		}
	}
	if len(s.versions) > 0 {
		return s.versions[0].Version
	}
	return ""
}

// versionOrLatest returns version when it is on offer, else the latest (an
// adopted install's version is recorded as "unknown (adopted)", which never
// matches).
func (s *Session) versionOrLatest(version string) string {
	for _, v := range s.versions {
		if v.Version == version {
			return version
		}
	}
	return s.latestVersion()
}

// recommendedDLL is the recorded DLL when editing an existing install,
// otherwise a guess from the executable's API, or dxgi.dll when there is no
// safe recommendation.
func (s *Session) recommendedDLL() string {
	rec := s.Exe.API.RecommendedDLL()
	if s.Existing != nil {
		rec = s.Existing.ReShade.DLL
	}
	for _, o := range dllOptions {
		if o.Name == rec {
			return rec
		}
	}
	return dllOptions[0].Name
}

// DLLOption is one choice of proxy DLL.
type DLLOption struct {
	Name string
	For  string
}

// DLLOptions are the proxy DLL names ReShade can be installed as, newest
// API first.
func DLLOptions() []DLLOption {
	out := make([]DLLOption, len(dllOptions))
	for i, o := range dllOptions {
		out[i] = DLLOption(o)
	}
	return out
}

// RecommendedDLL is the proxy DLL suggested for the reference executable,
// or "" when its API gives no safe answer.
func (s *Session) RecommendedDLL() string { return s.Exe.API.RecommendedDLL() }

// touch invalidates everything derived from the choices.
func (s *Session) touch() { s.gen++ }

// Refresh invalidates the derived answers (what is missing, what is in the
// way) so they are read from disk again — for when the game folder or the
// cache may have changed underneath.
func (s *Session) Refresh() {
	s.touch()
	s.managedSigs = nil
}

// SetFlavor chooses the normal or add-on build.
func (s *Session) SetFlavor(f install.Flavor) {
	if s.Flavor != f {
		s.Flavor = f
		s.touch()
	}
}

// SetVersion chooses a ReShade version.
func (s *Session) SetVersion(v string) {
	if s.Version != v {
		s.Version = v
		s.touch()
	}
}

// SetDLL chooses the proxy DLL name.
func (s *Session) SetDLL(name string) {
	if s.DLL != name {
		s.DLL = name
		s.touch()
	}
}

// SetOverwrite and SetBackup answer what to do with files in the way.
func (s *Session) SetOverwrite(on bool) { s.Overwrite = on; s.touch() }

// SetBackup chooses whether replaced files are saved first.
func (s *Session) SetBackup(on bool) { s.Backup = on; s.touch() }

// ToggleFolder checks or unchecks a game folder.
func (s *Session) ToggleFolder(dir string) {
	s.Folders.Toggle(dir)
	s.touch()
}

// CycleExe switches which executable stands for a folder with several.
func (s *Session) CycleExe(dir string) {
	s.Folders.CycleExe(dir)
	s.touch()
}

// ---- shader packs and add-ons ----

func (s *Session) fullPackages() []Item {
	if s.pkgsFull == nil {
		s.pkgsFull = s.Data.PackagesWithCustom(s.Deps.CacheStatus)
	}
	return s.pkgsFull
}

func (s *Session) fullAddons() []Item {
	if s.addonsFull == nil {
		s.addonsFull = s.buildAddonRows()
	}
	return s.addonsFull
}

// buildAddonRows assembles the add-on list: catalog rows with the RenoDX
// retargeting and the utility mods folded in.
func (s *Session) buildAddonRows() []Item {
	items := s.Data.AddonsWithCustom(s.Deps.CacheStatus)
	// RenoDX is in the catalog with no download URL, so it renders as
	// manual-only — but yarm does install it, from the RenoDX tab.
	for i := range items {
		if items[i].ID == renodxCatalogAddonID {
			items[i].DisabledNote = "installed from the RenoDX tab"
		}
	}
	// RenoDX's utility mods (FPS Limiter, DLSS Fix) land here rather than
	// on the RenoDX tab: unlike a per-game mod, more than one can be active
	// at once, so they behave like ordinary add-ons.
	for _, m := range s.Data.RenoDX {
		if m.Utility {
			items = append(items, s.renodxItem(m, false))
		}
	}
	return items
}

func (s *Session) hasPackage(id string) bool {
	for _, it := range s.fullPackages() {
		if !it.Header && it.ID == id {
			return true
		}
	}
	return false
}

func (s *Session) hasAddon(id string) bool {
	for _, it := range s.fullAddons() {
		if !it.Header && it.ID == id {
			return true
		}
	}
	return false
}

// selection is everything ticked across packs and add-ons, which is what a
// requirement is judged against. Add-ons only count when the build can load
// them.
func (s *Session) selection() map[string]bool {
	sel := make(map[string]bool, len(s.packages)+len(s.addons))
	for id, on := range s.packages {
		if on {
			sel[id] = true
		}
	}
	if s.Flavor.Addon() {
		for id, on := range s.addons {
			if on {
				sel[id] = true
			}
		}
	}
	return sel
}

// catalogIDs is every id the loaded catalog offers, so a requirement can
// only ever select something that exists to download.
func (s *Session) catalogIDs() map[string]bool {
	ids := map[string]bool{}
	for _, it := range slices.Concat(s.fullPackages(), s.fullAddons()) {
		if !it.Header && !it.Disabled {
			ids[it.ID] = true
		}
	}
	return ids
}

// ItemNames maps every catalog id to its display name.
func (s *Session) ItemNames() map[string]string {
	names := map[string]string{}
	for _, it := range slices.Concat(s.fullPackages(), s.fullAddons()) {
		names[it.ID] = it.Name
	}
	return names
}

// annotate marks rows with what they still need, and rows that are only
// there because something else needs them. Both are the same fact seen from
// either end, and without them a pack appearing ticked that the user never
// ticked would look like a bug.
func (s *Session) annotate(items []Item) []Item {
	sel := s.selection()
	wantedBy := map[string][]string{}
	for _, src := range slices.Concat(s.fullPackages(), s.fullAddons()) {
		if !sel[src.ID] {
			continue
		}
		for _, r := range requirementsFor(src.ID) {
			for _, dep := range r.AnyOf {
				if sel[dep] {
					wantedBy[dep] = append(wantedBy[dep], src.Name)
				}
			}
		}
	}

	out := make([]Item, len(items))
	copy(out, items)
	for i := range out {
		if note, unmet := requirementNote(out[i].ID, sel); unmet && sel[out[i].ID] {
			out[i].Note, out[i].NoteWarn = note, true
			continue
		}
		if who := wantedBy[out[i].ID]; len(who) > 0 {
			out[i].Note = "required by " + strings.Join(who, ", ")
		}
	}
	return out
}

func (s *Session) rows(items []Item, shortlist map[string]bool, all bool, sel map[string]bool) []Row {
	items = curate(s.annotate(items), shortlist, all, sel)
	out := make([]Row, 0, len(items))
	for _, it := range items {
		out = append(out, Row{Item: it, Selected: !it.Header && sel[it.ID]})
	}
	return out
}

// PackageRows is the shader pack list: the curated shortlist, or everything
// when all is set. A ticked row is always kept visible.
func (s *Session) PackageRows(all bool) []Row {
	return s.rows(s.fullPackages(), curatedPackages, all, s.packages)
}

// AddonRows is the add-on list, shortlisted the same way.
func (s *Session) AddonRows(all bool) []Row {
	return s.rows(s.fullAddons(), curatedAddons, all, s.addons)
}

// PackageTotal and AddonTotal count every selectable row, shortlisted or not.
func (s *Session) PackageTotal() int { return countSelectable(s.fullPackages()) }

// AddonTotal counts every selectable add-on row.
func (s *Session) AddonTotal() int { return countSelectable(s.fullAddons()) }

func countSelectable(items []Item) int {
	n := 0
	for _, it := range items {
		if !it.Header {
			n++
		}
	}
	return n
}

// TogglePackage ticks or unticks a shader pack. Turning one on pulls in what
// it needs and returns the names of what it added; turning one off leaves a
// dependency alone, since it may be wanted on its own and the review says if
// something is now missing.
func (s *Session) TogglePackage(id string) (pulled []string) {
	it, ok := s.findItem(s.fullPackages(), id)
	if !ok || it.Header || it.Disabled || it.Required {
		return nil
	}
	s.packages[id] = !s.packages[id]
	s.touch()
	if s.packages[id] {
		pulled = s.satisfied(id)
	}
	return pulled
}

// ToggleAddon ticks or unticks an add-on, with the same dependency
// behavior as TogglePackage.
func (s *Session) ToggleAddon(id string) (pulled []string) {
	it, ok := s.findItem(s.fullAddons(), id)
	if !ok || it.Header || it.Disabled || it.Required {
		return nil
	}
	s.addons[id] = !s.addons[id]
	s.touch()
	if s.addons[id] {
		pulled = s.satisfied(id)
	}
	return pulled
}

// satisfied selects what id needs and returns the names of the packs it
// turned on.
func (s *Session) satisfied(id string) []string {
	before := cloneSet(s.packages)
	satisfy(id, s.packages, s.catalogIDs())
	names := s.ItemNames()
	var out []string
	for dep, on := range s.packages {
		if on && !before[dep] {
			n := names[dep]
			if n == "" {
				n = dep
			}
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

func cloneSet(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *Session) findItem(items []Item, id string) (Item, bool) {
	for _, it := range items {
		if !it.Header && it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// PackageSelected and AddonSelected report whether an id is ticked.
func (s *Session) PackageSelected(id string) bool { return s.packages[id] }

// AddonSelected reports whether an add-on id is ticked.
func (s *Session) AddonSelected(id string) bool { return s.addons[id] }

// SelectedPackages are the ticked catalog shader pack ids, in list order.
// Custom shader folders are not included; see SelectedCustom.
func (s *Session) SelectedPackages() []string {
	return s.selected(s.fullPackages(), s.packages, false)
}

// SelectedAddons are the ticked catalog add-on ids, or none when the build
// does not support add-ons at all.
func (s *Session) SelectedAddons() []string {
	if !s.Flavor.Addon() {
		return nil
	}
	return s.selected(s.fullAddons(), s.addons, false)
}

// SelectedCustom are the ticked custom shader folders and add-ons.
func (s *Session) SelectedCustom() []string {
	out := s.selected(s.fullPackages(), s.packages, true)
	if s.Flavor.Addon() {
		out = append(out, s.selected(s.fullAddons(), s.addons, true)...)
	}
	return out
}

// selected lists ticked ids in list order, custom ones or catalog ones.
func (s *Session) selected(items []Item, on map[string]bool, custom bool) []string {
	var out []string
	for _, it := range items {
		if it.Header || !on[it.ID] || isCustomID(it.ID) != custom {
			continue
		}
		out = append(out, it.ID)
	}
	return out
}

// ---- RenoDX ----

// matchRenoDX finds the mod upstream says belongs to this game. yarm's Steam
// ids are "steam:<appid>" and RenoDX publishes the same app id, so the match
// is a parse and a compare.
func matchRenoDX(mods []catalog.RenoMod, gameID string) (catalog.RenoMod, bool) {
	rest, ok := strings.CutPrefix(gameID, "steam:")
	if !ok {
		return catalog.RenoMod{}, false
	}
	appID, err := strconv.Atoi(rest)
	if err != nil || appID == 0 {
		return catalog.RenoMod{}, false
	}
	for _, m := range mods {
		if m.SteamAppID == appID {
			return m, true
		}
	}
	return catalog.RenoMod{}, false
}

// renodxItem turns one mod into a row, carrying the reason it cannot be
// installed here when there is one.
func (s *Session) renodxItem(m catalog.RenoMod, matched bool) Item {
	it := Item{
		ID:          m.ID,
		Name:        m.Title,
		Description: m.Description,
		Cached:      s.Deps.CacheStatus != nil && s.Deps.CacheStatus.HasRenoDX(m.ID),
	}
	if it.Description == "" && len(m.Maintainers) > 0 {
		it.Description = "maintained by " + strings.Join(m.Maintainers, ", ")
	}

	switch {
	case m.Unsupported() != "":
		it.Disabled = true
		it.DisabledNote = "yarm does not support " + m.Unsupported() + " yet"
	default:
		if _, ok := m.ArtifactFor(s.Exe.Arch); !ok {
			it.Disabled = true
			it.DisabledNote = "no " + string(s.Exe.Arch) + " build"
		}
	}

	switch {
	case matched:
		it.Note = "matches this game"
	case m.Beta():
		it.Note, it.NoteWarn = "beta", true
	case m.Unverified():
		it.Note = "unverified"
	}
	return it
}

// RenoDXRows is the mod list: the one for this game first, then the rest,
// filtered by a case-insensitive substring of title or id. The matched mod
// is named but never pre-ticked: the add-on build is the one anti-cheat can
// detect, and quietly adding a second injected DLL would contradict that.
func (s *Session) RenoDXRows(query string) []Row {
	if s.renodxFull == nil {
		games := s.Data.RenoDXGames()
		matched, hasMatch := matchRenoDX(games, s.Entry.ID)
		if hasMatch {
			s.renodxFull = append(s.renodxFull, s.renodxItem(matched, true))
		}
		for _, m := range games {
			if hasMatch && m.ID == matched.ID {
				continue
			}
			s.renodxFull = append(s.renodxFull, s.renodxItem(m, false))
		}
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var out []Row
	for _, it := range s.renodxFull {
		if q != "" && !strings.Contains(strings.ToLower(it.Name), q) && !strings.Contains(strings.ToLower(it.ID), q) {
			continue
		}
		out = append(out, Row{Item: it, Selected: it.ID == s.RenoDX})
	}
	return out
}

// HasRenoDX reports whether the RenoDX mod list loaded at all.
func (s *Session) HasRenoDX() bool { return len(s.Data.RenoDX) > 0 }

// RenoDXMatch is the mod upstream publishes for this game, if any.
func (s *Session) RenoDXMatch() (catalog.RenoMod, bool) {
	return matchRenoDX(s.Data.RenoDXGames(), s.Entry.ID)
}

// ChooseRenoDX takes a mod as the answer; "" means none. A mod that cannot
// be installed here is ignored.
func (s *Session) ChooseRenoDX(id string) {
	if id != "" {
		it, ok := s.findItem(slices.Concat(s.RenoDXItems()), id)
		if !ok || it.Disabled {
			return
		}
	}
	s.RenoDX = id
	s.touch()
}

// RenoDXItems is every per-game RenoDX row, unfiltered.
func (s *Session) RenoDXItems() []Item {
	rows := s.RenoDXRows("")
	out := make([]Item, len(rows))
	for i, r := range rows {
		out[i] = r.Item
	}
	return out
}

func (s *Session) renodxMod(id string) (catalog.RenoMod, bool) {
	for _, m := range s.Data.RenoDX {
		if m.ID == id {
			return m, true
		}
	}
	return catalog.RenoMod{}, false
}

// RenoDXChoice is the mod that will be installed: none unless the build can
// load add-ons, the same rule as SelectedAddons — the normal build cannot
// load a RenoDX add-on and Request.Validate rejects a request that tries.
func (s *Session) RenoDXChoice() string {
	if !s.Flavor.Addon() {
		return ""
	}
	return s.RenoDX
}

// RenoDXTitle names the chosen mod.
func (s *Session) RenoDXTitle() string {
	if s.RenoDX == "" {
		return ""
	}
	if m, ok := s.renodxMod(s.RenoDX); ok {
		return m.Title
	}
	return s.RenoDX
}

// versionOK reports whether the chosen ReShade is new enough to load a
// RenoDX add-on at all.
func (s *Session) renodxVersionOK() bool {
	if s.Version == "" {
		return true
	}
	return catalog.CompareVersions(s.Version, catalog.RenoDXMinReShade) >= 0
}

// RenoDXMinReShade is the oldest ReShade that loads a RenoDX add-on.
func RenoDXMinReShade() string { return catalog.RenoDXMinReShade }

// Unmet lists what would stop the current choices from working: a pack that
// needs another, RenoDX with a ReShade too old for it, or RenoDX beside
// AutoHDR. Reported rather than blocked; each is changeable.
func (s *Session) Unmet() []string {
	sel := s.selection()
	ids := slices.Concat(s.SelectedPackages(), s.SelectedAddons())
	out := unmetRequirements(ids, s.ItemNames(), sel)

	if id := s.RenoDXChoice(); id != "" {
		if !s.renodxVersionOK() {
			out = append(out, fmt.Sprintf("RenoDX needs ReShade %s or newer (you chose %s)",
				catalog.RenoDXMinReShade, s.Version))
		}
		// Both replace the game's tone mapping, so running them together
		// gives whichever loads second — not a blend, and not a choice.
		if s.addons[autoHDRAddonID] {
			out = append(out, "RenoDX and AutoHDR both replace the game's tone mapping; pick one")
		}
	}
	if !s.renodxVersionOK() {
		for _, id := range s.SelectedAddons() {
			if m, ok := s.renodxMod(id); ok && m.Utility {
				out = append(out, fmt.Sprintf("%s needs ReShade %s or newer (you chose %s)",
					m.Title, catalog.RenoDXMinReShade, s.Version))
			}
		}
	}
	return out
}

// ---- the request ----

// Request builds the install request for exe from the shared choices. ok is
// false when something required is missing.
func (s *Session) Request(exe Executable) (install.Request, bool) {
	if s.Version == "" || s.DLL == "" {
		return install.Request{}, false
	}
	return install.Request{
		Game:     s.Entry.Game,
		Exe:      exe.Executable,
		Version:  s.Version,
		Flavor:   s.Flavor,
		DLLName:  s.DLL,
		Packages: s.SelectedPackages(),
		// Selections made while add-ons were on offer must not survive a
		// switch back to the normal build: normal can't load them at all.
		Addons:    s.SelectedAddons(),
		Custom:    s.SelectedCustom(),
		RenoDX:    s.RenoDXChoice(),
		Overwrite: s.Overwrite,
		NoBackup:  !s.Backup,
	}, true
}

// Ops builds one Op per folder that is checked: an install request for
// each, from the shared choices but that folder's own executable — the
// reason a 32- and a 64-bit tree of the same game can be set up in one pass
// — plus an uninstall for each folder that was unchecked and already had an
// install, since that is how an edit moves an install between folders.
func (s *Session) Ops() ([]Op, bool) {
	groups := s.Folders.SelectedGroups()
	ops := make([]Op, 0, len(groups)+len(s.Folders.Removed()))
	for _, g := range groups {
		req, ok := s.Request(s.Folders.ExeFor(g))
		if !ok {
			return nil, false
		}
		// Overwrite starts on but is only meaningful when Preflight sees a
		// foreign file here. Where it saw none, the user was never asked,
		// so the planner must not replace anything Preflight does not stat
		// (a hand-placed shader) on the strength of that default.
		if !hasForeign(install.Preflight(req, s.Existing)) {
			req.Overwrite = false
		}
		ops = append(ops, Op{Dir: g.Dir, Install: &req})
	}
	for _, g := range s.Folders.Removed() {
		target, ok := g.InstalledExe()
		if !ok {
			continue
		}
		ops = append(ops, Op{
			Dir:       g.Dir,
			Uninstall: &install.UninstallRequest{GameID: s.Entry.ID, Exe: target.Path},
		})
	}
	return ops, len(ops) > 0
}

// hasForeign reports whether any conflict is a file that is not yarm's own.
func hasForeign(cs []install.Conflict) bool {
	for _, c := range cs {
		if c.Kind == install.ConflictForeign {
			return true
		}
	}
	return false
}

// HasForeign reports whether anything the install would write is already
// held by a file that is not yarm's.
func (s *Session) HasForeign() bool { return hasForeign(s.Conflicts()) }

// ---- what is missing, what is in the way ----

// Missing names what applying would still have to download.
func (s *Session) Missing() []string {
	if s.missing.ok && s.missing.gen == s.gen {
		return s.missing.val
	}
	if s.Version == "" {
		return nil
	}
	needed := s.Needed()
	val := describeMissing(s.Deps.CacheStatus, needed, s.Version, s.Flavor.Addon(),
		s.SelectedPackages(), s.SelectedAddons(), s.Data.isRenoDXUtility,
		s.RenoDXChoice(), s.Exe.Arch, true)
	s.missing.gen, s.missing.val, s.missing.ok = s.gen, val, true
	return val
}

// Needed works out which artifact groups applying would actually write
// through: an edit whose installed files are unchanged resolves from the
// manifest, so those groups drop out. A fresh install needs everything.
func (s *Session) Needed() install.Needed {
	if s.needed.ok && s.needed.gen == s.gen {
		return s.needed.val
	}
	req, ok := s.Request(s.Exe)
	if !ok {
		return install.Needed{Packages: map[string]bool{}, Addons: map[string]bool{}}
	}
	val := install.NeededArtifacts(req, s.Existing)
	s.needed.gen, s.needed.val, s.needed.ok = s.gen, val, true
	return val
}

// Conflicts is what already sits where this install would write, minus the
// managed files the plan will leave alone. Cheap (a handful of stats, and a
// hash only when a managed file may have changed), so it is not cached
// across Refresh.
func (s *Session) Conflicts() []install.Conflict {
	req, ok := s.Request(s.Exe)
	if !ok {
		return nil
	}
	cs := install.Preflight(req, s.Existing)
	if s.Existing == nil {
		return cs
	}
	out := make([]install.Conflict, 0, len(cs))
	for _, c := range cs {
		if c.Kind != install.ConflictManaged || !s.managedWillSkip(req, c) {
			out = append(out, c)
		}
	}
	return out
}

// managedWillSkip reports whether a managed file will keep its bytes: the
// request names the same source the recorded install put there, and what is
// on disk still hashes to the recorded hash.
func (s *Session) managedWillSkip(req install.Request, c install.Conflict) bool {
	var rec state.File
	found := false
	for _, f := range s.Existing.Files {
		if f.Path == c.Path {
			rec, found = f, true
			break
		}
	}
	if !found {
		return false
	}
	// The compiler dll has no version: it is either ours and intact, or it
	// is not.
	if path.Base(c.Path) != artifacts.D3DCompiler {
		if req.Version != s.Existing.ReShade.Version ||
			string(req.Flavor) != s.Existing.ReShade.Flavor ||
			req.DLLName != s.Existing.ReShade.DLL {
			return false
		}
	}
	sum, err := s.managedSum(filepath.Join(s.Entry.Root, filepath.FromSlash(c.Path)))
	return err == nil && sum == rec.SHA256
}

// managedSig remembers a managed file's hash alongside the size and mtime
// it was taken at, so a per-frame call stats instead of re-reading
// megabytes of DLL.
type managedSig struct {
	size  int64
	mtime time.Time
	sum   string
}

func (s *Session) managedSum(abs string) (string, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if sig, ok := s.managedSigs[abs]; ok && sig.size == info.Size() && sig.mtime.Equal(info.ModTime()) {
		return sig.sum, nil
	}
	sum, err := fileSHA256(abs)
	if err != nil {
		return "", err
	}
	if s.managedSigs == nil {
		s.managedSigs = map[string]managedSig{}
	}
	s.managedSigs[abs] = managedSig{size: info.Size(), mtime: info.ModTime(), sum: sum}
	return sum, nil
}

// fileSHA256 hashes a file for comparison, not for the manifest — the
// install engine keeps its own copy tied to the bytes it writes.
func fileSHA256(abs string) (string, error) {
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ---- what changes ----

// Installed reports whether the reference folder already has an install.
func (s *Session) Installed() bool { return s.Existing != nil }

// namedDiff returns what an edit adds and removes, as display names rather
// than ids. An id the catalog no longer offers falls back to the id, so a
// mod that disappeared upstream is still named.
func namedDiff(before, after []string, names map[string]string) (added, removed []string) {
	name := func(id string) string {
		if n, ok := names[id]; ok && n != "" {
			return n
		}
		return id
	}
	for _, id := range after {
		if !slices.Contains(before, id) {
			added = append(added, name(id))
		}
	}
	for _, id := range before {
		if !slices.Contains(after, id) {
			removed = append(removed, name(id))
		}
	}
	return added, removed
}

// Change is one difference between what is installed and what is chosen.
type Change struct {
	// Kind is "add", "remove" or "swap".
	Kind string
	Text string
}

// Changes lists how the choices differ from the install recorded for the
// reference folder — what Apply is going to do. Nil for a fresh install.
func (s *Session) Changes() []Change {
	if s.Existing == nil {
		return nil
	}
	var out []Change
	for _, g := range s.Folders.Added() {
		out = append(out, Change{"add", "Installing on folder " + DetailFolderLabel(s.Entry, g.Dir)})
	}
	for _, g := range s.Folders.Removed() {
		out = append(out, Change{"remove", "Uninstalling from folder " + DetailFolderLabel(s.Entry, g.Dir)})
	}
	if s.Version != "" && s.Version != s.Existing.ReShade.Version {
		out = append(out, Change{"swap", fmt.Sprintf("ReShade %s → %s", displayVersion(s.Existing.ReShade.Version), s.Version)})
	}
	if string(s.Flavor) != s.Existing.ReShade.Flavor {
		out = append(out, Change{"swap", "Build → " + string(s.Flavor)})
	}
	if s.DLL != "" && s.DLL != s.Existing.ReShade.DLL {
		out = append(out, Change{"swap", s.Existing.ReShade.DLL + " → " + s.DLL})
	}

	names := s.ItemNames()
	add := func(added, removed []string) {
		for _, n := range added {
			out = append(out, Change{"add", n})
		}
		for _, n := range removed {
			out = append(out, Change{"remove", n})
		}
	}
	add(namedDiff(s.Existing.Packages, s.SelectedPackages(), names))
	add(namedDiff(s.Existing.Addons, s.SelectedAddons(), names))
	add(namedDiff(s.Existing.Custom, s.SelectedCustom(), names))

	before, after := s.Existing.RenoDX, s.RenoDXChoice()
	if before != after {
		title := func(id string) string {
			if m, ok := s.renodxMod(id); ok {
				return m.Title
			}
			return id
		}
		if after != "" {
			out = append(out, Change{"add", "RenoDX " + title(after)})
		}
		if before != "" {
			out = append(out, Change{"remove", "RenoDX " + title(before)})
		}
	}
	return out
}

// displayVersion renders a recorded version for a change line. An adopted
// install has no version worth printing verbatim.
func displayVersion(v string) string {
	if v == install.AdoptedVersion {
		return "adopted"
	}
	return v
}

// Dirty reports whether applying would do anything: always for a fresh
// install, and for an edit only when the choices differ from what is there.
func (s *Session) Dirty() bool {
	return s.Existing == nil || len(s.Changes()) > 0
}

// Headline is one sentence saying what applying will do, for the dock.
func (s *Session) Headline() string {
	if s.Existing != nil {
		ch := s.Changes()
		if len(ch) == 0 {
			return "Up to date"
		}
		parts := make([]string, 0, 3)
		for _, c := range ch[:min(len(ch), 3)] {
			parts = append(parts, c.Text)
		}
		line := strings.Join(parts, ", ")
		if n := len(ch) - len(parts); n > 0 {
			line += fmt.Sprintf(", +%d more", n)
		}
		return line
	}
	parts := []string{"ReShade " + s.Version}
	if n := len(s.SelectedPackages()) + countCustom(s.SelectedCustom(), "custom:shaders:"); n > 0 {
		parts = append(parts, plural(n, "shader pack"))
	}
	if n := len(s.SelectedAddons()) + countCustom(s.SelectedCustom(), "custom:addons:"); n > 0 {
		parts = append(parts, plural(n, "add-on"))
	}
	if s.RenoDXChoice() != "" {
		parts = append(parts, "RenoDX")
	}
	return "Installs " + strings.Join(parts, ", ")
}

func countCustom(ids []string, prefix string) int {
	n := 0
	for _, id := range ids {
		if strings.HasPrefix(id, prefix) {
			n++
		}
	}
	return n
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// DownloadNote says what has to be downloaded first, or that nothing does.
func (s *Session) DownloadNote() string {
	m := s.Missing()
	if len(m) == 0 {
		return "Nothing to download"
	}
	return plural(len(m), "download") + " needed"
}

// Warnings is everything worth a second look before applying, in plain
// words: what Unmet reports.
func (s *Session) Warnings() []string { return s.Unmet() }

// ExeFor is the executable the session uses for a folder: the one it is
// installed against, else the chosen pick.
func (s *Session) ExeFor(g FolderGroup) Executable { return s.Folders.ExeFor(g) }

// Arch is the reference executable's architecture, for display.
func (s *Session) Arch() game.Arch { return s.Exe.Arch }
