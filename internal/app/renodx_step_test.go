package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

// typeInto sends each rune of s to the wizard, as typing does.
func typeInto(t *testing.T, s *WizardScreen, text string) *WizardScreen {
	t.Helper()
	for _, r := range text {
		next, _ := s.Update(tea.KeyPressMsg{Code: r, Text: string(r)}, wizardEnv())
		s = next.(*WizardScreen)
	}
	return s
}

// rowNames lists what the RenoDX step currently shows.
func renodxRowNames(s *WizardScreen) []string {
	out := make([]string, 0, len(s.renodx.items))
	for _, it := range s.renodx.items {
		out = append(out, it.Name)
	}
	return out
}

func renodxRow(t *testing.T, s *WizardScreen, name string) selectItem {
	t.Helper()
	for _, it := range s.renodx.items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("no row %q in %v", name, renodxRowNames(s))
	return selectItem{}
}

// The step belongs to the add-on build, like the add-ons step: the normal
// build cannot load a RenoDX add-on at all.
func TestRenoDXStepOnlyExistsForTheAddonBuild(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	if !s.stepEnabled(stepRenoDX) {
		t.Fatal("the add-on build should reach the RenoDX step")
	}

	s = pressSpecial(t, s, tea.KeyLeft) // switch to the normal build
	if s.stepEnabled(stepRenoDX) {
		t.Error("the normal build should skip the RenoDX step")
	}

	// And a step back from Review must cross both skipped steps at once,
	// which the old single-decrement walk could not do.
	if got := s.prevStep(stepReview); got != stepShaders {
		t.Errorf("prevStep(Review) on the normal build = %v, want stepShaders", got)
	}
	if got := s.nextStep(stepShaders); got != stepReview {
		t.Errorf("nextStep(Shaders) on the normal build = %v, want stepReview", got)
	}
}

// The mod upstream says belongs to this game leads the list, so the
// common case is answered before it is asked.
func TestRenoDXStepLeadsWithTheModForThisGame(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepRenoDX)

	names := renodxRowNames(s)
	want := []string{"No RenoDX mod", "── For this game ──", "Ember Hollow", "── All mods ──"}
	for i, w := range want {
		if i >= len(names) || names[i] != w {
			t.Fatalf("rows = %v, want them to start %v", names, want)
		}
	}
	if note := renodxRow(t, s, "Ember Hollow").Note; note != "matches this game" {
		t.Errorf("matched row note = %q", note)
	}
}

// Surfaced, never ticked. The add-on build is the one anti-cheat detects
// and the wizard says so on every screen that matters; adding a second
// injected DLL on the user's behalf would contradict that.
func TestRenoDXStepDoesNotChooseForYou(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepRenoDX)

	if s.renodxChoice != "" {
		t.Errorf("renodxChoice = %q before the user chose anything", s.renodxChoice)
	}
	req, ok := s.buildRequest()
	if !ok {
		t.Fatal("buildRequest() ok = false")
	}
	if req.RenoDX != "" {
		t.Errorf("Request.RenoDX = %q, want empty until it is chosen", req.RenoDX)
	}
}

func TestRenoDXStepChoosesOneModAtATime(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepRenoDX)

	s.renodx.cursor = indexOfRow(t, s, "Ember Hollow")
	s = pressSpecial(t, s, ' ')
	if s.renodxChoice != "emberhollow" {
		t.Fatalf("renodxChoice = %q, want emberhollow", s.renodxChoice)
	}

	// A second choice replaces the first rather than adding to it.
	s.renodx.cursor = indexOfRow(t, s, "Wobbly Life")
	s = pressSpecial(t, s, ' ')
	if s.renodxChoice != "wobbly" {
		t.Fatalf("renodxChoice = %q, want wobbly", s.renodxChoice)
	}
	if s.renodx.selected["emberhollow"] {
		t.Error("the first mod is still selected; two would fight over the same swap chain")
	}

	// And "no mod" is a real answer, reachable in one keypress.
	s.renodx.cursor = indexOfRow(t, s, "No RenoDX mod")
	s = pressSpecial(t, s, ' ')
	if s.renodxChoice != "" {
		t.Errorf("renodxChoice = %q, want empty after choosing No RenoDX mod", s.renodxChoice)
	}
}

