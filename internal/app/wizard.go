package app

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

// wizardStep is one page of the install wizard, in the order §6.1 lists
// them. There is no longer an "Exe" step: which executable (and which
// folder it lives in) is resolved by the screen that opened the wizard —
// the games list or the game detail screen — before it ever appears, so
// there is nothing left to ask about here.
type wizardStep int

const (
	stepVersion wizardStep = iota
	stepDLL
	stepPackages
	stepAddons
	stepReview
)

// label names a step for the breadcrumb.
func (s wizardStep) label() string {
	switch s {
	case stepVersion:
		return "Version"
	case stepDLL:
		return "API"
	case stepPackages:
		return "Shaders"
	case stepAddons:
		return "Add-ons"
	case stepReview:
		return "Review"
	default:
		return "?"
	}
}

// dllOption is one radio choice on the API/DLL step, matching ReShade's
// own installer (docs/plan/04-external-sources.md §4.6).
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
// and flavor, which proxy DLL (skipped when it can be inferred with
// confidence), which effect shaders and add-ons, then a review before
// running. The executable it targets is fixed at construction — decided
// by whichever screen opened the wizard — not something picked here.
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

	// existing is the install already recorded for exe, if any — the
	// wizard starts every step's selection from it instead of the
	// configured defaults, so re-running the wizard to add an add-on or
	// switch flavor does not silently reset everything else.
	existing *state.Install

	step    wizardStep
	loading bool
	loadErr string
	data    WizardData

	// Step: version + flavor
	flavor        install.Flavor
	versionCursor cursorList

	// Step: API/DLL — shown only when it cannot be inferred confidently
	// (see dllResolved).
	dllCursor cursorList

	// Steps: shaders, add-ons
	packages multiSelect
	addons   multiSelect

	// Step: review — a read-only summary plus an options checklist
	// (currently just "overwrite"), navigated the same way shaders/
	// add-ons are: up/down to move, space to toggle.
	reviewOptions multiSelect
}

