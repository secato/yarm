package app

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

// wizardStep is one page of the install wizard. Each page asks exactly one
// question, and every page after the first carries a summary line of what
// the earlier ones already answered — the reason a step sequence is
// workable here at all, since the cost of paging is not being able to see
// what you already chose.
//
// There is no Exe step: which executable (and therefore which folder) is
// resolved by the screen that opened the wizard — the games list or the
// game detail screen — before it ever appears.
type wizardStep int

const (
	stepReShade wizardStep = iota
	stepAPI
	stepShaders
	stepAddons
	stepReview
	// stepHub is the edit-mode landing page: every section with what is
	// currently installed in it, each openable on its own. Declared after
	// the linear steps so their breadcrumb numbering is unaffected —
	// editing does not use the breadcrumb at all.
	stepHub
)

// wizardSteps is every step in order, for the breadcrumb.
var wizardSteps = []wizardStep{stepReShade, stepAPI, stepShaders, stepAddons, stepReview}

// label names a step for the breadcrumb.
func (s wizardStep) label() string {
	switch s {
	case stepReShade:
		return "ReShade"
	case stepAPI:
		return "API"
	case stepShaders:
		return "Shaders"
	case stepAddons:
		return "Add-ons"
	case stepReview:
		return "Review"
	case stepHub:
		return "Summary"
	default:
		return "?"
	}
}

// Pane navigation on the ReShade step, named the same way the resources
// browser names its own — the two screens show the same normal/addon
// split, so they should be driven by the same keys.
var (
	wizardPaneLeft  = key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "normal"))
	wizardPaneRight = key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "addon"))
)

// wizardShowAll widens the shaders and add-ons steps from the curated
// shortlist to the whole catalog. Not a global binding: it means nothing
// on the other three steps.
var wizardShowAll = key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "show all"))

// dllOption is one radio choice on the API step, matching ReShade's own
// installer (docs/plan/04-external-sources.md §4.6).
type dllOption struct {
	Name string
	For  string
}

var dllOptions = []dllOption{
	{"dxgi.dll", "D3D10 / D3D11 / D3D12 (recommended)"},
	{"d3d11.dll", "D3D11 only"},
	{"d3d10.dll", "D3D10 only"},
	{"d3d12.dll", "D3D12 only"},
	{"d3d9.dll", "D3D9"},
	{"opengl32.dll", "OpenGL"},
}

// wizardFlavors is the order the two ReShade builds are shown in, left to
// right — the same order the resources browser lists its panes.
var wizardFlavors = []install.Flavor{install.FlavorNormal, install.FlavorAddon}

// wizardDataLoadedMsg carries the catalog/custom data the wizard needs,
// once it has loaded — successfully or not.
//
// Built as a plain tea.Cmd in Init rather than through Async: Async would
// route a load failure to the shell's generic error overlay without ever
// reaching this screen's Update, leaving s.loading stuck true and every
// step forever showing "loading catalog data…" once the dialog closed.
type wizardDataLoadedMsg struct {
	data WizardData
	err  error
}

// WizardScreen walks the user through installing ReShade: which version
// and build, which proxy DLL, which effect shaders and add-ons, then a
// review before running. The folder it targets is fixed at construction —
// decided by whichever screen opened the wizard. ReShade intercepts by
// directory, so there is nothing per-executable to ask about here.
//
// It is one Screen implementation holding a step index and one field group
// per step, rather than several pushed screens, so the in-progress Request
// lives in exactly one place (§6.1: "wizard holds Request").
type WizardScreen struct {
	keys     KeyMap
	entry    GameEntry
	exe      Executable
	deps     Deps
	targetOS artifacts.TargetOS

	// existing is the install already recorded for this folder, if any —
	// the wizard starts every step's selection from it instead of the
	// configured defaults, so re-running the wizard to add an add-on or
	// switch build does not silently reset everything else.
	existing *state.Install

	step wizardStep
	// editing turns the linear five-step walk into a hub: an install that
	// already exists is usually being adjusted in one place, and making
	// someone re-confirm four unchanged answers to add one shader is the
	// wrong shape for that.
	editing   bool
	hubCursor cursorList

	loading bool
	loadErr string
	data    WizardData

	// Step: ReShade. The two builds are side-by-side panes over the same
	// version list, so ←/→ switches build without losing which version is
	// highlighted.
	flavor        install.Flavor
	versionCursor cursorList

	// Step: API
	dllCursor cursorList

	// Steps: shaders, add-ons. Both lists show a curated shortlist until
	// showAll is toggled on; the selections behind them are unaffected by
	// which rows are visible.
	packages multiSelect
	addons   multiSelect
	showAll  bool

	// Step: review — an options checklist (currently just "overwrite"),
	// navigated the same way shaders/add-ons are.
	options multiSelect
}

// NewWizardScreen returns a wizard targeting the folder exe lives in. When
// that folder already has a recorded install, the wizard edits it: every
// step starts from what is already installed (build, version, DLL,
// packages, add-ons) instead of the configured defaults.
func NewWizardScreen(entry GameEntry, exe Executable, deps Deps) *WizardScreen {
	existing := exe.Installed

	// The normal build unless something says otherwise: the add-on build
	// is the one anti-cheat can detect, so it is opted into, never
	// defaulted into.
	flavor := install.FlavorNormal
	switch {
	case existing != nil:
		flavor = install.Flavor(existing.ReShade.Flavor)
	case deps.Defaults.ReshadeFlavor == string(install.FlavorAddon):
		flavor = install.FlavorAddon
	}

	step := stepReShade
	if existing != nil {
		step = stepHub
	}

	return &WizardScreen{
		keys:     DefaultKeyMap(),
		step:     step,
		editing:  existing != nil,
		entry:    entry,
		exe:      exe,
		deps:     deps,
		targetOS: artifacts.CurrentTargetOS(),
		existing: existing,
		loading:  true,
		flavor:   flavor,
		options:  newOptions(),
	}
}

// newOptions builds the review page's checklist. Backups start on: the
// only irreversible thing an install does is replacing a file the user had
// without keeping a copy, and that has to be something they chose rather
// than something they failed to notice.
func newOptions() multiSelect {
	m := newMultiSelect([]selectItem{
		{ID: "overwrite", Name: "Overwrite existing files"},
		{
			ID:          "backup",
			Name:        "Back up what it replaces",
			Description: "saved as .yarm-bak beside each file, and put back when you uninstall",
		},
	})
	m.selected["backup"] = true
	return m
}

// Init implements Screen.
func (s *WizardScreen) Init() tea.Cmd {
	if s.deps.WizardData == nil {
		return nil
	}
	loader := s.deps.WizardData
	return func() tea.Msg {
		data, err := loader.LoadWizardData(context.Background())
		return wizardDataLoadedMsg{data: data, err: err}
	}
}

// verb is what finishing the wizard will actually do, given whether this
// folder already has an install recorded.
func (s *WizardScreen) verb() string {
	if s.existing != nil {
		return "update"
	}
	return "install"
}

// Title implements Screen.
func (s *WizardScreen) Title() string {
	if s.editing {
		if s.step == stepHub {
			return "edit install"
		}
		return "edit install — " + s.step.label()
	}
	return fmt.Sprintf("%s ReShade — %d %s", s.verb(), s.stepNumber(s.step), s.step.label())
}