func indexOfRow(t *testing.T, s *WizardScreen, name string) int {
	t.Helper()
	for i, it := range s.renodx.items {
		if it.Name == name {
			return i
		}
	}
	t.Fatalf("no row %q in %v", name, renodxRowNames(s))
	return -1
}

func TestRenoDXSearchNarrowsTheList(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepRenoDX)

	s = pressSpecial(t, s, '/')
	if !s.renodxFiltering {
		t.Fatal("'/' should open the search")
	}
	s = typeInto(t, s, "wob")

	names := renodxRowNames(s)
	if len(names) != 1 || names[0] != "Wobbly Life" {
		t.Errorf("rows = %v, want just Wobbly Life", names)
	}

	// esc clears the query and restores everything, including the
	// headings the search dropped.
	s = pressSpecial(t, s, tea.KeyEsc)
	if s.renodxFiltering {
		t.Error("esc should close the search")
	}
	if got := len(renodxRowNames(s)); got <= 1 {
		t.Errorf("after clearing, %d rows; want the whole list back", got)
	}
}

// The answer is held in renodxChoice, not read back from the visible
// rows — so a search that hides the chosen mod must not unanswer the step.
func TestRenoDXSearchDoesNotUnchooseAHiddenMod(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepRenoDX)
	s.renodx.cursor = indexOfRow(t, s, "Ember Hollow")
	s = pressSpecial(t, s, ' ')

	s = pressSpecial(t, s, '/')
	s = typeInto(t, s, "zzz-matches-nothing")

	if got := len(renodxRowNames(s)); got != 0 {
		t.Fatalf("search matched %d rows, want none — the setup needs a query that hides everything", got)
	}
	req, ok := s.buildRequest()
	if !ok {
		t.Fatal("buildRequest() ok = false")
	}
	if req.RenoDX != "emberhollow" {
		t.Errorf("Request.RenoDX = %q; a search hid the choice and lost it", req.RenoDX)
	}
}

// While the search is open the screen takes every key, so "q" types
// rather than quitting and "esc" closes the search rather than stepping
// the wizard back. The shell checks CapturesInput before its own
// bindings, which is what makes that possible.
func TestRenoDXSearchCapturesInput(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepRenoDX)

	if s.CapturesInput() {
		t.Fatal("the wizard captures input before the search is open")
	}
	s = pressSpecial(t, s, '/')
	if !s.CapturesInput() {
		t.Fatal("the wizard must capture input while the search is open")
	}

	s = typeInto(t, s, "q")
	if got := s.renodxFilter.Value(); got != "q" {
		t.Errorf("filter value = %q; 'q' should type, not quit", got)
	}

	// esc closes the search; a second esc is the wizard's own back.
	s = pressSpecial(t, s, tea.KeyEsc)
	if s.CapturesInput() {
		t.Error("esc should close the search")
	}
	if s.step != stepRenoDX {
		t.Errorf("step = %v; the first esc should close the search, not leave the step", s.step)
	}
	_, _, handled := s.HandleBack()
	if !handled || s.step != stepAddons {
		t.Errorf("a second esc should step back: handled=%v step=%v", handled, s.step)
	}
}

// A mod with no build for this game's architecture is inert, not
// degraded: the loader rejects it and nothing surfaces. So it is refused
// here, with the reason on the row.
func TestRenoDXRefusesTheWrongArchitecture(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepRenoDX)

	row := renodxRow(t, s, "Old Game") // 32-bit only; the sample exe is x64
	if !row.Disabled {
		t.Fatal("a mod with no build for this architecture should be disabled")
	}
	if !strings.Contains(row.DisabledNote, "x64") {
		t.Errorf("note = %q, want it to name the missing architecture", row.DisabledNote)
	}

	s.renodx.cursor = indexOfRow(t, s, "Old Game")
	s = pressSpecial(t, s, ' ')
	if s.renodxChoice != "" {
		t.Errorf("renodxChoice = %q; a disabled row must not be choosable", s.renodxChoice)
	}
}