// NewWizardScreen returns a wizard targeting exe. When exe already has a
// recorded install, the wizard edits it: every step starts from what is
// already installed (flavor, version, packages, add-ons) instead of the
// configured defaults, so running the wizard again to add an add-on or
// switch flavor does not silently reset everything else.
func NewWizardScreen(entry GameEntry, exe Executable, deps Deps) *WizardScreen {
	existing := exe.Installed

	flavor := install.FlavorAddon
	switch {
	case existing != nil:
		flavor = install.Flavor(existing.ReShade.Flavor)
	case deps.Defaults.ReshadeFlavor == string(install.FlavorNormal):
		flavor = install.FlavorNormal
	}

	return &WizardScreen{
		keys:     DefaultKeyMap(),
		entry:    entry,
		exe:      exe,
		deps:     deps,
		targetOS: artifacts.CurrentTargetOS(),
		existing: existing,
		loading:  true,
		flavor:   flavor,
		reviewOptions: newMultiSelect([]selectItem{
			{ID: "overwrite", Name: "Overwrite existing files"},
		}),
	}
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

// Title implements Screen.
func (s *WizardScreen) Title() string {
	verb := "install"
	if s.existing != nil {
		verb = "update"
	}
	return fmt.Sprintf("%s ReShade — %d %s", verb, s.step+1, s.step.label())
}

// KeyBindings implements Screen.
func (s *WizardScreen) KeyBindings() []key.Binding {
	switch s.step {
	case stepVersion:
		return []key.Binding{s.keys.Enter, s.keys.Flavor, s.keys.Back}
	case stepPackages, stepAddons:
		return []key.Binding{s.keys.Toggle, s.keys.Enter, s.keys.Back}
	case stepReview:
		return []key.Binding{s.keys.Up, s.keys.Down, s.keys.Toggle, s.keys.Enter, s.keys.Back}
	default:
		return []key.Binding{s.keys.Enter, s.keys.Back}
	}
}

// HandleBack implements backHandler: step back a page rather than leaving
// the wizard outright, except from the first page.
func (s *WizardScreen) HandleBack() (Screen, tea.Cmd, bool) {
	if s.step == stepVersion {
		return s, nil, false
	}
	s.step = s.prevStep(s.step)
	return s, nil, true
}

// dllResolved reports whether a DLL choice is already known with enough
// confidence to skip asking: either a recorded install already has one,
// or the target executable's guessed API maps to exactly one ReShade
// proxy DLL.
func (s *WizardScreen) dllResolved() bool {
	if s.existing != nil && s.existing.ReShade.DLL != "" {
		return true
	}
	return s.exe.API.RecommendedDLL() != ""
}

// prevStep steps backward, skipping API when it was resolved without
// asking and Add-ons when the flavor does not support it.
func (s *WizardScreen) prevStep(from wizardStep) wizardStep {
	prev := from - 1
	if prev == stepAddons && !s.flavor.Addon() {
		prev--
	}
	if prev == stepDLL && s.dllResolved() {
		prev--
	}
	return prev
}

// nextStep steps forward, skipping API when it was resolved without
// asking, and Add-ons when the flavor does not support it (§6.1: "skipped
// when flavor = normal").
func (s *WizardScreen) nextStep(from wizardStep) wizardStep {
	next := from + 1
	if next == stepDLL && s.dllResolved() {
		next++
	}
	if next == stepAddons && !s.flavor.Addon() {
		next++
	}
	return next
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
		versionIndex := s.indexOfLatest()
		if s.existing != nil {
			s.preselectExistingIDs(s.packages, s.existing.Packages)
			versionIndex = s.indexOfVersion(s.existing.ReShade.Version)
		} else {
			s.preselectDefaultPackages()
		}
		s.addons = newMultiSelect(s.data.AddonsWithCustom(s.deps.CacheStatus))
		if s.existing != nil {
			s.preselectExistingIDs(s.addons, s.existing.Addons)
		}
		s.versionCursor = newCursorList(len(s.data.Versions), versionIndex)
		s.dllCursor = newCursorList(len(dllOptions), s.recommendedDLLIndex())
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

// selectedVersion returns the version highlighted on the version step.
func (s *WizardScreen) selectedVersion() (catalog.Version, bool) {
	i := s.versionCursor.Cursor()
	if i < 0 || i >= len(s.data.Versions) {
		return catalog.Version{}, false
	}
	return s.data.Versions[i], true
}

// selectedDLL returns the DLL name highlighted on the API step (or
// resolved automatically when that step was skipped).
func (s *WizardScreen) selectedDLL() string {
	i := s.dllCursor.Cursor()
	if i < 0 || i >= len(dllOptions) {
		return ""
	}
	return dllOptions[i].Name
}

// overwrite reports whether the review's "overwrite existing files"
// option is checked.
func (s *WizardScreen) overwrite() bool {
	return s.reviewOptions.selected["overwrite"]
}

func (s *WizardScreen) handleKey(msg tea.KeyPressMsg, env Env) (Screen, tea.Cmd) {
	switch s.step {
	case stepVersion:
		return s.handleVersionKey(msg)
	case stepDLL:
		return s.handleDLLKey(msg)
	case stepPackages:
		return s.handleMultiSelectKey(msg, &s.packages, stepPackages)
	case stepAddons:
		return s.handleMultiSelectKey(msg, &s.addons, stepAddons)
	case stepReview:
		return s.handleReviewKey(msg)
	}
	return s, nil
}

func (s *WizardScreen) handleVersionKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Up):
		s.versionCursor.up()
	case key.Matches(msg, s.keys.Down):
		s.versionCursor.down()
	case key.Matches(msg, s.keys.Flavor):
		if s.flavor.Addon() {
			s.flavor = install.FlavorNormal
		} else {
			s.flavor = install.FlavorAddon
		}
	case key.Matches(msg, s.keys.Enter):
		if _, ok := s.selectedVersion(); ok {
			s.step = s.nextStep(s.step)
		}
	}
	return s, nil
}