// stepNumber is a step's 1-based position in the breadcrumb.
func (s *WizardScreen) stepNumber(step wizardStep) int { return int(step) + 1 }

// KeyBindings implements Screen.
func (s *WizardScreen) KeyBindings() []key.Binding {
	switch s.step {
	case stepHub:
		return []key.Binding{s.keys.Up, s.keys.Down, s.keys.Enter, s.keys.Back}
	case stepReShade:
		return []key.Binding{s.keys.Up, s.keys.Down, wizardPaneLeft, wizardPaneRight, s.keys.Enter, s.keys.Back}
	case stepShaders, stepAddons:
		return []key.Binding{s.keys.Up, s.keys.Down, s.keys.Toggle, wizardShowAll, s.keys.Enter, s.keys.Back}
	case stepReview:
		return []key.Binding{
			s.keys.Up, s.keys.Down, s.keys.Toggle,
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", s.verb())),
			s.keys.Back,
		}
	default:
		return []key.Binding{s.keys.Up, s.keys.Down, s.keys.Enter, s.keys.Back}
	}
}

// HandleBack implements backHandler: step back a page rather than leaving
// the wizard outright, except from the first page.
func (s *WizardScreen) HandleBack() (Screen, tea.Cmd, bool) {
	if s.editing {
		// Editing is random access, so back always means "back to the
		// summary" — never a step backwards through an order the user
		// never walked.
		if s.step == stepHub {
			return s, nil, false
		}
		s.step = stepHub
		return s, nil, true
	}
	if s.step == stepReShade {
		return s, nil, false
	}
	s.step = s.prevStep(s.step)
	return s, nil, true
}

// afterStep is where enter goes from a section: the next question when
// installing, and back to the summary when editing.
func (s *WizardScreen) afterStep(from wizardStep) wizardStep {
	if s.editing {
		return stepHub
	}
	return s.nextStep(from)
}

// hubSections lists the summary's rows, in order. Add-ons only appear for
// a build that can load them, and Review is last because it is the one row
// that leaves the summary rather than returning to it.
func (s *WizardScreen) hubSections() []wizardStep {
	rows := []wizardStep{stepReShade, stepAPI, stepShaders}
	if s.flavor.Addon() {
		rows = append(rows, stepAddons)
	}
	return append(rows, stepReview)
}

// syncHub re-clamps the summary cursor after the row list can have
// changed, which is any time the build changed.
func (s *WizardScreen) syncHub() { s.hubCursor.setCount(len(s.hubSections())) }

// prevStep steps backward, skipping Add-ons when the build cannot load
// them.
func (s *WizardScreen) prevStep(from wizardStep) wizardStep {
	prev := from - 1
	if prev == stepAddons && !s.flavor.Addon() {
		prev--
	}
	return prev
}

// nextStep steps forward, skipping Add-ons when the build cannot load them
// (§6.1: "skipped when flavor = normal").
func (s *WizardScreen) nextStep(from wizardStep) wizardStep {
	next := from + 1
	if next == stepAddons && !s.flavor.Addon() {
		next++
	}
	return next
}

// folder is the directory the install lands in — what the user actually
// chose, and what ReShade attaches to. The executable underneath it only
// decides which entry yarm records the install against.
func (s *WizardScreen) folder() string {
	return filepath.Join(s.entry.Root, filepath.FromSlash(install.ExeDir(s.exe.Path)))
}

// Update implements Screen.
func (s *WizardScreen) Update(msg tea.Msg, env Env) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case wizardDataLoadedMsg:
		s.loading = false
		s.data = msg.data
		existingVersion := ""
		if s.existing != nil {
			existingVersion = s.existing.ReShade.Version
		}
		s.data.Versions = capVersions(s.data.Versions, existingVersion)

		s.packages = newMultiSelect(s.data.PackagesWithCustom(s.deps.CacheStatus))
		s.addons = newMultiSelect(s.data.AddonsWithCustom(s.deps.CacheStatus))
		versionIndex := s.indexOfLatest()
		if s.existing != nil {
			s.preselectExistingIDs(s.packages, s.existing.Packages)
			s.preselectExistingIDs(s.addons, s.existing.Addons)
			versionIndex = s.indexOfVersion(s.existing.ReShade.Version)
		} else {
			s.preselectDefaultPackages()
		}
		// After preselection, not before: what is already selected is part
		// of what the shortlist has to keep visible.
		s.refreshLists()
		s.versionCursor = newCursorList(len(s.data.Versions), versionIndex)
		s.dllCursor = newCursorList(len(dllOptions), s.recommendedDLLIndex())
		// The summary's rows depend on the build, which is only settled
		// once the recorded install (or the configured default) has been
		// applied above.
		s.syncHub()

		switch {
		case msg.err != nil:
			s.loadErr = msg.err.Error()
			return s, ReportError(msg.err)
		case len(s.data.Versions) == 0 && len(s.data.Packages) == 0 && len(s.data.Addons) == 0:
			s.loadErr = "no catalog data available (offline, with nothing cached yet?)"
		}
		return s, nil

	case tea.KeyPressMsg:
		return s.handleKey(msg, env)
	}
	return s, nil
}