func TestRenoDXRefusesVulkan(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepRenoDX)

	row := renodxRow(t, s, "Vulkan Game")
	if !row.Disabled || !strings.Contains(row.DisabledNote, "Vulkan") {
		t.Errorf("Vulkan row = %+v, want it disabled and saying why", row)
	}
}

// RenoDX documents ReShade 6.8.0 as its floor. Reported on Review rather
// than blocked: the version was chosen four steps earlier and is still
// changeable.
func TestRenoDXReportsATooOldReShade(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = pressSpecial(t, s, tea.KeyDown) // 6.7.3, below the floor
	s = advance(t, s, stepRenoDX)
	s.renodx.cursor = indexOfRow(t, s, "Ember Hollow")
	s = pressSpecial(t, s, ' ')

	unmet := strings.Join(s.unmet(), "\n")
	if !strings.Contains(unmet, catalog.RenoDXMinReShade) {
		t.Errorf("Check block = %q, want the 6.8.0 requirement named", unmet)
	}

	// The step itself says so too, so it is not a surprise on Review.
	body := s.View(wizardEnv())
	if !strings.Contains(body, catalog.RenoDXMinReShade) {
		t.Errorf("the step should warn about the version:\n%s", body)
	}
}

// Both replace the game's tone mapping, so running them together gives
// whichever loads second rather than a blend.
func TestRenoDXReportsTheAutoHDRConflict(t *testing.T) {
	deps := fakeDeps()
	data := sampleWizardData()
	data.Addons = append(data.Addons, catalog.Addon{
		ID: autoHDRAddonID, Name: "AutoHDR", URL64: "https://example.invalid/autohdr.addon64",
	})
	deps.WizardData = fakeWizardData{data: data}

	s := loadWizard(t, sampleGameEntry(), 0, deps)
	s = advance(t, s, stepRenoDX)
	s.renodx.cursor = indexOfRow(t, s, "Ember Hollow")
	s = pressSpecial(t, s, ' ')
	s.addons.selected[autoHDRAddonID] = true

	if unmet := strings.Join(s.unmet(), "\n"); !strings.Contains(unmet, "tone mapping") {
		t.Errorf("Check block = %q, want the AutoHDR conflict reported", unmet)
	}
}

// A choice made while the add-on build was on offer must not survive a
// switch back: the normal build cannot load it and Validate would reject
// the whole request.
func TestRenoDXChoiceDroppedOnTheNormalBuild(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepRenoDX)
	s.renodx.cursor = indexOfRow(t, s, "Ember Hollow")
	s = pressSpecial(t, s, ' ')

	s.step = stepReShade
	s = pressSpecial(t, s, tea.KeyLeft) // back to the normal build

	req, ok := s.buildRequest()
	if !ok {
		t.Fatal("buildRequest() ok = false")
	}
	if req.RenoDX != "" {
		t.Errorf("Request.RenoDX = %q on the normal build", req.RenoDX)
	}
	if err := req.Validate(); err != nil {
		t.Errorf("Validate() = %v, want a request the normal build accepts", err)
	}
}

// RenoDX comes from a different project on a different host. Its being
// unreachable must not stop the wizard installing ReShade.
func TestRenoDXUnavailableStillLetsTheInstallProceed(t *testing.T) {
	deps := fakeDeps()
	data := sampleWizardData()
	data.RenoDX = nil
	deps.WizardData = fakeWizardData{data: data}

	s := loadWizard(t, sampleGameEntry(), 0, deps)
	s = advance(t, s, stepRenoDX)

	if body := s.View(wizardEnv()); !strings.Contains(body, "could not be loaded") {
		t.Errorf("the step should say the list is unavailable:\n%s", body)
	}
	s = pressSpecial(t, s, tea.KeyEnter)
	if s.step != stepReview {
		t.Errorf("step = %v, want the wizard to carry on to Review", s.step)
	}
}