func (s *WizardScreen) handleDLLKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Up):
		s.dllCursor.up()
	case key.Matches(msg, s.keys.Down):
		s.dllCursor.down()
	case key.Matches(msg, s.keys.Enter):
		if s.selectedDLL() != "" {
			s.step = s.nextStep(s.step)
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
		m.toggle()
	case key.Matches(msg, s.keys.Enter):
		s.step = s.nextStep(current)
	}
	return s, nil
}

func (s *WizardScreen) handleReviewKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Up):
		s.reviewOptions.up()
	case key.Matches(msg, s.keys.Down):
		s.reviewOptions.down()
	case key.Matches(msg, s.keys.Toggle):
		s.reviewOptions.toggle()
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
		// later switch back to the normal flavor: normal can't load them
		// at all, and install.Request.Validate rejects a request that
		// tries, so a stale selection here would silently block the
		// install rather than merely not installing an add-on.
		Addons:    addonsForDownload(s.flavor, s.addons),
		Overwrite: s.overwrite(),
		TargetOS:  s.targetOS,
	}, true
}

// View implements Screen.
func (s *WizardScreen) View(env Env) string {
	var b strings.Builder
	b.WriteString(s.breadcrumb(env))
	b.WriteString("\n\n")

	if s.loading {
		b.WriteString(env.Styles.Faint.Render("loading catalog data…"))
		return b.String()
	}
	if s.loadErr != "" {
		b.WriteString(env.Styles.Bad.Render(s.loadErr))
		return b.String()
	}

	switch s.step {
	case stepVersion:
		s.viewVersion(&b, env)
	case stepDLL:
		s.viewDLL(&b, env)
	case stepPackages:
		s.viewMultiSelect(&b, env, s.packages, "Select shaders.")
	case stepAddons:
		s.viewMultiSelect(&b, env, s.addons, "Select add-ons.")
	case stepReview:
		s.viewReview(&b, env)
	}
	return b.String()
}

func (s *WizardScreen) breadcrumb(env Env) string {
	steps := []wizardStep{stepVersion, stepDLL, stepPackages, stepAddons, stepReview}
	var parts []string
	for i, st := range steps {
		label := fmt.Sprintf("%d %s", i+1, st.label())
		switch {
		case st == stepAddons && !s.flavor.Addon():
			parts = append(parts, env.Styles.Faint.Render(label+" (skipped)"))
			continue
		case st == stepDLL && s.dllResolved():
			parts = append(parts, env.Styles.Faint.Render(label+" (skipped)"))
			continue
		}
		if st == s.step {
			parts = append(parts, env.Styles.Accent.Render(label))
		} else {
			parts = append(parts, env.Styles.Faint.Render(label))
		}
	}
	return strings.Join(parts, " › ")
}