// capVersions limits the offered list to the most recent 10, plus the
// existing install's version if editing one that has since aged out of
// that window — otherwise re-running the wizard on an old install would
// silently jump to latest instead of showing what is actually there.
func capVersions(versions []catalog.Version, existing string) []catalog.Version {
	const shown = 10
	if len(versions) > shown {
		// A full slice expression caps capacity too, so the append below
		// (when needed) allocates a new backing array instead of
		// overwriting versions the caller's slice still holds beyond the
		// cutoff.
		versions = versions[:shown:shown]
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

// refreshLists rebuilds what the shaders and add-ons steps show, given
// whether the shortlist is in force. Selections survive it: curate keeps
// every checked row visible, so selectedIDs stays complete whichever view
// is on.
func (s *WizardScreen) refreshLists() {
	sel := s.selection()
	s.packages.setItems(curate(s.annotate(s.fullPackages(), sel), curatedPackages, s.showAll, s.packages.selected))
	s.addons.setItems(curate(s.annotate(s.fullAddons(), sel), curatedAddons, s.showAll, s.addons.selected))
}

// selection is everything checked across both steps, which is what a
// requirement is judged against: add-ons depend on effect packages, so
// neither list can answer the question alone. Add-ons only count when the
// build can load them.
func (s *WizardScreen) selection() map[string]bool {
	sel := make(map[string]bool, len(s.packages.selected)+len(s.addons.selected))
	for id, on := range s.packages.selected {
		sel[id] = on
	}
	if s.flavor.Addon() {
		for id, on := range s.addons.selected {
			sel[id] = on
		}
	}
	return sel
}

// annotate marks rows with what they still need, and rows that are only
// there because something else needs them. Both are the same fact seen
// from either end, and without them a package appearing checked that the
// user never checked would look like a bug.
func (s *WizardScreen) annotate(items []selectItem, sel map[string]bool) []selectItem {
	// Which selected entries pull in which packages, so a dependency can
	// name what wanted it.
	wantedBy := map[string][]string{}
	for _, src := range s.allItems() {
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

	out := make([]selectItem, len(items))
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

// allItems is every catalog row from both steps, unfiltered — the lookup
// requirements are resolved against, which must not depend on which rows
// happen to be visible.
func (s *WizardScreen) allItems() []selectItem {
	return append(s.fullPackages(), s.fullAddons()...)
}

// catalogIDs is every id the loaded catalog offers, so a requirement can
// only ever select something that exists to download.
func (s *WizardScreen) catalogIDs() map[string]bool {
	ids := map[string]bool{}
	for _, it := range s.allItems() {
		if !it.Header && !it.Disabled {
			ids[it.ID] = true
		}
	}
	return ids
}

// itemNames maps every catalog id to its display name.
func (s *WizardScreen) itemNames() map[string]string {
	names := map[string]string{}
	for _, it := range s.allItems() {
		names[it.ID] = it.Name
	}
	return names
}

// unmet lists the requirements still not satisfied by the current
// selection, for the review page.
func (s *WizardScreen) unmet() []string {
	sel := s.selection()
	ids := append(s.packages.selectedIDs(), addonsForDownload(s.flavor, s.addons)...)
	return unmetRequirements(ids, s.itemNames(), sel)
}

func (s *WizardScreen) fullPackages() []selectItem {
	return s.data.PackagesWithCustom(s.deps.CacheStatus)
}

func (s *WizardScreen) fullAddons() []selectItem {
	return s.data.AddonsWithCustom(s.deps.CacheStatus)
}

// showAllHint is the footer's half of the shortlist: a list that hides
// most of the catalog has to say so, and how much of it is hidden.
func (s *WizardScreen) showAllHint() string {
	if s.showAll {
		return "a recommended"
	}
	total := len(s.fullPackages())
	if s.step == stepAddons {
		total = len(s.fullAddons())
	}
	return fmt.Sprintf("a all (%d)", total)
}

// preselectDefaultPackages adds the user's configured default packages
// (aliases allowed, matching how they are written in config.yaml) on top
// of whatever newMultiSelect already preselected via Required.
func (s *WizardScreen) preselectDefaultPackages() {
	for _, id := range s.deps.Defaults.Packages {
		resolved := catalog.ResolveAlias(id)
		if resolved != "" {
			s.packages.selected[resolved] = true
		}
	}
}

// preselectExistingIDs marks every id from a recorded install as selected,
// on top of whatever newMultiSelect already preselected via Required. ids
// are already canonical catalog ids (recorded verbatim from a prior
// buildRequest), unlike the aliases config.yaml allows for defaults.
func (s *WizardScreen) preselectExistingIDs(m multiSelect, ids []string) {
	for _, id := range ids {
		m.selected[id] = true
	}
}

// indexOfLatest returns the position of the version marked Latest, or 0.
func (s *WizardScreen) indexOfLatest() int {
	for i, v := range s.data.Versions {
		if v.Latest {
			return i
		}
	}
	return 0
}

// indexOfVersion returns the position of version in the catalog list, or
// the latest version's when it is no longer listed (an adopted install's
// version is recorded as "unknown (adopted)", which never matches).
func (s *WizardScreen) indexOfVersion(version string) int {
	for i, v := range s.data.Versions {
		if v.Version == version {
			return i
		}
	}
	return s.indexOfLatest()
}

// recommendedDLLIndex returns the dllOptions index to preselect: the
// recorded DLL when editing an existing install, otherwise a guess from
// the target exe's API, or 0 (dxgi.dll) when there is no safe
// recommendation.
func (s *WizardScreen) recommendedDLLIndex() int {
	rec := ""
	if s.existing != nil {
		rec = s.existing.ReShade.DLL
	} else {
		rec = s.exe.API.RecommendedDLL()
	}
	if rec == "" {
		return 0
	}
	for i, o := range dllOptions {
		if o.Name == rec {
			return i
		}
	}
	return 0
}

// selectedVersion returns the version highlighted on the ReShade step.
func (s *WizardScreen) selectedVersion() (catalog.Version, bool) {
	i := s.versionCursor.Cursor()
	if i < 0 || i >= len(s.data.Versions) {
		return catalog.Version{}, false
	}
	return s.data.Versions[i], true
}

// selectedDLL returns the DLL name highlighted on the API step.
func (s *WizardScreen) selectedDLL() string {
	i := s.dllCursor.Cursor()
	if i < 0 || i >= len(dllOptions) {
		return ""
	}
	return dllOptions[i].Name
}

// overwrite reports whether the review's "overwrite existing files"
// option is checked.
func (s *WizardScreen) overwrite() bool { return s.options.selected["overwrite"] }

// backup reports whether displaced files are saved rather than discarded.
// Only meaningful alongside overwrite, which is the only thing that
// displaces anything.
func (s *WizardScreen) backup() bool { return s.options.selected["backup"] }

// cached reports whether the given version is already downloaded for the
// given build.
func (s *WizardScreen) cached(version string, flavor install.Flavor) bool {
	return s.deps.CacheStatus != nil && s.deps.CacheStatus.HasReShade(version, flavor.Addon())
}

func (s *WizardScreen) handleKey(msg tea.KeyPressMsg, env Env) (Screen, tea.Cmd) {
	switch s.step {
	case stepHub:
		return s.handleHubKey(msg)
	case stepReShade:
		return s.handleReShadeKey(msg)
	case stepAPI:
		return s.handleAPIKey(msg)
	case stepShaders:
		return s.handleMultiSelectKey(msg, &s.packages, stepShaders)
	case stepAddons:
		return s.handleMultiSelectKey(msg, &s.addons, stepAddons)
	case stepReview:
		return s.handleReviewKey(msg)
	}
	return s, nil
}

func (s *WizardScreen) handleHubKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	// ←/→ because the sections are side by side; ↑/↓ works too, since
	// below the width where the panes fit they stack into a list.
	case key.Matches(msg, wizardPaneLeft), key.Matches(msg, s.keys.Up):
		s.hubCursor.up()
	case key.Matches(msg, wizardPaneRight), key.Matches(msg, s.keys.Down):
		s.hubCursor.down()
	case key.Matches(msg, s.keys.Enter):
		rows := s.hubSections()
		if i := s.hubCursor.Cursor(); i >= 0 && i < len(rows) {
			s.step = rows[i]
		}
	}
	return s, nil
}

func (s *WizardScreen) handleReShadeKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Up):
		s.versionCursor.up()
	case key.Matches(msg, s.keys.Down):
		s.versionCursor.down()
	case key.Matches(msg, wizardPaneLeft):
		s.flavor = install.FlavorNormal
		s.syncHub()
	case key.Matches(msg, wizardPaneRight):
		s.flavor = install.FlavorAddon
		s.syncHub()
	case key.Matches(msg, s.keys.Enter):
		if _, ok := s.selectedVersion(); ok {
			s.step = s.afterStep(s.step)
		}
	}
	return s, nil
}

func (s *WizardScreen) handleAPIKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Up):
		s.dllCursor.up()
	case key.Matches(msg, s.keys.Down):
		s.dllCursor.down()
	case key.Matches(msg, s.keys.Enter):
		if s.selectedDLL() != "" {
			s.step = s.afterStep(s.step)
		}
	}
	return s, nil
}