// Editing an install starts from what is recorded, this step included.
func TestRenoDXPreselectsTheRecordedMod(t *testing.T) {
	entry := sampleGameEntry()
	entry.Exes[0].Installed = &state.Install{
		Exe:      entry.Exes[0].Path,
		ReShade:  state.ReShadeInfo{Version: "6.8.0", Flavor: "addon", DLL: "dxgi.dll"},
		RenoDX:   "emberhollow",
		Packages: []string{"standard-effects"},
	}

	s := loadWizard(t, entry, 0, fakeDeps())
	if s.renodxChoice != "emberhollow" {
		t.Fatalf("renodxChoice = %q, want the recorded mod", s.renodxChoice)
	}
	if s.sectionChanged(stepRenoDX) {
		t.Error("an unchanged section should not be marked changed")
	}
	if got := s.hubValue(stepRenoDX, nil); !strings.Contains(got, "Ember Hollow") {
		t.Errorf("hub value = %q, want the mod named", got)
	}
}

func TestRenoDXSummaryNamesTheChoice(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepRenoDX)

	if got := s.renodxSummary(); got != "no RenoDX" {
		t.Errorf("summary with nothing chosen = %q", got)
	}
	s.renodx.cursor = indexOfRow(t, s, "Ember Hollow")
	s = pressSpecial(t, s, ' ')
	if got := s.renodxSummary(); !strings.Contains(got, "Ember Hollow") {
		t.Errorf("summary = %q, want the mod named", got)
	}
}

// matchRenoDX is the whole reason the step can lead with an answer, and
// it has to be quiet about every id that is not a Steam one.
func TestMatchRenoDX(t *testing.T) {
	mods := []catalog.RenoMod{
		{ID: "a", SteamAppID: 700110},
		{ID: "b", SteamAppID: 0},
	}
	tests := []struct {
		gameID string
		want   string
	}{
		{"steam:700110", "a"},
		{"steam:999999", ""},
		{"manual:abc123", ""},
		{"steam:", ""},
		{"steam:notanumber", ""},
		{"", ""},
	}
	for _, tt := range tests {
		m, ok := matchRenoDX(mods, tt.gameID)
		switch {
		case tt.want == "" && ok:
			t.Errorf("matchRenoDX(%q) matched %q, want no match", tt.gameID, m.ID)
		case tt.want != "" && m.ID != tt.want:
			t.Errorf("matchRenoDX(%q) = %q, want %q", tt.gameID, m.ID, tt.want)
		}
	}
	// A mod without an app id must never match a game without one either.
	if _, ok := matchRenoDX(mods, "steam:0"); ok {
		t.Error("app id 0 should not match the mod that has none")
	}
}

// The install engine refuses a RenoDX mod on a build that cannot load it,
// which is the backstop behind renodxForDownload.
func TestRequestValidateRejectsRenoDXOnTheNormalBuild(t *testing.T) {
	req := install.Request{
		Game: game.Game{Root: "/games/x"}, Exe: game.Executable{Path: "x.exe"},
		Version: "6.8.0", Flavor: install.FlavorNormal, DLLName: "dxgi.dll",
		RenoDX: "emberhollow",
	}
	if err := req.Validate(); err == nil {
		t.Fatal("Validate() = nil, want RenoDX refused on the normal build")
	}
}

// Adding a step adds a hub pane, and the hub stacks below
// panes*(minPaneWidth+gutter). On the add-on build that threshold moved
// from 96 columns to 120, so a 100-column terminal in edit mode now gets
// the stacked list instead of panes. That is a real change in what people
// see, pinned here so it stays a decision rather than a bug report.
func TestHubStacksBelow120ColumnsOnTheAddonBuild(t *testing.T) {
	entry := sampleGameEntry()
	entry.Exes[0].Installed = &state.Install{
		Exe:     entry.Exes[0].Path,
		ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "addon", DLL: "dxgi.dll"},
	}
	s := loadWizard(t, entry, 0, fakeDeps())
	if s.step != stepHub {
		t.Fatalf("step = %v, want the edit-mode summary", s.step)
	}
	if got := len(s.hubSections()); got != 7 {
		t.Fatalf("hubSections = %d, want 7 on the add-on build", got)
	}

	for _, tt := range []struct {
		width   int
		stacked bool
	}{{130, true}, {143, true}, {144, false}, {150, false}} {
		got := s.hubStacked(Env{Styles: NewStyles(true), Width: tt.width, Height: 24})
		if got != tt.stacked {
			t.Errorf("hubStacked at %d columns = %v, want %v", tt.width, got, tt.stacked)
		}
	}
}
