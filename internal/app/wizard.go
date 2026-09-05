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
)

// wizardStep is one page of the install wizard, in the order §6.1 lists
// them.
type wizardStep int

const (
	stepExe wizardStep = iota
	stepVersion
	stepDLL
	stepPackages
	stepAddons
	stepReview
)

// label names a step for the breadcrumb.
func (s wizardStep) label() string {
	switch s {
	case stepExe:
		return "Exe"
	case stepVersion:
		return "Version"
	case stepDLL:
		return "API"
	case stepPackages:
		return "Packages"
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

// wizardDataLoadedMsg carries the catalog/custom data the wizard needs
// once it has loaded.
type wizardDataLoadedMsg struct{ data WizardData }

// WizardScreen walks the user through installing ReShade into one
// executable: which exe, which ReShade version and flavor, which proxy
// DLL, which effect packages and add-ons, then a review before running.
//
// It is one Screen implementation holding a step index and one field group
// per step, rather than six pushed screens, so the in-progress Request
// lives in exactly one place (§6.1: "wizard holds Request").
type WizardScreen struct {
	keys     KeyMap
	entry    GameEntry
	deps     Deps
	targetOS artifacts.TargetOS

	step    wizardStep
	loading bool
	loadErr string
	data    WizardData

	// Step 1: exe
	exes        []Executable
	showAllExes bool
	exeCursor   cursorList

	// Step 2: version
	flavor        install.Flavor
	versionCursor cursorList

	// Step 3: API/DLL
	dllCursor     cursorList
	dllUserPicked bool

	// Step 4/5: packages, add-ons
	packages multiSelect
	addons   multiSelect

	// Step 6: review
	overwrite bool
}

// NewWizardScreen returns a wizard for entry, with preselected marking
// which executable (by index into entry.PlayableExes()) was highlighted
// when the wizard was opened.
func NewWizardScreen(entry GameEntry, preselected int, deps Deps) *WizardScreen {
	exes := entry.PlayableExes()

	flavor := install.FlavorAddon
	if deps.Defaults.ReshadeFlavor == string(install.FlavorNormal) {
		flavor = install.FlavorNormal
	}

	return &WizardScreen{
		keys:      DefaultKeyMap(),
		entry:     entry,
		deps:      deps,
		targetOS:  artifacts.CurrentTargetOS(),
		loading:   true,
		exes:      exes,
		exeCursor: newCursorList(len(exes), preselected),
		flavor:    flavor,
	}
}

// Init implements Screen.
func (s *WizardScreen) Init() tea.Cmd {
	if s.deps.WizardData == nil {
		return nil
	}
	return Async(context.Background(),
		func(ctx context.Context) (WizardData, error) { return s.deps.WizardData.LoadWizardData(ctx) },
		func(d WizardData) tea.Msg { return wizardDataLoadedMsg{data: d} },
	)
}

// Title implements Screen.
func (s *WizardScreen) Title() string {
	return fmt.Sprintf("install ReShade — %d %s", s.step+1, s.step.label())
}

// KeyBindings implements Screen.
func (s *WizardScreen) KeyBindings() []key.Binding {
	switch s.step {
	case stepVersion:
		return []key.Binding{s.keys.Enter, s.keys.Flavor, s.keys.Back}
	case stepPackages, stepAddons:
		return []key.Binding{s.keys.Toggle, s.keys.Enter, s.keys.Back}
	case stepReview:
		return []key.Binding{s.keys.Enter, s.keys.Overwrite, s.keys.Back}
	case stepExe:
		return []key.Binding{s.keys.Enter, showAllBinding, s.keys.Back}
	default:
		return []key.Binding{s.keys.Enter, s.keys.Back}
	}
}

// HandleBack implements backHandler: step back a page rather than leaving
// the wizard outright, except from the first page.
func (s *WizardScreen) HandleBack() (Screen, tea.Cmd, bool) {
	if s.step == stepExe {
		return s, nil, false
	}
	s.step = s.prevStep(s.step)
	return s, nil, true
}

// prevStep steps backward, skipping Add-ons when the flavor does not
// support it.
func (s *WizardScreen) prevStep(from wizardStep) wizardStep {
	prev := from - 1
	if prev == stepAddons && !s.flavor.Addon() {
		prev--
	}
	return prev
}

// nextStep steps forward, skipping Add-ons when the flavor does not
// support it (§6.1: "skipped when flavor = normal").
func (s *WizardScreen) nextStep(from wizardStep) wizardStep {
	next := from + 1
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
		s.packages = newMultiSelect(s.data.PackagesWithCustom(s.deps.CacheStatus))
		s.preselectDefaultPackages()
		s.addons = newMultiSelect(s.data.AddonsWithCustom(s.deps.CacheStatus))
		s.versionCursor = newCursorList(len(s.data.Versions), s.indexOfLatest())
		s.dllCursor = newCursorList(len(dllOptions), s.recommendedDLLIndex())
		if len(s.data.Versions) == 0 && len(s.data.Packages) == 0 && len(s.data.Addons) == 0 {
			s.loadErr = "no catalog data available (offline, with nothing cached yet?)"
		}
		return s, nil

	case tea.KeyPressMsg:
		return s.handleKey(msg, env)
	}
	return s, nil
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

// indexOfLatest returns the position of the version marked Latest, or 0.
func (s *WizardScreen) indexOfLatest() int {
	for i, v := range s.data.Versions {
		if v.Latest {
			return i
		}
	}
	return 0
}

// recommendedDLLIndex returns the dllOptions index matching the selected
// exe's guessed API, or 0 (dxgi.dll) when there is no safe recommendation.
func (s *WizardScreen) recommendedDLLIndex() int {
	exe, ok := s.selectedExe()
	if !ok {
		return 0
	}
	rec := exe.API.RecommendedDLL()
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

// selectedExe returns the executable highlighted on the exe step.
func (s *WizardScreen) selectedExe() (Executable, bool) {
	i := s.exeCursor.Cursor()
	if i < 0 || i >= len(s.exes) {
		return Executable{}, false
	}
	return s.exes[i], true
}

// selectedVersion returns the version highlighted on the version step.
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

func (s *WizardScreen) handleKey(msg tea.KeyPressMsg, env Env) (Screen, tea.Cmd) {
	switch s.step {
	case stepExe:
		return s.handleExeKey(msg)
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

func (s *WizardScreen) handleExeKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, s.keys.Up):
		s.exeCursor.up()
	case key.Matches(msg, s.keys.Down):
		s.exeCursor.down()
	case key.Matches(msg, showAllBinding):
		s.showAllExes = !s.showAllExes
		if s.showAllExes {
			s.exes = s.entry.Exes
		} else {
			s.exes = s.entry.PlayableExes()
		}
		s.exeCursor.setCount(len(s.exes))
	case key.Matches(msg, s.keys.Enter):
		if _, ok := s.selectedExe(); ok {
			// The recommendation follows the selected exe's guessed API
			// until the user overrides it themselves; once they have, a
			// trip back to the exe step should not discard that choice.
			if !s.dllUserPicked {
				s.dllCursor = newCursorList(len(dllOptions), s.recommendedDLLIndex())
			}
			s.step = s.nextStep(s.step)
		}
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
		s.dllUserPicked = true
	case key.Matches(msg, s.keys.Down):
		s.dllCursor.down()
		s.dllUserPicked = true
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
	case key.Matches(msg, s.keys.Overwrite):
		s.overwrite = !s.overwrite
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
	exe, ok := s.selectedExe()
	if !ok {
		return install.Request{}, false
	}
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
		Exe:      exe.Executable,
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
		Overwrite: s.overwrite,
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
	if s.loadErr != "" && s.step != stepExe {
		b.WriteString(env.Styles.Bad.Render(s.loadErr))
		return b.String()
	}

	switch s.step {
	case stepExe:
		s.viewExe(&b, env)
	case stepVersion:
		s.viewVersion(&b, env)
	case stepDLL:
		s.viewDLL(&b, env)
	case stepPackages:
		s.viewMultiSelect(&b, env, s.packages, "Select effect packages.")
	case stepAddons:
		s.viewMultiSelect(&b, env, s.addons, "Select add-ons.")
	case stepReview:
		s.viewReview(&b, env)
	}
	return b.String()
}

func (s *WizardScreen) breadcrumb(env Env) string {
	steps := []wizardStep{stepExe, stepVersion, stepDLL, stepPackages, stepAddons, stepReview}
	var parts []string
	for i, st := range steps {
		label := fmt.Sprintf("%d %s", i+1, st.label())
		if st == stepAddons && !s.flavor.Addon() {
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

func (s *WizardScreen) viewExe(b *strings.Builder, env Env) {
	if len(s.exes) == 0 {
		b.WriteString(env.Styles.Faint.Render("No executables to install into."))
		return
	}
	for i, e := range s.exes {
		marker := "  "
		if i == s.exeCursor.Cursor() {
			marker = "▸ "
		}
		status := ""
		if e.Installed != nil {
			status = "  ✓ installed"
		}
		line := fmt.Sprintf("%s%s  %s · %s%s", marker, e.Path, e.Arch, e.API, status)
		if i == s.exeCursor.Cursor() {
			b.WriteString(env.Styles.Selected.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

func (s *WizardScreen) viewVersion(b *strings.Builder, env Env) {
	b.WriteString("flavor: ")
	if s.flavor.Addon() {
		b.WriteString(env.Styles.Accent.Render("addon"))
	} else {
		b.WriteString(env.Styles.Accent.Render("normal"))
	}
	b.WriteString(env.Styles.Faint.Render("  (tab to toggle)"))
	b.WriteString("\n\n")

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
	if exe, ok := s.selectedExe(); ok && !exe.API.Supported() {
		b.WriteString(env.Styles.Warn.Render(
			fmt.Sprintf("%s is not supported in v1 — pick a DLL manually.", exe.API)))
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
		if it.Cached {
			name += "  cached"
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
	exe, _ := s.selectedExe()
	version, _ := s.selectedVersion()

	_, _ = fmt.Fprintf(b, "%s\n", exe.Path)
	_, _ = fmt.Fprintf(b, "ReShade %s (%s) → %s\n", version.Version, s.flavor, s.selectedDLL())

	if ids := s.packages.selectedIDs(); len(ids) > 0 {
		b.WriteString("packages: " + strings.Join(ids, ", ") + "\n")
	}
	if s.flavor.Addon() {
		if ids := s.addons.selectedIDs(); len(ids) > 0 {
			b.WriteString("add-ons: " + strings.Join(ids, ", ") + "\n")
		}
	}

	b.WriteString("\n")
	overwrite := "off"
	if s.overwrite {
		overwrite = "on"
	}
	b.WriteString(env.Styles.Faint.Render(
		fmt.Sprintf("overwrite existing files: %s  (o to toggle)", overwrite)))
	b.WriteString("\n\n")

	needsD3D := artifacts.NeedsD3DCompiler(s.targetOS)
	missing := describeMissing(s.deps.CacheStatus, version.Version, s.flavor.Addon(),
		s.packages.selectedIDs(), addonsForDownload(s.flavor, s.addons), exe.Arch, needsD3D)
	if len(missing) > 0 {
		b.WriteString(env.Styles.Subtitle.Render("To download:"))
		b.WriteString("\n")
		for _, m := range missing {
			b.WriteString("  " + m + "\n")
		}
		b.WriteString("\n")
	}

	if !s.overwrite {
		b.WriteString(env.Styles.Faint.Render(
			"Files not created by yarm are left in place unless overwrite is on."))
		b.WriteString("\n\n")
	}

	b.WriteString(env.Styles.Accent.Render("enter to install"))
}

// addonsForDownload returns the selected add-on ids, or none when the
// flavor does not support add-ons at all.
func addonsForDownload(flavor install.Flavor, addons multiSelect) []string {
	if !flavor.Addon() {
		return nil
	}
	return addons.selectedIDs()
}