func (s *WizardScreen) handleMultiSelectKey(msg tea.KeyPressMsg, m *multiSelect, current wizardStep) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Up):
		m.up()
	case key.Matches(msg, s.keys.Down):
		m.down()
	case key.Matches(msg, s.keys.Toggle):
		id := ""
		if m.canToggle(m.cursor) {
			id = m.items[m.cursor].ID
		}
		m.toggle()
		// Turning something on pulls in what it needs; turning it off
		// leaves the dependency alone, since it may be wanted on its own
		// and the review page will say if something is now missing.
		if id != "" && m.selected[id] {
			satisfy(id, s.packages.selected, s.catalogIDs())
		}
		s.refreshLists()
	case key.Matches(msg, wizardShowAll):
		s.showAll = !s.showAll
		s.refreshLists()
	case key.Matches(msg, s.keys.Enter):
		s.step = s.afterStep(current)
	}
	return s, nil
}

func (s *WizardScreen) handleReviewKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Up):
		s.options.up()
	case key.Matches(msg, s.keys.Down):
		s.options.down()
	case key.Matches(msg, s.keys.Toggle):
		was := s.overwrite()
		s.options.toggle()
		// Turning overwrite on re-asserts the safe default: whatever was
		// decided about backups the last time overwriting was on should
		// not quietly carry over into a new decision to overwrite.
		if !was && s.overwrite() {
			s.options.selected["backup"] = true
		}
	case key.Matches(msg, s.keys.Enter):
		req, ok := s.buildRequest()
		if !ok {
			return s, nil
		}
		return s, PushScreen(NewProgressScreen(req, s.deps.Installer))
	}
	return s, nil
}

// buildRequest assembles the install.Request from every step's selection.
// ok is false if something required is missing — the caller should not be
// able to reach Review without everything set, but this is the one place
// that would notice if it happened anyway.
func (s *WizardScreen) buildRequest() (install.Request, bool) {
	version, ok := s.selectedVersion()
	if !ok {
		return install.Request{}, false
	}
	dll := s.selectedDLL()
	if dll == "" {
		return install.Request{}, false
	}

	return install.Request{
		Game:     s.entry.Game,
		Exe:      s.exe.Executable,
		Version:  version.Version,
		Flavor:   s.flavor,
		DLLName:  dll,
		Packages: s.packages.selectedIDs(),
		// Selections made while add-ons were on offer must not survive a
		// later switch back to the normal build: normal can't load them at
		// all, and install.Request.Validate rejects a request that tries,
		// so a stale selection here would silently block the install
		// rather than merely not installing an add-on.
		Addons:    addonsForDownload(s.flavor, s.addons),
		Overwrite: s.overwrite(),
		NoBackup:  !s.backup(),
		TargetOS:  s.targetOS,
	}, true
}

// missing names what finishing the wizard would still have to download.
func (s *WizardScreen) missing() []string {
	version, ok := s.selectedVersion()
	if !ok {
		return nil
	}
	return describeMissing(s.deps.CacheStatus, version.Version, s.flavor.Addon(),
		s.packages.selectedIDs(), addonsForDownload(s.flavor, s.addons),
		s.exe.Arch, artifacts.NeedsD3DCompiler(s.targetOS))
}

// View implements Screen.
func (s *WizardScreen) View(env Env) string {
	if s.loading {
		return env.Styles.Faint.Render("loading catalog data…")
	}
	if s.loadErr != "" {
		return env.Styles.Bad.Render(s.loadErr)
	}

	head := s.viewHeader(env)
	foot := s.viewFooter(env)

	// Whatever the header and footer do not use belongs to the step's own
	// list, which windows into it rather than printing past the bottom.
	body := env.Height - countLines(head) - countLines(foot)
	if body < 4 {
		body = 4
	}

	var b strings.Builder
	b.WriteString(head)
	switch s.step {
	case stepHub:
		s.viewHub(&b, env, body)
	case stepReShade:
		s.viewReShade(&b, env, body)
	case stepAPI:
		s.viewAPI(&b, env, body)
	case stepShaders:
		writeSelectList(&b, env, s.packages, body)
	case stepAddons:
		writeSelectList(&b, env, s.addons, body)
	case stepReview:
		s.viewReview(&b, env, body)
	}
	b.WriteString(foot)
	return b.String()
}