func (s *WizardScreen) viewVersion(b *strings.Builder, env Env) {
	b.WriteString("flavor: ")
	if s.flavor.Addon() {
		b.WriteString(env.Styles.Accent.Render("addon"))
	} else {
		b.WriteString(env.Styles.Accent.Render("normal"))
	}
	b.WriteString(env.Styles.Faint.Render("  (tab to toggle)"))
	b.WriteString("\n")
	b.WriteString(env.Styles.Faint.Render("DLL: " + dllWithCoverage(s.selectedDLL())))
	b.WriteString("\n")
	if s.flavor.Addon() {
		b.WriteString(env.Styles.Bad.Render(anticheatWarning))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if len(s.data.Versions) == 0 {
		b.WriteString(env.Styles.Faint.Render("No versions available."))
		return
	}
	for i, v := range s.data.Versions {
		marker := "  "
		if i == s.versionCursor.Cursor() {
			marker = "▸ "
		}
		badges := ""
		if v.Latest {
			badges += "  latest"
		}
		if s.deps.CacheStatus != nil && s.deps.CacheStatus.HasReShade(v.Version, s.flavor.Addon()) {
			badges += "  cached"
		}
		line := marker + v.Version + badges
		if i == s.versionCursor.Cursor() {
			b.WriteString(env.Styles.Selected.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

func (s *WizardScreen) viewDLL(b *strings.Builder, env Env) {
	if !s.exe.API.Supported() {
		b.WriteString(env.Styles.Warn.Render(
			fmt.Sprintf("%s is not supported in v1 — pick a DLL manually.", s.exe.API)))
		b.WriteString("\n\n")
	}
	for i, o := range dllOptions {
		marker := "  "
		if i == s.dllCursor.Cursor() {
			marker = "▸ "
		}
		line := fmt.Sprintf("%s%-14s %s", marker, o.Name, o.For)
		if i == s.dllCursor.Cursor() {
			b.WriteString(env.Styles.Selected.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

func (s *WizardScreen) viewMultiSelect(b *strings.Builder, env Env, m multiSelect, hint string) {
	b.WriteString(env.Styles.Faint.Render(hint + " space toggles, enter continues."))
	b.WriteString("\n\n")

	for i, it := range m.items {
		if it.Header {
			b.WriteString(env.Styles.Faint.Render(it.Name))
			b.WriteString("\n")
			continue
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
		switch {
		case it.Disabled:
			line = env.Styles.Faint.Render(line + "  — " + it.DisabledNote)
		case i == m.cursor:
			line = env.Styles.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
		if it.Description != "" && i == m.cursor {
			b.WriteString(env.Styles.Faint.Render("    " + it.Description))
			b.WriteString("\n")
		}
	}
}

func (s *WizardScreen) viewReview(b *strings.Builder, env Env) {
	version, _ := s.selectedVersion()

	section := func(title string) {
		b.WriteString(env.Styles.Subtitle.Render(title))
		b.WriteString("\n")
	}

	section("Folder")
	_, _ = fmt.Fprintf(b, "  %s\n\n", s.exe.Path)

	section("ReShade")
	_, _ = fmt.Fprintf(b, "  %s (%s) → %s\n", version.Version, s.flavor, dllWithCoverage(s.selectedDLL()))
	if s.flavor.Addon() {
		b.WriteString("  ")
		b.WriteString(env.Styles.Bad.Render(anticheatWarning))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	section("Shaders")
	if ids := s.packages.selectedIDs(); len(ids) > 0 {
		b.WriteString("  " + strings.Join(ids, ", ") + "\n")
	} else {
		b.WriteString(env.Styles.Faint.Render("  none selected") + "\n")
	}
	b.WriteString("\n")

	if s.flavor.Addon() {
		section("Add-ons")
		if ids := s.addons.selectedIDs(); len(ids) > 0 {
			b.WriteString("  " + strings.Join(ids, ", ") + "\n")
		} else {
			b.WriteString(env.Styles.Faint.Render("  none selected") + "\n")
		}
		b.WriteString("\n")
	}

	needsD3D := artifacts.NeedsD3DCompiler(s.targetOS)
	missing := describeMissing(s.deps.CacheStatus, version.Version, s.flavor.Addon(),
		s.packages.selectedIDs(), addonsForDownload(s.flavor, s.addons), s.exe.Arch, needsD3D)
	if len(missing) > 0 {
		section("To download")
		for _, m := range missing {
			b.WriteString("  " + m + "\n")
		}
		b.WriteString("\n")
	}

	section("Options")
	for i, it := range s.reviewOptions.items {
		box := "[ ]"
		if s.reviewOptions.isSelected(i) {
			box = "[x]"
		}
		marker := "  "
		if i == s.reviewOptions.cursor {
			marker = "▸ "
		}
		line := fmt.Sprintf("%s%s %s", marker, box, it.Name)
		if i == s.reviewOptions.cursor {
			line = env.Styles.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(env.Styles.Faint.Render("  space toggles"))
	b.WriteString("\n\n")

	if !s.overwrite() {
		b.WriteString(env.Styles.Warn.Render(
			"Files not created by yarm are left in place unless overwrite is on."))
		b.WriteString("\n\n")
	}

	verb := "install"
	if s.existing != nil {
		verb = "update"
	}
	b.WriteString(env.Styles.Accent.Render("enter to " + verb))
}

// addonsForDownload returns the selected add-on ids, or none when the
// flavor does not support add-ons at all.
func addonsForDownload(flavor install.Flavor, addons multiSelect) []string {
	if !flavor.Addon() {
		return nil
	}
	return addons.selectedIDs()
}