// viewHeader is the block above every step: where the install is going,
// how far through the wizard we are, and — the point of paging at all
// being bearable — what the earlier steps already decided.
func (s *WizardScreen) viewHeader(env Env) string {
	var b strings.Builder
	if s.editing {
		// No breadcrumb: there is no numbered walk to be partway through.
		// What matters is which section is open, and that closing it goes
		// back to the summary rather than onwards.
		// Clipped before styling: clipTail counts runes, and a styled
		// string's runes are mostly escape sequences.
		trail := "Editing install"
		if s.step != stepHub {
			trail += " › " + s.step.label()
		}
		b.WriteString(env.Styles.Accent.Render(clipTail(trail, env.Width)))
		b.WriteString("\n")
		b.WriteString(env.Styles.Faint.Render(truncate(s.folder(), env.Width-1)))
		b.WriteString("\n\n")
		return b.String()
	}
	b.WriteString(s.breadcrumb(env))
	b.WriteString("\n")
	b.WriteString(env.Styles.Faint.Render(truncate(s.folder(), env.Width-1)))
	b.WriteString("\n")
	if summary := s.decided(env); summary != "" {
		b.WriteString(summary)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

func (s *WizardScreen) breadcrumb(env Env) string {
	var parts []string
	for _, st := range wizardSteps {
		label := fmt.Sprintf("%d %s", s.stepNumber(st), st.label())
		switch {
		case st == stepAddons && !s.flavor.Addon():
			parts = append(parts, env.Styles.Faint.Render(label+" (skipped)"))
		case st == s.step:
			parts = append(parts, env.Styles.Accent.Render(label))
		default:
			parts = append(parts, env.Styles.Faint.Render(label))
		}
	}
	line := strings.Join(parts, " › ")
	if lipgloss.Width(line) <= env.Width {
		return line
	}
	// Too narrow for the trail: where you are still fits, and matters more
	// than what the other four steps are called.
	return env.Styles.Accent.Render(fmt.Sprintf("%d/%d %s",
		s.stepNumber(s.step), len(wizardSteps), s.step.label()))
}

// decided is the running summary of every step already answered, so a
// later step never has to be taken on trust or paged back to. Empty on the
// first step, where nothing has been decided yet.
func (s *WizardScreen) decided(env Env) string {
	var parts []string
	add := func(text string) {
		parts = append(parts, env.Styles.Good.Render("✓")+" "+text)
	}

	if s.step > stepReShade {
		if v, ok := s.selectedVersion(); ok {
			add(fmt.Sprintf("ReShade %s (%s)", v.Version, s.flavor))
		}
	}
	if s.step > stepAPI {
		// Just the name here: which APIs it covers is what the API step
		// itself is for, and spelling it out again pushes the summary onto
		// a second line for no new information.
		add(s.selectedDLL())
	}
	if s.step > stepShaders {
		add(countSummary(s.packages, "shader"))
	}
	if s.step > stepAddons && s.flavor.Addon() {
		add(countSummary(s.addons, "add-on"))
	}
	if len(parts) == 0 {
		return ""
	}

	// Three spaces read as a gap between facts without needing a
	// separator glyph; if they do not fit, one per line does.
	line := strings.Join(parts, "   ")
	if lipgloss.Width(line) <= env.Width {
		return line
	}
	return strings.Join(parts, "\n")
}

// countSummary names a selection by its picks when there are few enough to
// read, and by a count when there are not.
func countSummary(m multiSelect, noun string) string {
	var names []string
	for i, it := range m.items {
		if !it.Header && m.isSelected(i) {
			names = append(names, it.Name)
		}
	}
	switch {
	case len(names) == 0:
		return "no " + noun + "s"
	case len(names) <= 2:
		return strings.Join(names, ", ")
	default:
		return fmt.Sprintf("%d %ss", len(names), noun)
	}
}

// viewFooter is the block under every step: the add-on safety warning when
// it applies, and the keys.
func (s *WizardScreen) viewFooter(env Env) string {
	var b strings.Builder
	if s.flavor.Addon() {
		writeAnticheatWarning(&b, env, env.Width-1)
	} else {
		b.WriteString("\n")
	}

	action, hint := "enter continues", ""
	switch s.step {
	case stepHub:
		action = "enter opens"
		hint = "←→ move · esc leaves"
		if s.hubStacked(env) {
			hint = "↑↓ move · esc leaves"
		}
	case stepReShade:
		hint = "↑↓ version · ←→ normal/addon"
	case stepAPI:
		hint = "↑↓ move"
	case stepShaders, stepAddons:
		hint = "↑↓ move · space toggles · " + s.showAllHint()
	case stepReview:
		action = "enter " + s.verb()
		hint = "↑↓ move · space toggles"
	}
	switch {
	case s.editing && s.step != stepHub:
		// Both keys do the same thing here, and saying so is better than
		// implying that one of them discards.
		action = "enter done"
		hint += " · esc back"
		if s.step == stepReview {
			action = "enter " + s.verb()
		}
	case !s.editing && s.step > stepReShade:
		hint += " · esc back"
	}

	b.WriteString(env.Styles.Accent.Render(action))
	b.WriteString(env.Styles.Faint.Render(clipTail("  ·  "+hint, env.Width-lipgloss.Width(action))))
	b.WriteString("\n")
	return b.String()
}

// minVersionPaneWidth is the narrowest a version pane can be and still
// read as a column of versions with their badges.
const minVersionPaneWidth = 22

// viewReShade draws the two builds as side-by-side panes over the same
// version list, so the choice between them is a visible comparison rather
// than a toggle the user has to know about. ←/→ moves between panes; the
// version cursor is shared, so switching builds keeps the version.
func (s *WizardScreen) viewReShade(b *strings.Builder, env Env, height int) {
	if len(s.data.Versions) == 0 {
		b.WriteString(env.Styles.Faint.Render("No versions available."))
		b.WriteString("\n")
		return
	}

	// Two panes need room for both; below that, show the focused build
	// full width with a strip naming the other, the same fold the
	// resources browser uses when its four panes stop fitting.
	const gutter = 2
	if env.Width < len(wizardFlavors)*(minVersionPaneWidth+gutter) {
		b.WriteString(s.flavorStrip(env))
		b.WriteString("\n")
		// The strip is already this pane's header; repeating it inside
		// would just say the same thing twice.
		b.WriteString(s.versionPane(s.flavor, env.Width, height-1, env, false))
		b.WriteString("\n")
		return
	}

	paneWidth := env.Width/len(wizardFlavors) - gutter
	cols := make([]string, 0, len(wizardFlavors))
	for _, fl := range wizardFlavors {
		cols = append(cols, s.versionPane(fl, paneWidth, height, env, true))
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cols[0], strings.Repeat(" ", gutter), cols[1]))
	b.WriteString("\n")
}

// flavorStrip names both builds when only one pane fits, so the other does
// not simply vanish.
func (s *WizardScreen) flavorStrip(env Env) string {
	var parts []string
	for _, fl := range wizardFlavors {
		label := "ReShade (" + string(fl) + ")"
		if fl == s.flavor {
			parts = append(parts, env.Styles.Selected.Render("▸ "+label))
		} else {
			parts = append(parts, env.Styles.Faint.Render(label))
		}
	}
	return strings.Join(parts, "   ")
}

// versionPane renders one build's version list. Only the focused pane
// draws a cursor: the other is there to be compared against, not moved in.
func (s *WizardScreen) versionPane(flavor install.Flavor, width, height int, env Env, showHeader bool) string {
	focused := flavor == s.flavor

	// What the box's border and padding leave for the rows themselves.
	inner := width - 4
	if inner < 8 {
		inner = 8
	}

	var b strings.Builder
	if showHeader {
		header := clipTail("ReShade ("+string(flavor)+")", inner)
		if focused {
			b.WriteString(env.Styles.Accent.Render(header))
		} else {
			b.WriteString(env.Styles.Faint.Render(header))
		}
		b.WriteString("\n")
		height--
	}

	writeWindow(&b, env, len(s.data.Versions), s.versionCursor.Cursor(), height, "", func(i int) {
		v := s.data.Versions[i]
		marker := "  "
		if focused && i == s.versionCursor.Cursor() {
			marker = "▸ "
		}
		line := marker + v.Version
		if v.Latest {
			line += "  latest"
		}
		if s.cached(v.Version, flavor) {
			line += "  cached"
		}
		line = clipTail(line, inner)
		switch {
		case focused && i == s.versionCursor.Cursor():
			line = env.Styles.Selected.Render(line)
		case !focused:
			line = env.Styles.Faint.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	})

	// A bordered box rather than a bare column: two lists side by side
	// read as one wrapped list until something draws the line between
	// them. The border is what makes "these are two builds, pick one"
	// visible, and it is the same panel the resources browser uses for the
	// same split.
	//
	// Styles.Panel spends four columns on border and padding, and lipgloss
	// wraps what does not fit rather than clipping it, which would break
	// the box open.
	panel := env.Styles.Panel.Width(width)
	if focused {
		panel = panel.BorderForeground(env.Styles.Accent.GetForeground())
	}
	return panel.Render(strings.TrimSuffix(b.String(), "\n"))
}

func (s *WizardScreen) viewAPI(b *strings.Builder, env Env, height int) {
	if !s.exe.API.Supported() {
		b.WriteString(env.Styles.Warn.Render(wrap(fmt.Sprintf(
			"%s is not supported in v1 — pick a DLL yourself.", apiLabel(s.exe.API)), env.Width-1)))
		b.WriteString("\n\n")
		height -= 2
	}

	recommended := s.exe.API.RecommendedDLL()
	writeWindow(b, env, len(dllOptions), s.dllCursor.Cursor(), height, "", func(i int) {
		o := dllOptions[i]
		marker := "  "
		if i == s.dllCursor.Cursor() {
			marker = "▸ "
		}
		line := fmt.Sprintf("%s%-14s %s", marker, o.Name, o.For)
		if o.Name == recommended {
			line += "  ← detected " + apiLabel(s.exe.API)
		}
		line = clipTail(line, env.Width)
		if i == s.dllCursor.Cursor() {
			line = env.Styles.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	})
}

// writeSelectList renders a checkbox step's rows, windowed to height.
func writeSelectList(b *strings.Builder, env Env, m multiSelect, height int) {
	// The row under the cursor also prints its description, so it costs two
	// lines rather than one.
	writeWindow(b, env, len(m.items), m.cursor, height-1, "", func(i int) {
		it := m.items[i]
		if it.Header {
			b.WriteString(env.Styles.Faint.Render(it.Name))
			b.WriteString("\n")
			return
		}

		box := "[ ]"
		if m.isSelected(i) {
			box = "[x]"
		}
		marker := "  "
		if i == m.cursor {
			marker = "▸ "
		}
		name := it.Name
		if it.Required {
			name += "  (required)"
		}
		line := fmt.Sprintf("%s%s %s", marker, box, name)

		// A note rides on the row rather than under it: an unmet
		// requirement has to be readable without moving the cursor onto
		// it, and a second line per row would break the window's height
		// arithmetic for the sake of a handful of rows. The note keeps its
		// own color, so the row is styled and clipped before it is
		// appended.
		note, noteStyle := "", env.Styles.Faint
		switch {
		case it.Disabled:
			// A manual-only add-on says both things: that yarm cannot
			// install it, which is why the row is greyed, and where to get
			// it by hand when the catalog knows.
			note = "manual install only"
			if it.DisabledNote != "" {
				note += ": " + it.DisabledNote
			}
		case it.Note != "":
			note = it.Note
			if it.NoteWarn {
				noteStyle = env.Styles.Warn
			}
		}

		if note != "" {
			note = "  — " + note
		}
		line, note = splitRow(line, note, env.Width)
		switch {
		case it.Disabled:
			line = env.Styles.Faint.Render(line)
		case i == m.cursor:
			line = env.Styles.Selected.Render(line)
		}
		b.WriteString(line + noteStyle.Render(note))
		b.WriteString("\n")

		if it.Description != "" && i == m.cursor {
			b.WriteString(env.Styles.Faint.Render(clipTail("    "+it.Description, env.Width)))
			b.WriteString("\n")
		}
	})
}

// viewReview is the commit page. What was chosen is already spelled out in
// the header's summary, so this adds only what the earlier steps could not
// know: what it will have to download, and the options that apply to the
// run itself.
func (s *WizardScreen) viewReview(b *strings.Builder, env Env, height int) {
	section := func(into *strings.Builder, title string) {
		into.WriteString(env.Styles.Subtitle.Render(title))
		into.WriteString("\n")
	}

	// What is already in the folder, and what will happen to it. This is
	// the one thing on this page that can make the install run to
	// completion and still leave ReShade not loading — and the one the
	// Overwrite option below decides.
	var files strings.Builder
	conflicts := s.conflicts()
	if len(conflicts) > 0 {
		section(&files, "Already in this folder")
		blocking := false
		for _, c := range conflicts {
			line, style := s.describeConflict(c, env)
			files.WriteString(style.Render(clipTail(line, env.Width)))
			files.WriteString("\n")
			blocking = blocking || c.Blocking(s.overwrite())
		}
		// What to do about it is said once, under the rows, rather than
		// repeated per row until it clips off the end of every line.
		if note := s.conflictAdvice(blocking); note != "" {
			files.WriteString(env.Styles.Faint.Render(wrap(note, env.Width-1)))
			files.WriteString("\n")
		}
		files.WriteString("\n")
	}

	// The options block is fixed-size but its disclaimer wraps, so measure
	// it rather than assuming a line count; the download list is the only
	// part that can grow, so it is what gives way on a short terminal.
	var opts strings.Builder
	section(&opts, "Options")
	for i, it := range s.options.items {
		box := "[ ]"
		if s.options.isSelected(i) {
			box = "[x]"
		}
		marker := "  "
		if i == s.options.cursor {
			marker = "▸ "
		}
		line := fmt.Sprintf("%s%s %s", marker, box, it.Name)

		// The backup option only does anything while overwriting, and
		// saying so is better than offering a checkbox that silently means
		// nothing.
		note := ""
		if it.ID == "backup" && !s.overwrite() {
			note = "  — only when overwriting"
		}
		line, note = splitRow(line, note, env.Width)
		switch {
		case note != "":
			line = env.Styles.Faint.Render(line)
			if i == s.options.cursor {
				line = env.Styles.Selected.Render(line)
			}
			line += env.Styles.Faint.Render(note)
		case i == s.options.cursor:
			line = env.Styles.Selected.Render(line)
		}
		opts.WriteString(line)
		opts.WriteString("\n")
	}
	// The generic disclaimer only earns its lines when nothing concrete
	// was found: with a conflicts block above, it repeats — less
	// precisely — what that block already said about actual files.
	if !s.overwrite() && len(conflicts) == 0 {
		opts.WriteString("\n")
		opts.WriteString(env.Styles.Warn.Render(wrap(
			"Files not created by yarm are left in place unless overwrite is on.", env.Width-1)))
		opts.WriteString("\n")
	}

	// An unmet requirement is the one thing on this page that can make the
	// install do nothing once it runs, so it goes above the download list
	// and is not allowed to scroll away. Built into its own builder
	// because b already carries the wizard header, which is not this
	// page's to measure.
	var check strings.Builder
	if unmet := s.unmet(); len(unmet) > 0 {
		section(&check, "Check")
		for _, line := range unmet {
			check.WriteString(env.Styles.Warn.Render(clipTail("  ! "+line, env.Width)))
			check.WriteString("\n")
		}
		check.WriteString("\n")
	}
	// The page is assembled separately from b, which already carries the
	// wizard header: only this page's own content can be measured against
	// the height it was given, and only it may be clipped to fit.
	var page strings.Builder
	page.WriteString(files.String())
	page.WriteString(check.String())

	missing := s.missing()
	section(&page, "To download")
	if len(missing) == 0 {
		page.WriteString(env.Styles.Faint.Render("  nothing — everything is already cached"))
		page.WriteString("\n")
	}
	// -2 for this section's own header and the blank line before Options;
	// whatever the Check block above already took comes off too.
	room := height - countLines(opts.String()) - countLines(check.String()) - countLines(files.String()) - 2
	if room < 1 {
		room = 1
	}
	writeWindow(&page, env, len(missing), 0, room, "  ", func(i int) {
		page.WriteString(clipTail("  "+missing[i], env.Width))
		page.WriteString("\n")
	})
	page.WriteString("\n")
	page.WriteString(opts.String())

	// Backstop: on a terminal too short for even a one-row download list
	// plus the options, the arithmetic above cannot win, and a page that
	// overflows would push the footer — the anti-cheat warning included —
	// off the screen entirely.
	b.WriteString(clipLines(page.String(), height))
}

// addonsForDownload returns the selected add-on ids, or none when the
// build does not support add-ons at all.
func addonsForDownload(flavor install.Flavor, addons multiSelect) []string {
	if !flavor.Addon() {
		return nil
	}
	return addons.selectedIDs()
}

// conflicts is what already sits where this install would write. Cheap
// (a handful of stats), so it is recomputed each render rather than cached
// and risking a stale answer after the user alt-tabs away and edits the
// folder.
func (s *WizardScreen) conflicts() []install.Conflict {
	req, ok := s.buildRequest()
	if !ok {
		return nil
	}
	return install.Preflight(req, s.existing)
}

// describeConflict turns one conflict into its row: what is there, and
// what will happen to it. Rows stay short enough to survive a 60-column
// terminal — the part that must never clip is what happens to the file.
func (s *WizardScreen) describeConflict(c install.Conflict, env Env) (string, lipgloss.Style) {
	name := path.Base(c.Path)

	switch {
	case c.Kind == install.ConflictConfig:
		return fmt.Sprintf("  %s — your settings, kept", name), env.Styles.Faint
	case c.Kind == install.ConflictManaged:
		return fmt.Sprintf("  %s — yours, replaced", name), env.Styles.Faint
	case s.overwrite() && s.backup():
		return fmt.Sprintf("  %s (%s) — replaced, original saved", name, humanSize(c.Size)), env.Styles.Warn
	case s.overwrite():
		return fmt.Sprintf("  ! %s (%s) — replaced, original discarded", name, humanSize(c.Size)), env.Styles.Bad
	default:
		// The size is here because it is often what identifies the file: a
		// 24 MiB dxgi.dll is some other injector, not a stale ReShade.
		return fmt.Sprintf("  ! %s (%s) — kept, so ReShade will not load",
			name, humanSize(c.Size)), env.Styles.Bad
	}
}

// conflictAdvice is the one line under the conflicts list saying what to
// do, or what will be done. Empty when everything in the way is already
// yarm's own and there is nothing to decide.
func (s *WizardScreen) conflictAdvice(blocking bool) string {
	switch {
	case blocking:
		return "Turn on Overwrite below to replace these. The originals are saved next to them as " +
			install.BackupSuffix + " files and put back when you uninstall."
	case s.overwrite() && s.backup():
		return "Originals are saved as " + install.BackupSuffix +
			" files and put back when you uninstall."
	case s.overwrite():
		return "Backups are off, so these files are gone for good — uninstall cannot put them back."
	default:
		return ""
	}
}

// viewHub is the edit-mode summary. Editing is the one screen where
// everything about an install is already known, so it shows it: each
// section as a pane listing what is actually in it, rather than a column
// of labels with a truncated value beside each. The panes are also the
// navigation — ←/→ or ↑/↓ moves between them, enter opens one.
func (s *WizardScreen) viewHub(b *strings.Builder, env Env, height int) {
	rows := s.hubSections()
	// The last row is Apply, which is a line under the panes rather than a
	// pane of its own: it is an action, not a thing with contents.
	panes := rows[:len(rows)-1]
	applyFocused := s.hubCursor.Cursor() == len(rows)-1

	apply := s.applyLine(env, applyFocused)

	const gutter = 2
	if s.hubStacked(env) {
		s.viewHubCompact(b, env, height-countLines(apply), rows)
		b.WriteString(apply)
		return
	}

	paneWidth := env.Width/len(panes) - gutter

	// Boxes are as tall as their contents, not as tall as the window: an
	// install with three shaders should not draw twenty empty rows to
	// prove there is room for more. The tallest pane sets the height so
	// the row of boxes still lines up.
	content := 0
	for _, step := range panes {
		if n := len(s.hubLines(step, env, paneWidth-4)); n > content {
			content = n
		}
	}
	// Measured: Panel.Height(h) makes a box exactly h rows tall in total,
	// so the content budget is h minus the two border rows.
	paneHeight := content + 1 + 2 // content + the pane's header + borders
	if max := height - countLines(apply) - 1; paneHeight > max {
		paneHeight = max
	}
	if paneHeight < 5 {
		paneHeight = 5
	}

	cols := make([]string, 0, len(panes)*2)
	for i, step := range panes {
		if i > 0 {
			cols = append(cols, strings.Repeat(" ", gutter))
		}
		cols = append(cols, s.hubPane(step, paneWidth, paneHeight, env, i == s.hubCursor.Cursor()))
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cols...))
	b.WriteString("\n\n")
	b.WriteString(apply)
}

// hubStacked reports whether the summary has had to fall back to one line
// per section — which is also when ↑/↓ is the natural way to move, rather
// than the ←/→ that matches a row of panes.
func (s *WizardScreen) hubStacked(env Env) bool {
	const gutter = 2
	const minPaneWidth = 22
	panes := len(s.hubSections()) - 1 // Apply is a line, not a pane
	return env.Width < panes*(minPaneWidth+gutter)
}

// hubPane draws one section as a box listing what is in it.
func (s *WizardScreen) hubPane(step wizardStep, width, height int, env Env, focused bool) string {
	inner := width - 4
	if inner < 8 {
		inner = 8
	}

	var b strings.Builder
	header := step.label()
	if s.sectionChanged(step) {
		// A dot rather than the word "changed": the Apply line already
		// spells out what changed, and this only has to say where.
		header += " •"
	}
	// The focused pane is the bright one. Styles.Panel's own border color
	// is dimmer than Styles.Faint, so coloring only the *unfocused*
	// borders faint made them the ones that stood out.
	if focused {
		b.WriteString(env.Styles.Accent.Render(clipTail("▸ "+header, inner)))
	} else {
		b.WriteString(env.Styles.Faint.Render(clipTail("  "+header, inner)))
	}
	b.WriteString("\n")

	lines := s.hubLines(step, env, inner)
	// -2 for the border rows, -1 for the header; one more goes when
	// something has to say what was cut.
	room := height - 3
	if len(lines) > room {
		room--
		lines = append(lines[:room], env.Styles.Faint.Render(
			fmt.Sprintf("  +%d more", len(lines)-room)))
	}
	for _, line := range lines {
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Measured, not assumed: Styles.Panel.Width(w) renders a box exactly w
	// columns wide, of which w-4 is text — two for the border, two for the
	// padding.
	panel := env.Styles.Panel.Width(width).Height(height)
	if focused {
		panel = panel.BorderForeground(env.Styles.Accent.GetForeground())
	}
	return panel.Render(strings.TrimSuffix(b.String(), "\n"))
}

// hubLines is what one section's pane lists: the state of the install, in
// that section's own terms.
func (s *WizardScreen) hubLines(step wizardStep, env Env, width int) []string {
	// Each line is clipped before it is styled: a pane that wraps is a
	// pane whose border no longer closes.
	plain := func(text string) string { return clipTail("  "+text, width) }
	faint := func(text string) string { return env.Styles.Faint.Render(plain(text)) }

	switch step {
	case stepReShade:
		v, _ := s.selectedVersion()
		lines := []string{plain(v.Version), faint(string(s.flavor) + " build")}
		// An update the user has not noticed is exactly the thing a
		// summary of an existing install should surface.
		if latest, ok := s.latestVersion(); ok && latest != v.Version {
			lines = append(lines, env.Styles.Accent.Render(plain(latest+" available")))
		}
		return lines

	case stepAPI:
		lines := []string{plain(s.selectedDLL())}
		if i := s.dllCursor.Cursor(); i >= 0 && i < len(dllOptions) {
			lines = append(lines, faint(dllOptions[i].For))
		}
		if s.exe.API.Supported() {
			lines = append(lines, faint("detected "+apiLabel(s.exe.API)))
		}
		return lines

	case stepShaders:
		lines := s.hubSelectionLines(s.packages, env, "no shaders selected", width)
		return append(lines, s.hubOrphanLines(s.existingPackages(), s.packages, env, width)...)

	case stepAddons:
		lines := s.hubSelectionLines(s.addons, env, "no add-ons selected", width)
		return append(lines, s.hubOrphanLines(s.existingAddons(), s.addons, env, width)...)
	}
	return nil
}

// hubSelectionLines lists what is checked in one of the multi-selects,
// which is the whole point of showing panes rather than a count.
func (s *WizardScreen) hubSelectionLines(m multiSelect, env Env, empty string, width int) []string {
	var lines []string
	for _, it := range m.items {
		if !it.Header && m.selected[it.ID] {
			lines = append(lines, clipTail("  "+it.Name, width))
		}
	}
	if len(lines) == 0 {
		return []string{env.Styles.Faint.Render(clipTail("  "+empty, width))}
	}
	return lines
}

// hubOrphanLines names anything the recorded install uses that the loaded
// catalog has no entry for — upstream dropped it, renamed it, or the
// catalog is a partial offline copy. Without this the row simply is not
// there, and applying would quietly remove a package the user still has,
// with only a "-1 shader" on the Apply line to hint at it.
func (s *WizardScreen) hubOrphanLines(recorded []string, m multiSelect, env Env, width int) []string {
	known := make(map[string]bool, len(m.items))
	for _, it := range m.items {
		known[it.ID] = true
	}

	var out []string
	for _, id := range recorded {
		if !known[id] {
			out = append(out, env.Styles.Warn.Render(clipTail("  "+id+" — not in catalog", width)))
		}
	}
	return out
}

// existingPackages and existingAddons are what the recorded install used,
// or nothing when this is a fresh install.
func (s *WizardScreen) existingPackages() []string {
	if s.existing == nil {
		return nil
	}
	return s.existing.Packages
}

func (s *WizardScreen) existingAddons() []string {
	if s.existing == nil {
		return nil
	}
	return s.existing.Addons
}

// applyLine is the action under the panes: what applying would change, or
// that it would change nothing.
func (s *WizardScreen) applyLine(env Env, focused bool) string {
	changes := s.changes()
	text := "Apply — no changes yet; this would reinstall the same files"
	if len(changes) > 0 {
		text = "Apply — " + strings.Join(changes, ", ")
	}

	marker := "  "
	if focused {
		marker = "▸ "
	}
	line := clipTail(marker+text, env.Width)
	switch {
	case focused:
		line = env.Styles.Selected.Render(line)
	case len(changes) == 0:
		line = env.Styles.Faint.Render(line)
	default:
		line = env.Styles.Accent.Render(line)
	}
	return line + "\n"
}

// viewHubCompact is the narrow fallback: one line per section, the same
// content the panes carry with the listing dropped to a count.
func (s *WizardScreen) viewHubCompact(b *strings.Builder, env Env, height int, rows []wizardStep) {
	const labelWidth = 10
	sections := rows[:len(rows)-1]

	writeWindow(b, env, len(sections), s.hubCursor.Cursor(), height, "", func(i int) {
		step := sections[i]
		marker := "  "
		if i == s.hubCursor.Cursor() {
			marker = "▸ "
		}
		label := step.label()
		if s.sectionChanged(step) {
			label += " •"
		}
		line := clipTail(fmt.Sprintf("%s%-*s %s", marker, labelWidth, label, s.hubValue(step, nil)), env.Width)
		if i == s.hubCursor.Cursor() {
			line = env.Styles.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	})
	b.WriteString("\n")
}

// latestVersion returns the version the catalog marks as latest.
func (s *WizardScreen) latestVersion() (string, bool) {
	for _, v := range s.data.Versions {
		if v.Latest {
			return v.Version, true
		}
	}
	return "", false
}

// sectionChanged reports whether one section differs from the install
// already recorded, so its pane can say where a change is without the
// reader having to compare the Apply line against four panes.
func (s *WizardScreen) sectionChanged(step wizardStep) bool {
	if s.existing == nil {
		return false
	}
	switch step {
	case stepReShade:
		v, ok := s.selectedVersion()
		return (ok && v.Version != s.existing.ReShade.Version) || string(s.flavor) != s.existing.ReShade.Flavor
	case stepAPI:
		return s.selectedDLL() != s.existing.ReShade.DLL
	case stepShaders:
		return countDiff(s.existing.Packages, s.packages.selectedIDs()) != ""
	case stepAddons:
		return countDiff(s.existing.Addons, addonsForDownload(s.flavor, s.addons)) != ""
	}
	return false
}

// hubValue is what one summary row shows to the right of its label.
func (s *WizardScreen) hubValue(step wizardStep, changes []string) string {
	switch step {
	case stepReShade:
		v, _ := s.selectedVersion()
		return fmt.Sprintf("%s (%s)", v.Version, s.flavor)
	case stepAPI:
		return s.selectedDLL()
	case stepShaders:
		return namesOrCount(s.packages)
	case stepAddons:
		return namesOrCount(s.addons)
	case stepReview:
		if len(changes) == 0 {
			return "no changes — reinstalls the same files"
		}
		return strings.Join(changes, ", ")
	default:
		return ""
	}
}

// namesOrCount lists what is selected, falling back to a count once there
// are too many to read at a glance.
func namesOrCount(m multiSelect) string {
	var names []string
	for _, it := range m.items {
		if !it.Header && m.selected[it.ID] {
			names = append(names, it.Name)
		}
	}
	switch {
	case len(names) == 0:
		return "none"
	case len(names) <= 3:
		return strings.Join(names, ", ")
	default:
		return fmt.Sprintf("%s, +%d more", strings.Join(names[:2], ", "), len(names)-2)
	}
}

// changes describes how the current selection differs from the install
// already recorded for this folder — the thing an edit is actually about,
// and what Apply is going to do.
func (s *WizardScreen) changes() []string {
	if s.existing == nil {
		return nil
	}
	var out []string

	if v, ok := s.selectedVersion(); ok && v.Version != s.existing.ReShade.Version {
		out = append(out, fmt.Sprintf("%s → %s", displayVersion(s.existing.ReShade.Version), v.Version))
	}
	if string(s.flavor) != s.existing.ReShade.Flavor {
		out = append(out, "→ "+string(s.flavor)+" build")
	}
	if dll := s.selectedDLL(); dll != "" && dll != s.existing.ReShade.DLL {
		out = append(out, s.existing.ReShade.DLL+" → "+dll)
	}
	if n := countDiff(s.existing.Packages, s.packages.selectedIDs()); n != "" {
		out = append(out, n+" shader")
	}
	if n := countDiff(s.existing.Addons, addonsForDownload(s.flavor, s.addons)); n != "" {
		out = append(out, n+" add-on")
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

// countDiff summarizes an id-set change as "+2/-1", or "" when nothing
// moved. The exact ids are one keypress away in the section itself; what
// belongs on a summary line is whether anything changed at all.
func countDiff(before, after []string) string {
	had := make(map[string]bool, len(before))
	for _, id := range before {
		had[id] = true
	}
	has := make(map[string]bool, len(after))
	for _, id := range after {
		has[id] = true
	}

	added, removed := 0, 0
	for id := range has {
		if !had[id] {
			added++
		}
	}
	for id := range had {
		if !has[id] {
			removed++
		}
	}

	var parts []string
	if added > 0 {
		parts = append(parts, fmt.Sprintf("+%d", added))
	}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("-%d", removed))
	}
	return strings.Join(parts, "/")
}
