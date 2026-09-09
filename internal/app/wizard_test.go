package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/config"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

func wizardEnv() Env {
	return Env{Styles: NewStyles(true), Width: 100, Height: 30}
}

// sampleGameEntry is a two-executable game used across wizard tests: one
// x64/D3D12 exe (a clean RecommendedDLL case) and one x86/Vulkan exe (the
// unsupported-API case).
func sampleGameEntry() GameEntry {
	return GameEntry{
		Game: game.Game{ID: "steam:700110", Name: "Ember Hollow", Provider: "steam", Root: "/games/EH"},
		Exes: []Executable{
			{Executable: game.Executable{Path: "Game/emberhollow.exe", Arch: game.ArchX64, API: game.APID3D12}},
			{Executable: game.Executable{Path: "Game/launcher.exe", Arch: game.ArchX86, API: game.APIVulkan}},
		},
	}
}

// press drives one printable key through the wizard's Update.
func press(t *testing.T, s *WizardScreen, key rune) *WizardScreen {
	t.Helper()
	msg := tea.KeyPressMsg{Code: key, Text: string(key)}
	next, _ := s.Update(msg, wizardEnv())
	return next.(*WizardScreen)
}

// pressSpecial drives one non-printable key (enter, tab, space) through
// the wizard's Update.
func pressSpecial(t *testing.T, s *WizardScreen, code rune) *WizardScreen {
	t.Helper()
	next, _ := s.Update(tea.KeyPressMsg{Code: code}, wizardEnv())
	return next.(*WizardScreen)
}

// pressEsc drives the Back binding through the wizard's own HandleBack,
// mirroring how the shell routes it.
func pressEsc(t *testing.T, s *WizardScreen) (*WizardScreen, bool) {
	t.Helper()
	next, _, handled := s.HandleBack()
	return next.(*WizardScreen), handled
}

// loadWizard drives a freshly constructed wizard through Init so its
// catalog-derived state (version rows, packages, addons, dll cursor) is
// populated, exactly as the real Bubble Tea loop would before the user can
// press anything. preselected indexes entry.PlayableExes(), matching how a
// caller resolves a target folder before ever opening the form — the form
// itself has no exe-picking step of its own.
func loadWizard(t *testing.T, entry GameEntry, preselected int, deps Deps) *WizardScreen {
	t.Helper()
	s := NewWizardScreen(entry, entry.PlayableExes()[preselected], deps)
	cmd := s.Init()
	if cmd == nil {
		t.Fatal("Init() returned a nil command; nothing would ever load")
	}
	msg := cmd()
	next, _ := s.Update(msg, wizardEnv())
	return next.(*WizardScreen)
}

// advance walks the wizard forward to step by pressing enter, failing
// rather than looping forever if the step is skipped for this build.
func advance(t *testing.T, s *WizardScreen, step wizardStep) *WizardScreen {
	t.Helper()
	if s.editing {
		return openSection(t, s, step)
	}
	for i := 0; i <= len(wizardSteps); i++ {
		if s.step == step {
			return s
		}
		s = pressSpecial(t, s, tea.KeyEnter)
	}
	t.Fatalf("step %v was never reached (stuck on %v)", step, s.step)
	return s
}

// openSection opens one section from the edit-mode summary, which is how
// every step is reached when an install already exists.
func openSection(t *testing.T, s *WizardScreen, step wizardStep) *WizardScreen {
	t.Helper()
	if s.step != stepHub {
		s, _ = pressEsc(t, s)
	}
	for i, row := range s.hubSections() {
		if row != step {
			continue
		}
		s.hubCursor.setCursor(i)
		return pressSpecial(t, s, tea.KeyEnter)
	}
	t.Fatalf("section %v is not offered by this summary (%v)", step, s.hubSections())
	return s
}

func TestWizardLoadsDataAndPreselectsRequired(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())

	if s.loading {
		t.Fatal("loading should be false once wizardDataLoadedMsg has been handled")
	}
	if !s.packages.isSelected(0) {
		t.Error("the Required package (standard-effects) should start selected")
	}
}

// The user's configured default packages (config.yaml defaults.packages,
// aliases and all) must be preselected alongside whatever is Required.
func TestWizardPreselectsConfiguredDefaults(t *testing.T) {
	deps := fakeDeps()
	deps.Defaults.Packages = []string{"sweetfx"} // an alias, not the raw id
	s := loadWizard(t, sampleGameEntry(), 0, deps)

	if !s.packages.selected["sweetfx-by-ceejay-dk"] {
		t.Error("a configured default package (given as an alias) should be preselected")
	}
}

// The first step shows the two ReShade builds as side-by-side panes over
// the same version list — the same normal/addon split the resources
// browser shows — with ←/→ moving between them and the version cursor
// shared, so switching build keeps the version.
func TestWizardReShadeStepShowsBothBuildsAsPanes(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	body := s.View(wizardEnv())

	for _, want := range []string{"ReShade (normal)", "ReShade (addon)"} {
		if !strings.Contains(body, want) {
			t.Errorf("the ReShade step should show both builds, missing %q:\n%s", want, body)
		}
	}
	// Both panes list the same versions, so each version appears twice.
	if got := strings.Count(body, "6.8.0"); got < 2 {
		t.Errorf("6.8.0 appears %d time(s); both panes should list it:\n%s", got, body)
	}

	if s.flavor != install.FlavorAddon {
		t.Fatalf("flavor = %q, want addon (the configured default)", s.flavor)
	}
	v, _ := s.selectedVersion()
	s = pressSpecial(t, s, tea.KeyLeft)
	if s.flavor != install.FlavorNormal {
		t.Errorf("flavor = %q after ←, want normal", s.flavor)
	}
	if got, _ := s.selectedVersion(); got.Version != v.Version {
		t.Errorf("version = %q after switching build, want it kept at %q", got.Version, v.Version)
	}
	s = pressSpecial(t, s, tea.KeyRight)
	if s.flavor != install.FlavorAddon {
		t.Errorf("flavor = %q after →, want addon", s.flavor)
	}
}

// Re-running the wizard on an already-installed folder must edit what is
// there — build, version, DLL and packages preselected from the recorded
// install — rather than silently resetting to the configured defaults.
func TestWizardEditingExistingInstallPreselectsWhatIsThere(t *testing.T) {
	entry := sampleGameEntry()
	entry.Exes[0].Installed = &state.Install{
		Exe:      "Game/emberhollow.exe",
		ReShade:  state.ReShadeInfo{Version: "6.7.3", Flavor: "normal", DLL: "dxgi.dll"},
		Packages: []string{"sweetfx-by-ceejay-dk"},
	}

	deps := fakeDeps()
	deps.Defaults.Packages = nil // would preselect nothing if defaults were used instead
	s := loadWizard(t, entry, 0, deps)

	if s.flavor != install.FlavorNormal {
		t.Errorf("flavor = %q, want normal (from the existing install, not the addon default)", s.flavor)
	}
	if v, _ := s.selectedVersion(); v.Version != "6.7.3" {
		t.Errorf("preselected version = %q, want 6.7.3 (the recorded one, not latest)", v.Version)
	}
	if !s.packages.selected["sweetfx-by-ceejay-dk"] {
		t.Error("the existing install's package should be preselected")
	}
	if got := s.Title(); !strings.Contains(got, "edit install") {
		t.Errorf("Title() = %q, want it to say it is editing an existing install", got)
	}
}

// An adopted install's version is recorded as "unknown (adopted)", which
// never matches a catalog entry — the step must fall back to latest rather
// than leaving the cursor on nothing.
func TestWizardEditingAdoptedInstallFallsBackToLatestVersion(t *testing.T) {
	entry := sampleGameEntry()
	entry.Exes[0].Installed = &state.Install{
		ReShade: state.ReShadeInfo{Version: install.AdoptedVersion, Flavor: "addon", DLL: "dxgi.dll"},
	}
	s := loadWizard(t, entry, 0, fakeDeps())

	if v, ok := s.selectedVersion(); !ok || !v.Latest {
		t.Errorf("preselected version = %+v, want the latest one", v)
	}
}

// The full forward path — ReShade, API, Shaders, Add-ons, Review — ending
// with a Request that reflects every selection made along the way.
func TestWizardFullForwardFlowBuildsRequest(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())

	if s.step != stepReShade {
		t.Fatalf("step = %v, want stepReShade (no Exe step)", s.step)
	}
	s = pressSpecial(t, s, tea.KeyEnter) // accept the preselected (latest) version
	if s.step != stepAPI {
		t.Fatalf("step = %v, want stepAPI", s.step)
	}
	if got := s.selectedDLL(); got != "dxgi.dll" {
		t.Fatalf("recommended DLL = %q, want dxgi.dll for a D3D12 exe", got)
	}

	s = pressSpecial(t, s, tea.KeyEnter)
	if s.step != stepShaders {
		t.Fatalf("step = %v, want stepShaders", s.step)
	}
	s = pressSpecial(t, s, tea.KeyDown) // move to SweetFX
	s = pressSpecial(t, s, ' ')         // select it alongside the required Standard effects
	s = pressSpecial(t, s, tea.KeyEnter)

	if s.step != stepAddons {
		t.Fatalf("step = %v, want stepAddons (the addon build is the default)", s.step)
	}
	s = pressSpecial(t, s, ' ') // the cursor starts on the installable add-on
	s = pressSpecial(t, s, tea.KeyEnter)
	if s.step != stepReview {
		t.Fatalf("step = %v, want stepReview", s.step)
	}

	req, ok := s.buildRequest()
	if !ok {
		t.Fatal("buildRequest() ok = false at the review step")
	}
	if req.Version != "6.8.0" {
		t.Errorf("Version = %q, want the latest (6.8.0)", req.Version)
	}
	if req.Flavor != install.FlavorAddon {
		t.Errorf("Flavor = %q, want addon (the default)", req.Flavor)
	}
	if req.DLLName != "dxgi.dll" {
		t.Errorf("DLLName = %q, want dxgi.dll", req.DLLName)
	}
	if req.Exe.Path != "Game/emberhollow.exe" {
		t.Errorf("Exe.Path = %q, want the first executable", req.Exe.Path)
	}
	wantPackages := map[string]bool{"standard-effects": true, "sweetfx-by-ceejay-dk": true}
	if len(req.Packages) != 2 {
		t.Fatalf("Packages = %v, want 2 entries", req.Packages)
	}
	for _, p := range req.Packages {
		if !wantPackages[p] {
			t.Errorf("unexpected package %q in Request", p)
		}
	}
	if len(req.Addons) != 1 || req.Addons[0] != "swap-chain-override-by-crosire" {
		t.Errorf("Addons = %v, want just the installable one", req.Addons)
	}
	if req.TargetOS != artifacts.CurrentTargetOS() {
		t.Errorf("TargetOS = %q, want the current platform", req.TargetOS)
	}
}

// Paging is only bearable if a later step still shows what the earlier
// ones decided — that running summary is the whole reason the wizard can
// be a sequence of steps at all.
func TestWizardShowsWhatEarlierStepsDecided(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())

	// Nothing is decided yet on the first step, so there is nothing to
	// summarize.
	if got := s.decided(wizardEnv()); got != "" {
		t.Errorf("the first step should have no summary, got %q", got)
	}

	s = advance(t, s, stepAPI)
	if got := s.decided(wizardEnv()); !strings.Contains(got, "ReShade 6.8.0 (addon)") {
		t.Errorf("the API step should show the chosen version and build, got %q", got)
	}

	s = advance(t, s, stepShaders)
	summary := s.decided(wizardEnv())
	for _, want := range []string{"ReShade 6.8.0 (addon)", "dxgi.dll"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the shaders step's summary should carry %q, got %q", want, summary)
		}
	}

	s = advance(t, s, stepReview)
	summary = s.decided(wizardEnv())
	for _, want := range []string{"ReShade 6.8.0 (addon)", "dxgi.dll", "Standard effects"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the review step's summary should carry %q, got %q", want, summary)
		}
	}
	// And it is actually rendered, not merely computed.
	if !strings.Contains(s.View(wizardEnv()), "Standard effects") {
		t.Error("the summary should appear in the rendered step")
	}
}

// A manual-only add-on must never end up in the built Request, however
// forcefully the user tries to select it.
func TestWizardCannotSelectManualOnlyAddon(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepAddons)

	// Move to and try to toggle the manual-only add-on (index 1).
	s = pressSpecial(t, s, tea.KeyDown)
	s = pressSpecial(t, s, ' ')

	if s.addons.isSelected(1) {
		t.Fatal("a manual-only add-on must not become selectable")
	}
}

// Choosing the normal build must skip the add-ons step entirely, both
// going forward and coming back — it cannot load them at all.
func TestWizardNormalBuildSkipsAddonsStep(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = pressSpecial(t, s, tea.KeyLeft) // addon -> normal
	if s.flavor != install.FlavorNormal {
		t.Fatalf("flavor = %q after ←, want normal", s.flavor)
	}

	s = pressSpecial(t, s, tea.KeyEnter) // -> API
	s = pressSpecial(t, s, tea.KeyEnter) // -> Shaders
	s = pressSpecial(t, s, tea.KeyEnter) // should skip Add-ons -> Review
	if s.step != stepReview {
		t.Fatalf("step = %v, want stepReview (Add-ons skipped)", s.step)
	}
	if !strings.Contains(s.View(wizardEnv()), "Add-ons (skipped)") {
		t.Error("the breadcrumb should mark Add-ons as skipped for the normal build")
	}

	back, handled := pressEsc(t, s)
	if !handled {
		t.Fatal("HandleBack() should handle esc mid-wizard")
	}
	if back.step != stepShaders {
		t.Fatalf("stepping back from Review with the normal build landed on %v, want stepShaders", back.step)
	}
}

// Add-ons selected while on the addon build must not survive a request
// built after switching to normal (install.Request.Validate would
// otherwise reject it).
func TestWizardSwitchingToNormalDropsAddonsFromRequest(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepAddons)
	s = pressSpecial(t, s, ' ') // select the installable add-on
	s = pressSpecial(t, s, tea.KeyEnter)

	// Step back to ReShade and switch to the normal build. The build is
	// still addon while stepping back, so Review's previous step is Add-ons.
	s, _ = pressEsc(t, s) // Review   -> Addons
	s, _ = pressEsc(t, s) // Addons   -> Shaders
	s, _ = pressEsc(t, s) // Shaders  -> API
	s, _ = pressEsc(t, s) // API      -> ReShade
	if s.step != stepReShade {
		t.Fatalf("step = %v, want stepReShade", s.step)
	}
	s = pressSpecial(t, s, tea.KeyLeft)

	req, ok := s.buildRequest()
	if !ok {
		t.Fatal("buildRequest() ok = false")
	}
	if err := req.Validate(); err != nil {
		t.Errorf("Request.Validate() = %v, want nil", err)
	}
	if len(req.Addons) != 0 {
		t.Errorf("Addons = %v, want none: the normal build cannot carry them", req.Addons)
	}
}

// The add-on build can trip anti-cheat detection — a real account-ban risk
// in an online game — so every step must warn while it is the current
// build, and none of them when it is not.
func TestWizardWarnsAboutAddonAntiCheatRisk(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps()) // fakeDeps defaults to addon
	const marker = "anti-cheat detection"
	if body := s.View(wizardEnv()); !strings.Contains(body, marker) {
		t.Errorf("the addon build should show the anti-cheat warning:\n%s", body)
	}

	s = pressSpecial(t, s, tea.KeyLeft) // -> normal
	if body := s.View(wizardEnv()); strings.Contains(body, marker) {
		t.Errorf("the normal build should not show the anti-cheat warning:\n%s", body)
	}
}

// HandleBack at the very first step must defer to the shell (pop back to
// the game detail screen), not try to step further back.
func TestWizardBackAtFirstStepDefers(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	if _, _, handled := s.HandleBack(); handled {
		t.Error("HandleBack() at stepReShade should return handled=false")
	}
}

// The API step always runs — it is one page, and skipping it also skipped
// telling the user which DLL was chosen. It marks the option matching the
// detected API, and says plainly when there is nothing to detect.
func TestWizardAPIStepExplainsItsRecommendation(t *testing.T) {
	confident := loadWizard(t, sampleGameEntry(), 0, fakeDeps()) // D3D12
	if confident.exe.API.RecommendedDLL() == "" {
		t.Fatal("a D3D12 exe should map to a recommended DLL")
	}
	confident = advance(t, confident, stepAPI)
	body := confident.View(wizardEnv())
	if !strings.Contains(body, "dxgi.dll") || !strings.Contains(body, "← detected DirectX 12") {
		t.Errorf("the API step should mark the option matching the detected API:\n%s", body)
	}

	unsupported := loadWizard(t, sampleGameEntry(), 1, fakeDeps()) // Vulkan
	if unsupported.exe.API.RecommendedDLL() != "" {
		t.Fatal("an unsupported API should not map to a recommended DLL")
	}
	unsupported = advance(t, unsupported, stepAPI)
	body = unsupported.View(wizardEnv())
	if !strings.Contains(strings.ToLower(body), "not supported yet") {
		t.Errorf("the API step should say vulkan is unsupported:\n%s", body)
	}
	if got := unsupported.selectedDLL(); got != "dxgi.dll" {
		t.Errorf("fallback DLL = %q, want dxgi.dll", got)
	}
}

// A manual DLL pick must survive a trip back to the first step and
// forward again, rather than being silently recomputed.
func TestWizardManualDLLChoiceSurvivesRevisit(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 1, fakeDeps()) // Vulkan: nothing detected
	s = advance(t, s, stepAPI)

	s = pressSpecial(t, s, tea.KeyDown) // move off the fallback
	picked := s.selectedDLL()
	if picked == "dxgi.dll" {
		t.Fatal("test setup: cursor did not move")
	}

	s, _ = pressEsc(t, s)                // -> ReShade
	s = pressSpecial(t, s, tea.KeyEnter) // -> API
	if got := s.selectedDLL(); got != picked {
		t.Errorf("DLL choice = %q after revisiting, want the earlier manual pick %q", got, picked)
	}
}

// Overwrite is a checkbox on the review step, toggled the same way
// shaders and add-ons are.
func TestWizardReviewOverwriteToggle(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepReview)

	if s.overwrite() {
		t.Fatal("overwrite should start off")
	}
	s = pressSpecial(t, s, ' ')
	if !s.overwrite() {
		t.Error("space should toggle overwrite on")
	}
	s = pressSpecial(t, s, ' ')
	if s.overwrite() {
		t.Error("space should toggle overwrite back off")
	}
}

// Pressing enter at Review must push a ProgressScreen carrying the built
// Request.
func TestWizardReviewEnterPushesProgress(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepReview)

	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, wizardEnv())
	if cmd == nil {
		t.Fatal("enter at Review should return a command")
	}
	push, ok := cmd().(pushScreenMsg)
	if !ok {
		t.Fatalf("message = %T, want pushScreenMsg", cmd())
	}
	if _, ok := push.screen.(*ProgressScreen); !ok {
		t.Fatalf("pushed screen = %T, want *ProgressScreen", push.screen)
	}
}

// Review says what the install still has to fetch — the one thing the
// earlier steps could not already show.
func TestWizardReviewListsWhatItWillDownload(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = advance(t, s, stepReview)
	body := s.View(wizardEnv())

	for _, want := range []string{"To download", "ReShade 6.8.0 (addon)", "Options"} {
		if !strings.Contains(body, want) {
			t.Errorf("review should show %q:\n%s", want, body)
		}
	}
	const disclaimer = "Files not created by yarm are left in place unless overwrite is on."
	if !strings.Contains(body, wizardEnv().Styles.Warn.Render(disclaimer)) {
		t.Errorf("the disclaimer should be styled as a warning (yellow):\n%s", body)
	}
}

// The wizard names the folder ReShade will attach to, not one executable
// inside it — the folder is what the user chose, and what ReShade
// intercepts by.
func TestWizardShowsTargetFolderNotExecutable(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	body := s.View(wizardEnv())

	if !strings.Contains(body, "/games/EH/Game") {
		t.Errorf("the wizard should name the target folder:\n%s", body)
	}
	if strings.Contains(body, "emberhollow.exe") {
		t.Errorf("the wizard should not present an executable as the target:\n%s", body)
	}
}

// The shaders step is labeled "Shaders", matching how the ReShade
// community refers to them — not "Packages".
func TestWizardBreadcrumbLabelsShadersNotPackages(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	body := s.View(wizardEnv())
	if !strings.Contains(body, "Shaders") {
		t.Errorf("breadcrumb should say \"Shaders\":\n%s", body)
	}
	if strings.Contains(body, "Packages") {
		t.Errorf("breadcrumb should not say \"Packages\":\n%s", body)
	}
}

// A shader row shows its name and description but no "cached" badge — the
// user found it redundant with the review step's download list.
func TestWizardShaderRowsOmitCachedBadge(t *testing.T) {
	deps := fakeDeps()
	deps.CacheStatus = fakeCacheStatus{packages: map[string]bool{"standard-effects": true}}
	s := loadWizard(t, sampleGameEntry(), 0, deps)
	s = advance(t, s, stepShaders)

	for _, line := range strings.Split(s.View(wizardEnv()), "\n") {
		if strings.Contains(line, "Standard effects") && strings.Contains(line, "cached") {
			t.Errorf("a shader row should not carry a \"cached\" badge: %q", line)
		}
	}
}

// A wizard with no catalog data at all (offline, nothing cached) must say
// so rather than silently offering an empty install.
func TestWizardReportsEmptyCatalog(t *testing.T) {
	deps := fakeDeps()
	deps.WizardData = fakeWizardData{data: WizardData{}}
	s := loadWizard(t, sampleGameEntry(), 0, deps)

	if s.loadErr == "" {
		t.Error("an empty catalog should set loadErr")
	}
	if _, ok := s.buildRequest(); ok {
		t.Error("buildRequest() should refuse when there is no version to install")
	}
}

// A catalog load failure should not crash the wizard; it reaches the
// shell's error overlay while the wizard's own state stays sane.
func TestWizardCatalogLoadErrorDoesNotBreakWizard(t *testing.T) {
	deps := fakeDeps()
	deps.WizardData = fakeWizardData{err: errors.New("network unreachable")}
	entry := sampleGameEntry()
	s := NewWizardScreen(entry, entry.PlayableExes()[0], deps)

	cmd := s.Init()
	msg := cmd()
	wd, ok := msg.(wizardDataLoadedMsg)
	if !ok || wd.err == nil {
		t.Fatalf("message = %#v, want a wizardDataLoadedMsg carrying an error", msg)
	}

	next, followUp := s.Update(msg, wizardEnv())
	s = next.(*WizardScreen)

	// The error must still reach the shell's overlay...
	if followUp == nil {
		t.Fatal("a load error should still produce a command reporting it")
	}
	if _, ok := followUp().(errorMsg); !ok {
		t.Error("the follow-up command should report the error to the shell")
	}
	// ...but the wizard itself must not get stuck loading forever, and must
	// remain usable (still on the first step, not stranded).
	if s.loading {
		t.Error("loading should be false once the (failed) load has been handled")
	}
	if s.step != stepReShade {
		t.Errorf("step = %v, want stepReShade (still usable) after a load error", s.step)
	}
}

// describeMissing is what tells the user what an install is actually about
// to download — getting it wrong would either alarm them over something
// already cached or hide a real ~40 MB d3dcompiler fetch.
func TestDescribeMissing(t *testing.T) {
	cache := fakeCacheStatus{
		reshade:  map[string]bool{"6.8.0:addon": true},
		packages: map[string]bool{"standard-effects": true},
		addons:   map[string]bool{},
	}

	got := describeMissing(cache, "6.8.0", true,
		[]string{"standard-effects", "sweetfx-by-ceejay-dk"},
		[]string{"swap-chain-override-by-crosire"},
		game.ArchX64, true)

	want := []string{
		"package sweetfx-by-ceejay-dk",
		"add-on swap-chain-override-by-crosire",
		"d3dcompiler_47.dll (~40 MB, once)",
	}
	if len(got) != len(want) {
		t.Fatalf("describeMissing() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDescribeMissingNothingMissing(t *testing.T) {
	cache := fakeCacheStatus{
		reshade:     map[string]bool{"6.8.0:normal": true},
		d3dcompiler: true,
	}
	got := describeMissing(cache, "6.8.0", false, nil, nil, game.ArchX64, true)
	if len(got) != 0 {
		t.Errorf("describeMissing() = %v, want none (everything already cached)", got)
	}
}

// A nil CacheStatus (no cache wired up at all) must report everything as
// missing rather than panicking or, worse, claiming nothing is needed.
func TestDescribeMissingNilCache(t *testing.T) {
	got := describeMissing(nil, "6.8.0", true, []string{"standard-effects"}, nil, game.ArchX64, true)
	want := []string{"ReShade 6.8.0 (addon)", "package standard-effects", "d3dcompiler_47.dll (~40 MB, once)"}
	if len(got) != len(want) {
		t.Fatalf("describeMissing(nil cache) = %v, want %v", got, want)
	}
}

// Version list is capped to the most recent 10; editing an install whose
// recorded version has since aged out of that window must still be able to
// find it, rather than silently defaulting to latest.
func TestCapVersionsLimitsToTenAndKeepsEditedVersionVisible(t *testing.T) {
	versions := make([]catalog.Version, 0, 15)
	for i := 0; i < 15; i++ {
		versions = append(versions, catalog.Version{Version: fmt.Sprintf("6.%d.0", 15-i)})
	}

	capped := capVersions(versions, "")
	if len(capped) != 10 {
		t.Fatalf("len(capped) = %d, want 10", len(capped))
	}

	// "6.1.0" (index 14) is well outside the top 10 by recency.
	capped = capVersions(versions, "6.1.0")
	found := false
	for _, v := range capped {
		if v.Version == "6.1.0" {
			found = true
		}
	}
	if !found {
		t.Error("an edited install's version outside the top 10 should still be listed")
	}
	if len(capped) != 11 {
		t.Errorf("len(capped) = %d, want 11 (10 + the appended existing version)", len(capped))
	}
}

// A long catalog (Shaders can run ~40 real packages) must not print past
// the height it is given — it should window around the cursor and say how
// many rows are hidden, rather than overrunning the shell's footer with no
// indication anything was cut off.
func TestWriteSelectListWindowsLongListsToHeight(t *testing.T) {
	items := make([]selectItem, 30)
	for i := range items {
		items[i] = selectItem{ID: fmt.Sprintf("pkg-%d", i), Name: fmt.Sprintf("Package %d", i)}
	}
	m := newMultiSelect(items)
	m.cursor = 20 // scrolled well past the first page

	const height = 12
	var b strings.Builder
	writeSelectList(&b, Env{Styles: NewStyles(true), Width: 100, Height: 30}, m, height)
	body := b.String()

	if strings.Contains(body, "Package 0\n") {
		t.Errorf("a row far above the cursor should have scrolled out of view:\n%s", body)
	}
	if !strings.Contains(body, "Package 20") {
		t.Errorf("the row under the cursor must stay visible:\n%s", body)
	}
	if !strings.Contains(body, "more above") {
		t.Errorf("scrolling past the top should say how many rows are hidden above:\n%s", body)
	}
	if got := countLines(body); got > height {
		t.Errorf("rendered %d lines for a height of %d:\n%s", got, height, body)
	}
}

// Every step must fit the sizes yarm claims to support — a plain 80x24
// terminal, and the 60 columns a `tmux split-window -h` leaves — without
// any line running past the width or the page past the height. 60 columns
// is also where the ReShade step's two panes stop fitting side by side.
func TestWizardFitsNarrowTerminals(t *testing.T) {
	// Heights are the terminal's, less the shell's own three rows of chrome.
	sizes := []struct{ width, height int }{{80, 21}, {60, 21}, {44, 21}}

	for _, size := range sizes {
		for _, step := range wizardSteps {
			for _, normal := range []bool{false, true} {
				if normal && step == stepAddons {
					continue // skipped for the normal build
				}
				s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
				if normal {
					s = pressSpecial(t, s, tea.KeyLeft)
				}
				s = advance(t, s, step)

				env := Env{Styles: NewStyles(true), Width: size.width, Height: size.height}
				body := s.View(env)
				if got := countLines(body); got > env.Height {
					t.Errorf("%dx%d, step %v (normal=%v): rendered %d lines into a height of %d:\n%s",
						size.width, size.height, step, normal, got, env.Height, body)
				}
				for _, line := range strings.Split(body, "\n") {
					if lipgloss.Width(line) > size.width {
						t.Errorf("%dx%d, step %v (normal=%v): line is %d columns wide:\n%q",
							size.width, size.height, step, normal, lipgloss.Width(line), line)
					}
				}
			}
		}
	}
}

// Below the width where both version panes fit, the ReShade step folds to
// the focused build full-width, with a strip naming the other so it does
// not simply vanish.
func TestWizardReShadeStepFoldsToOnePaneWhenNarrow(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())

	env := Env{Styles: NewStyles(true), Width: 40, Height: 21}
	body := s.View(env)
	for _, want := range []string{"ReShade (normal)", "ReShade (addon)"} {
		if !strings.Contains(body, want) {
			t.Errorf("the narrow layout should still name %q:\n%s", want, body)
		}
	}
	// Only the focused build's versions are listed now, so each appears once.
	if got := strings.Count(body, "6.7.3"); got != 1 {
		t.Errorf("6.7.3 appears %d time(s) in the folded layout, want 1:\n%s", got, body)
	}
}

// withCatalogNoise returns the sample data plus a package and an add-on
// that are in the catalog but not on either shortlist — the 30-odd niche
// entries the shortlist exists to demote.
func withCatalogNoise(d WizardData) WizardData {
	d.Packages = append(d.Packages, catalog.Package{
		ID: "crt-royale-reshade-by-akgunter", Name: "CRT-Royale-ReShade by akgunter",
	})
	d.Addons = append(d.Addons, catalog.Addon{
		ID: "livesplit-overlay-by-mleise", Name: "LiveSplit Overlay by mleise",
		URL64: "https://example.invalid/livesplit.zip",
	})
	return d
}

// The shaders and add-ons steps open on a curated shortlist rather than
// the whole catalog, and "a" widens them to everything.
func TestWizardShowsCuratedShortlistUntilShowAllIsPressed(t *testing.T) {
	deps := fakeDeps()
	deps.WizardData = fakeWizardData{data: withCatalogNoise(sampleWizardData())}

	for _, tc := range []struct {
		step  wizardStep
		short string
		noise string
	}{
		{stepShaders, "SweetFX by CeeJay.dk", "CRT-Royale-ReShade by akgunter"},
		{stepAddons, "Swap chain override by crosire", "LiveSplit Overlay by mleise"},
	} {
		s := advance(t, loadWizard(t, sampleGameEntry(), 0, deps), tc.step)

		body := s.View(wizardEnv())
		if !strings.Contains(body, tc.short) {
			t.Errorf("%v: shortlisted %q should be shown:\n%s", tc.step, tc.short, body)
		}
		if strings.Contains(body, tc.noise) {
			t.Errorf("%v: %q is not shortlisted and should be hidden until 'a':\n%s", tc.step, tc.noise, body)
		}
		if !strings.Contains(body, "a all") {
			t.Errorf("%v: a hidden remainder must say so in the footer:\n%s", tc.step, body)
		}

		body = press(t, s, 'a').View(wizardEnv())
		if !strings.Contains(body, tc.noise) {
			t.Errorf("%v: 'a' should reveal %q:\n%s", tc.step, tc.noise, body)
		}
	}
}

// Widening the list, checking something obscure, then narrowing it again
// must not quietly drop the choice: the row stays visible because it is
// selected, and it reaches the Request.
func TestWizardShowAllSelectionSurvivesNarrowingBack(t *testing.T) {
	deps := fakeDeps()
	deps.WizardData = fakeWizardData{data: withCatalogNoise(sampleWizardData())}
	s := advance(t, loadWizard(t, sampleGameEntry(), 0, deps), stepShaders)

	s = press(t, s, 'a')
	for s.packages.items[s.packages.cursor].ID != "crt-royale-reshade-by-akgunter" {
		s = pressSpecial(t, s, tea.KeyDown)
	}
	s = pressSpecial(t, s, tea.KeySpace)
	s = press(t, s, 'a') // back to the shortlist

	if !strings.Contains(s.View(wizardEnv()), "CRT-Royale-ReShade by akgunter") {
		t.Error("a selected package must stay visible after narrowing back to the shortlist")
	}
	req, ok := advance(t, s, stepReview).buildRequest()
	if !ok {
		t.Fatal("buildRequest() failed")
	}
	if !slices.Contains(req.Packages, "crt-royale-reshade-by-akgunter") {
		t.Errorf("Packages = %v, want the off-shortlist package that was checked", req.Packages)
	}
}

// Editing an install that uses an off-shortlist package must show it,
// however obscure — the shortlist is about first-run noise, not about
// hiding what a folder already has.
func TestWizardCuratedListStillShowsWhatIsAlreadyInstalled(t *testing.T) {
	deps := fakeDeps()
	deps.WizardData = fakeWizardData{data: withCatalogNoise(sampleWizardData())}

	entry := sampleGameEntry()
	entry.Exes[0].Installed = &state.Install{
		ReShade:  state.ReShadeInfo{Version: "6.8.0", Flavor: "addon", DLL: "dxgi.dll"},
		Packages: []string{"crt-royale-reshade-by-akgunter"},
	}
	s := advance(t, loadWizard(t, entry, 0, deps), stepShaders)

	if !strings.Contains(s.View(wizardEnv()), "CRT-Royale-ReShade by akgunter") {
		t.Error("an installed package outside the shortlist should still be listed")
	}
}

// Custom content is the user's own and is never filtered, header and all.
func TestCurateKeepsCustomContentAndHeaders(t *testing.T) {
	items := []selectItem{
		{ID: "standard-effects", Name: "Standard effects", Required: true},
		{ID: "crt-royale-reshade-by-akgunter", Name: "CRT-Royale"},
		{Header: true, Name: "── Custom ──"},
		{ID: "custom:shaders:mine", Name: "mine"},
	}
	got := curate(items, curatedPackages, false, map[string]bool{})

	var names []string
	for _, it := range got {
		names = append(names, it.Name)
	}
	want := []string{"Standard effects", "── Custom ──", "mine"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Errorf("curate() kept %v, want %v", names, want)
	}
}

// A typo'd shortlist id fails silently — the pack just never appears — so
// pin the one property that catches it: every id must already be the slug
// form the catalog generates.
func TestCuratedIDsAreCatalogSlugs(t *testing.T) {
	for _, list := range []map[string]bool{curatedPackages, curatedAddons} {
		for id := range list {
			if got := catalog.Slugify(id); got != id {
				t.Errorf("curated id %q is not a catalog slug (Slugify gives %q)", id, got)
			}
		}
	}
}

// withDependencies returns sample data holding a real dependency pair from
// the catalog: the AutoHDR add-on, which does nothing useful without a
// tone-mapping shader, plus both shaders that can satisfy it.
func withDependencies(d WizardData) WizardData {
	d.Packages = append(d.Packages,
		catalog.Package{ID: "reshade-hdr-shaders-by-lilium", Name: "ReShade_HDR_shaders by Lilium"},
		catalog.Package{ID: "advancedautohdr-by-pumbo", Name: "AdvancedAutoHDR by Pumbo"},
	)
	d.Addons = append(d.Addons, catalog.Addon{
		ID:    "autohdr-by-endlesslyflowering-original-by-majorpainthecactus",
		Name:  "AutoHDR by EndlesslyFlowering",
		URL64: "https://example.invalid/autohdr.addon64",
	})
	return d
}

// depsWizard loads a wizard over withDependencies data, parked on step.
func depsWizard(t *testing.T, step wizardStep) *WizardScreen {
	t.Helper()
	deps := fakeDeps()
	deps.WizardData = fakeWizardData{data: withDependencies(sampleWizardData())}
	return advance(t, loadWizard(t, sampleGameEntry(), 0, deps), step)
}

// toggleRow moves the cursor onto id and presses space.
func toggleRow(t *testing.T, s *WizardScreen, m *multiSelect, id string) *WizardScreen {
	t.Helper()
	for i := 0; i < len(m.items); i++ {
		if m.items[m.cursor].ID == id {
			return pressSpecial(t, s, tea.KeySpace)
		}
		s = pressSpecial(t, s, tea.KeyDown)
	}
	t.Fatalf("row %q is not in the list", id)
	return s
}

const autoHDR = "autohdr-by-endlesslyflowering-original-by-majorpainthecactus"

// Selecting the AutoHDR add-on must bring its tone-mapping shader with it:
// upstream records the requirement only as English inside the add-on's
// description, and an AutoHDR install without it silently does nothing.
func TestWizardSelectingAnAddonSelectsWhatItRequires(t *testing.T) {
	s := depsWizard(t, stepAddons)
	s = toggleRow(t, s, &s.addons, autoHDR)

	if !s.packages.selected["reshade-hdr-shaders-by-lilium"] {
		t.Fatal("selecting AutoHDR should also select the tone-mapping shader it requires")
	}

	req, ok := advance(t, s, stepReview).buildRequest()
	if !ok {
		t.Fatal("buildRequest() failed")
	}
	if !slices.Contains(req.Packages, "reshade-hdr-shaders-by-lilium") {
		t.Errorf("Packages = %v, want the required shader included", req.Packages)
	}
}

// A dependency that was pulled in says who pulled it in, so a package the
// user never checked does not just appear checked.
func TestWizardDependencySaysWhatRequiredIt(t *testing.T) {
	s := depsWizard(t, stepAddons)
	s = toggleRow(t, s, &s.addons, autoHDR)

	body := pressEscTo(t, s, stepShaders).View(wizardEnv())
	if !strings.Contains(body, "required by AutoHDR by EndlesslyFlowering") {
		t.Errorf("the shaders step should say what pulled the dependency in:\n%s", body)
	}
}

// A requirement the user already met another way is left alone: yarm must
// not add Lilium's shader on top of the alternative that is already ticked.
func TestWizardRequirementAlreadyMetIsNotAddedTwice(t *testing.T) {
	s := depsWizard(t, stepShaders)
	s = press(t, s, 'a') // both alternatives are off the shortlist
	s = toggleRow(t, s, &s.packages, "advancedautohdr-by-pumbo")
	s = toggleRow(t, advance(t, s, stepAddons), &s.addons, autoHDR)

	if s.packages.selected["reshade-hdr-shaders-by-lilium"] {
		t.Error("the requirement was already met by AdvancedAutoHDR; nothing more should have been added")
	}
}

// Unchecking a dependency by hand is allowed — it may be installed
// already — but the review page has to say what is now missing rather than
// running an install that quietly does nothing.
func TestWizardReviewWarnsAboutUnmetRequirements(t *testing.T) {
	s := depsWizard(t, stepAddons)
	s = toggleRow(t, s, &s.addons, autoHDR)

	// Drop the dependency again from the shaders step.
	s = pressEscTo(t, s, stepShaders)
	s = toggleRow(t, s, &s.packages, "reshade-hdr-shaders-by-lilium")
	if s.packages.selected["reshade-hdr-shaders-by-lilium"] {
		t.Fatal("the dependency should have been unchecked")
	}

	body := advance(t, s, stepReview).View(wizardEnv())
	if !strings.Contains(body, "Check") || !strings.Contains(body, "needs a tone-mapping shader") {
		t.Errorf("review should warn that AutoHDR's requirement is unmet:\n%s", body)
	}
}

// A requirement nothing in the catalog can satisfy is reported, never
// silently "satisfied" by selecting something arbitrary.
func TestRequirementWithNothingToSelectIsOnlyReported(t *testing.T) {
	selected := map[string]bool{"frame-capture-by-murchalloo": true}
	if added := satisfy("frame-capture-by-murchalloo", selected, map[string]bool{}); len(added) != 0 {
		t.Errorf("satisfy() selected %v for a requirement with no catalog entry", added)
	}
	got := unmetRequirements([]string{"frame-capture-by-murchalloo"},
		map[string]string{"frame-capture-by-murchalloo": "Frame Capture"}, selected)
	if len(got) != 1 || !strings.Contains(got[0], "DepthToAddon.fx") {
		t.Errorf("unmetRequirements() = %v, want the manual requirement named", got)
	}
}

// Every id named by the requirements table must be one the catalog can
// actually produce, or the dependency silently never resolves.
func TestRequirementIDsAreCatalogSlugs(t *testing.T) {
	for id, reqs := range requires {
		if got := catalog.Slugify(id); got != id {
			t.Errorf("requires key %q is not a catalog slug (Slugify gives %q)", id, got)
		}
		for _, r := range reqs {
			for _, dep := range r.AnyOf {
				if got := catalog.Slugify(dep); got != dep {
					t.Errorf("%q requires %q, which is not a catalog slug (Slugify gives %q)", id, dep, got)
				}
			}
		}
	}
}

// pressEscTo walks the wizard backward to step.
func pressEscTo(t *testing.T, s *WizardScreen, step wizardStep) *WizardScreen {
	t.Helper()
	for i := 0; i <= len(wizardSteps); i++ {
		if s.step == step {
			return s
		}
		next, handled := pressEsc(t, s)
		if !handled {
			t.Fatalf("esc stopped at %v before reaching %v", s.step, step)
		}
		s = next
	}
	t.Fatalf("step %v was never reached", step)
	return s
}

// Requirement notes ride on the row and the review page grows a "Check"
// block, both of which are new ways to overflow a narrow terminal.
func TestWizardWithRequirementsFitsNarrowTerminals(t *testing.T) {
	for _, size := range []struct{ width, height int }{{80, 21}, {60, 21}, {44, 21}} {
		for _, step := range []wizardStep{stepShaders, stepAddons, stepReview} {
			s := depsWizard(t, stepAddons)
			s = press(t, s, 'a')
			s = toggleRow(t, s, &s.addons, autoHDR)
			// Leave the requirement unmet, which is the widest case: the
			// row carries a "needs …" note and Review grows a Check block.
			s = pressEscTo(t, s, stepShaders)
			s = toggleRow(t, s, &s.packages, "reshade-hdr-shaders-by-lilium")
			if step != stepShaders {
				s = advance(t, s, step)
			}

			env := Env{Styles: NewStyles(true), Width: size.width, Height: size.height}
			body := s.View(env)
			if got := countLines(body); got > env.Height {
				t.Errorf("%dx%d, step %v: rendered %d lines into a height of %d:\n%s",
					size.width, size.height, step, got, env.Height, body)
			}
			for _, line := range strings.Split(body, "\n") {
				if lipgloss.Width(line) > size.width {
					t.Errorf("%dx%d, step %v: line is %d columns wide:\n%q",
						size.width, size.height, step, lipgloss.Width(line), line)
				}
			}
		}
	}
}

// gameWithExistingFiles returns an entry rooted at a temp dir holding the
// named files beside the executable, so the wizard's preflight has
// something real to stat.
func gameWithExistingFiles(t *testing.T, files map[string]int) GameEntry {
	t.Helper()
	root := t.TempDir()
	for name, size := range files {
		abs := filepath.Join(root, "Game", name)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entry := sampleGameEntry()
	entry.Root = root
	return entry
}

// The review page has to say what is already in the folder before the user
// commits, not after: installing into a folder whose dxgi.dll belongs to
// another injector completes successfully and leaves ReShade not loading.
func TestWizardReviewShowsWhatIsAlreadyInTheFolder(t *testing.T) {
	entry := gameWithExistingFiles(t, map[string]int{"dxgi.dll": 25872384})
	s := advance(t, loadWizard(t, entry, 0, fakeDeps()), stepReview)

	body := s.View(wizardEnv())
	if !strings.Contains(body, "Already in this folder") {
		t.Fatalf("review should list existing files:\n%s", body)
	}
	if !strings.Contains(body, "dxgi.dll") || !strings.Contains(body, "kept, so ReShade will not load") {
		t.Errorf("review should name the file and say what keeping it costs:\n%s", body)
	}
	if !strings.Contains(body, "Turn on Overwrite") {
		t.Errorf("review should say how to fix it:\n%s", body)
	}
	if !strings.Contains(body, "24.7 MiB") {
		t.Errorf("review should show the size, which is what identifies the file:\n%s", body)
	}
}

// With overwrite on, the same file is reported as replaced-and-recoverable
// — the backup is the reason overwriting is not a one-way door.
func TestWizardReviewSaysForeignFilesAreBackedUpWhenOverwriting(t *testing.T) {
	entry := gameWithExistingFiles(t, map[string]int{"dxgi.dll": 25872384})
	s := advance(t, loadWizard(t, entry, 0, fakeDeps()), stepReview)
	s = pressSpecial(t, s, tea.KeySpace) // the overwrite checkbox

	body := s.View(wizardEnv())
	if !strings.Contains(body, install.BackupSuffix) {
		t.Errorf("review should name the backup file:\n%s", body)
	}
	if !strings.Contains(body, "put back when you uninstall") {
		t.Errorf("review should say the backup is restored on uninstall:\n%s", body)
	}
	if strings.Contains(body, "will not load") {
		t.Errorf("with overwrite on, nothing is kept:\n%s", body)
	}
}

// A folder with nothing in the way says nothing at all — the section is
// only worth its lines when there is a decision behind it.
func TestWizardReviewOmitsTheSectionForACleanFolder(t *testing.T) {
	entry := gameWithExistingFiles(t, nil)
	s := advance(t, loadWizard(t, entry, 0, fakeDeps()), stepReview)

	if body := s.View(wizardEnv()); strings.Contains(body, "Already in this folder") {
		t.Errorf("a clean folder should not grow an empty section:\n%s", body)
	}
}

// The conflicts block adds long lines (a path, a size, an explanation) to
// the page that was already tightest.
func TestWizardReviewWithConflictsFitsNarrowTerminals(t *testing.T) {
	entry := gameWithExistingFiles(t, map[string]int{
		"dxgi.dll": 25872384, "d3dcompiler_47.dll": 4493352, "ReShade.ini": 500,
	})
	for _, size := range []struct{ width, height int }{{80, 21}, {60, 21}, {44, 21}} {
		for _, overwrite := range []bool{false, true} {
			s := advance(t, loadWizard(t, entry, 0, fakeDeps()), stepReview)
			if overwrite {
				s = pressSpecial(t, s, tea.KeySpace)
			}
			env := Env{Styles: NewStyles(true), Width: size.width, Height: size.height}
			body := s.View(env)
			if got := countLines(body); got > env.Height {
				t.Errorf("%dx%d (overwrite=%v): rendered %d lines into %d:\n%s",
					size.width, size.height, overwrite, got, env.Height, body)
			}
			for _, line := range strings.Split(body, "\n") {
				if lipgloss.Width(line) > size.width {
					t.Errorf("%dx%d (overwrite=%v): line is %d columns wide:\n%q",
						size.width, size.height, overwrite, lipgloss.Width(line), line)
				}
			}
		}
	}
}

// Backups start on, and the request says so — the only irreversible thing
// an install does must be chosen, not defaulted into.
func TestWizardBackupIsOnByDefault(t *testing.T) {
	s := advance(t, loadWizard(t, sampleGameEntry(), 0, fakeDeps()), stepReview)

	if !s.backup() {
		t.Error("the backup option should start on")
	}
	req, ok := s.buildRequest()
	if !ok {
		t.Fatal("buildRequest() failed")
	}
	if req.NoBackup {
		t.Error("Request.NoBackup should be false while the backup option is on")
	}
}

// Turning backups off is allowed, reaches the request, and the review page
// says plainly what it costs.
func TestWizardBackupCanBeTurnedOff(t *testing.T) {
	entry := gameWithExistingFiles(t, map[string]int{"dxgi.dll": 25872384})
	s := advance(t, loadWizard(t, entry, 0, fakeDeps()), stepReview)
	s = pressSpecial(t, s, tea.KeySpace) // overwrite on
	s = pressSpecial(t, s, tea.KeyDown)
	s = pressSpecial(t, s, tea.KeySpace) // backup off

	req, ok := s.buildRequest()
	if !ok {
		t.Fatal("buildRequest() failed")
	}
	if !req.NoBackup {
		t.Error("Request.NoBackup should be true once the backup option is off")
	}

	body := s.View(wizardEnv())
	if !strings.Contains(body, "original discarded") || !strings.Contains(body, "gone for good") {
		t.Errorf("review should say the originals are not recoverable:\n%s", body)
	}
}

// Overwriting is a fresh decision each time it is turned on, so the safe
// default comes back with it rather than inheriting an earlier "no
// backups" from a decision the user may not remember making.
func TestWizardTurningOverwriteBackOnRestoresBackups(t *testing.T) {
	s := advance(t, loadWizard(t, sampleGameEntry(), 0, fakeDeps()), stepReview)

	s = pressSpecial(t, s, tea.KeySpace) // overwrite on
	s = pressSpecial(t, s, tea.KeyDown)
	s = pressSpecial(t, s, tea.KeySpace) // backup off
	s = pressSpecial(t, s, tea.KeyUp)
	s = pressSpecial(t, s, tea.KeySpace) // overwrite off
	if s.backup() {
		t.Fatal("turning overwrite off should leave the backup choice alone")
	}
	s = pressSpecial(t, s, tea.KeySpace) // overwrite on again

	if !s.backup() {
		t.Error("turning overwrite back on should restore backups to the safe default")
	}
}

// While nothing is being overwritten the option does nothing, and says so
// rather than presenting a checkbox with no effect.
func TestWizardBackupOptionSaysItOnlyAppliesWhenOverwriting(t *testing.T) {
	s := advance(t, loadWizard(t, sampleGameEntry(), 0, fakeDeps()), stepReview)

	if !strings.Contains(s.View(wizardEnv()), "only when overwriting") {
		t.Errorf("the backup row should say when it applies:\n%s", s.View(wizardEnv()))
	}
	s = pressSpecial(t, s, tea.KeySpace)
	if strings.Contains(s.View(wizardEnv()), "only when overwriting") {
		t.Errorf("with overwrite on, the caveat is wrong:\n%s", s.View(wizardEnv()))
	}
}

// The add-on build is the one anti-cheat can detect, so it is opted into
// rather than defaulted into: with nothing configured, the wizard starts
// on the normal build.
func TestWizardDefaultsToTheNormalBuild(t *testing.T) {
	deps := fakeDeps()
	deps.Defaults.ReshadeFlavor = "" // nothing configured
	s := loadWizard(t, sampleGameEntry(), 0, deps)

	if s.flavor != install.FlavorNormal {
		t.Errorf("flavor = %q, want normal by default", s.flavor)
	}
	if strings.Contains(s.View(wizardEnv()), anticheatWarning) {
		t.Error("the anti-cheat warning belongs to the add-on build, which is not selected")
	}

	// A configured preference still wins, and so does an existing install.
	deps.Defaults.ReshadeFlavor = "addon"
	if s := loadWizard(t, sampleGameEntry(), 0, deps); s.flavor != install.FlavorAddon {
		t.Errorf("flavor = %q, want the configured addon default", s.flavor)
	}
}

// Two lists side by side read as one wrapped list until something draws
// the line between them.
func TestWizardReShadeStepDrawsEachBuildInItsOwnBox(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	body := s.View(wizardEnv())

	// Two boxes on the same row: the top border appears twice before the
	// first version row does.
	first := strings.SplitN(body, "\n", 5)
	var borders int
	for _, line := range first {
		borders += strings.Count(line, "╭")
	}
	if borders != 2 {
		t.Errorf("expected two panel boxes on the ReShade step, found %d:\n%s", borders, body)
	}
}

// editWizard loads a wizard over a folder that already has an install.
func editWizard(t *testing.T, packages ...string) *WizardScreen {
	t.Helper()
	if packages == nil {
		packages = []string{"standard-effects", "sweetfx-by-ceejay-dk"}
	}
	entry := sampleGameEntry()
	entry.Exes[0].Installed = &state.Install{
		Exe:      "Game/emberhollow.exe",
		ReShade:  state.ReShadeInfo{Version: "6.7.3", Flavor: "normal", DLL: "dxgi.dll"},
		Packages: packages,
	}
	return loadWizard(t, entry, 0, fakeDeps())
}

// Editing an install opens on a summary of what is installed, not on step
// one of five: adding a shader should not mean re-confirming four answers
// that are already right.
func TestEditingOpensOnASummaryOfWhatIsInstalled(t *testing.T) {
	s := editWizard(t)

	if s.step != stepHub {
		t.Fatalf("step = %v, want the summary", s.step)
	}
	// Each section is a pane listing what is actually in it, not a label
	// with a truncated value beside it.
	body := s.View(wizardEnv())
	for _, want := range []string{
		"Editing install", "ReShade", "6.7.3", "normal build",
		"API", "dxgi.dll", "Shaders", "Standard effects", "SweetFX by CeeJay.dk",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the summary should show %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "1 ReShade › 2 API") {
		t.Errorf("editing is not a numbered walk, so it has no breadcrumb:\n%s", body)
	}
}

// Opening one section, changing it, and pressing enter comes back to the
// summary rather than marching on through the remaining steps.
func TestEditingASectionReturnsToTheSummary(t *testing.T) {
	s := openSection(t, editWizard(t), stepShaders)
	if s.step != stepShaders {
		t.Fatalf("step = %v, want the shaders list", s.step)
	}

	s = pressSpecial(t, s, tea.KeyDown)
	s = pressSpecial(t, s, tea.KeySpace)
	s = pressSpecial(t, s, tea.KeyEnter)

	if s.step != stepHub {
		t.Fatalf("step after enter = %v, want back at the summary", s.step)
	}
	// And esc from a section does the same, rather than stepping to an
	// earlier question in an order the user never walked.
	s = openSection(t, s, stepAPI)
	s, handled := pressEsc(t, s)
	if !handled || s.step != stepHub {
		t.Errorf("esc from a section: handled=%v step=%v, want back at the summary", handled, s.step)
	}
}

// The summary says what applying would actually change, so "apply" is
// never a blind commit.
func TestEditingSummaryShowsWhatWouldChange(t *testing.T) {
	s := editWizard(t)
	if !strings.Contains(s.View(wizardEnv()), "no changes") {
		t.Errorf("an untouched install has no changes to report:\n%s", s.View(wizardEnv()))
	}

	// Drop a shader.
	s = openSection(t, s, stepShaders)
	s = pressSpecial(t, s, tea.KeyDown)
	s = pressSpecial(t, s, tea.KeySpace)
	s = pressSpecial(t, s, tea.KeyEnter)
	if got := s.changes(); len(got) != 1 || got[0] != "-1 shader" {
		t.Errorf("changes() = %v, want one removed shader", got)
	}

	// And a different version, from the other build.
	s = openSection(t, s, stepReShade)
	s = pressSpecial(t, s, tea.KeyRight)
	s = pressSpecial(t, s, tea.KeyUp)
	s = pressSpecial(t, s, tea.KeyEnter)

	body := s.View(wizardEnv())
	for _, want := range []string{"6.7.3 → 6.8.0", "addon build", "-1 shader"} {
		if !strings.Contains(body, want) {
			t.Errorf("the summary should report %q:\n%s", want, body)
		}
	}
}

// Switching to the add-on build while editing makes the add-ons section
// appear, since that build is the only one that can load them.
func TestEditingSummaryGainsAddonsWithTheAddonBuild(t *testing.T) {
	s := editWizard(t)
	if slices.Contains(s.hubSections(), stepAddons) {
		t.Fatal("the normal build cannot load add-ons, so the section should be absent")
	}

	s = openSection(t, s, stepReShade)
	s = pressSpecial(t, s, tea.KeyRight)
	s = pressSpecial(t, s, tea.KeyEnter)

	if !slices.Contains(s.hubSections(), stepAddons) {
		t.Errorf("sections = %v, want add-ons once the add-on build is chosen", s.hubSections())
	}
}

// Esc at the summary leaves the wizard, the way esc at step one does when
// installing.
func TestEditingEscAtTheSummaryLeaves(t *testing.T) {
	if _, handled := pressEsc(t, editWizard(t)); handled {
		t.Error("esc at the summary should fall through and pop the screen")
	}
}

// A fresh install is still the linear walk: there is nothing to summarize
// and every answer has to be given once.
func TestFreshInstallStillWalksTheSteps(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	if s.editing || s.step != stepReShade {
		t.Fatalf("a fresh install starts at step %v (editing=%v), want the ReShade step", s.step, s.editing)
	}
	if s := pressSpecial(t, s, tea.KeyEnter); s.step != stepAPI {
		t.Errorf("enter went to %v, want the next step", s.step)
	}
}

// The summary is a list like any other and must not overflow a small
// terminal, and neither must a section reached from it.
func TestEditingFitsNarrowTerminals(t *testing.T) {
	for _, size := range []struct{ width, height int }{{80, 21}, {60, 21}, {44, 21}} {
		for _, step := range []wizardStep{stepHub, stepReShade, stepAPI, stepShaders, stepReview} {
			s := editWizard(t)
			if step != stepHub {
				s = openSection(t, s, step)
			}
			env := Env{Styles: NewStyles(true), Width: size.width, Height: size.height}
			body := s.View(env)
			if got := countLines(body); got > env.Height {
				t.Errorf("%dx%d, %v: rendered %d lines into %d:\n%s",
					size.width, size.height, step, got, env.Height, body)
			}
			for _, line := range strings.Split(body, "\n") {
				if lipgloss.Width(line) > size.width {
					t.Errorf("%dx%d, %v: line is %d columns wide:\n%q",
						size.width, size.height, step, lipgloss.Width(line), line)
				}
			}
		}
	}
}

// The summary has a whole screen to work with, so it lists what is in
// each section rather than counting it — and marks the sections that
// differ from what is recorded, so a change is visible where it happened.
func TestEditingSummaryListsSelectionsInPanes(t *testing.T) {
	s := editWizard(t, "standard-effects", "sweetfx-by-ceejay-dk")

	body := s.View(Env{Styles: NewStyles(true), Width: 100, Height: 26})
	for _, want := range []string{"Standard effects", "SweetFX by CeeJay.dk"} {
		if !strings.Contains(body, want) {
			t.Errorf("the shaders pane should list %q rather than count it:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "╭") {
		t.Errorf("sections should be drawn as panes:\n%s", body)
	}

	// A section that differs from the recorded install is marked.
	if s.sectionChanged(stepShaders) {
		t.Error("nothing has changed yet")
	}
	s = openSection(t, s, stepShaders)
	s = pressSpecial(t, s, tea.KeyDown)
	s = pressSpecial(t, s, tea.KeySpace)
	s = pressSpecial(t, s, tea.KeyEnter)
	if !s.sectionChanged(stepShaders) || s.sectionChanged(stepAPI) {
		t.Error("only the section that changed should be marked")
	}
	if !strings.Contains(s.View(Env{Styles: NewStyles(true), Width: 100, Height: 26}), "Shaders •") {
		t.Error("a changed section should be marked in its pane header")
	}
}

// An install running an older build is the thing a summary of an existing
// install is best placed to point out.
func TestEditingSummarySurfacesAnAvailableUpdate(t *testing.T) {
	s := editWizard(t) // recorded at 6.7.3; the catalog's latest is 6.8.0

	if !strings.Contains(s.View(Env{Styles: NewStyles(true), Width: 100, Height: 26}), "6.8.0 available") {
		t.Error("the ReShade pane should say a newer version exists")
	}
}

// A recorded install can name a package the loaded catalog no longer has —
// dropped upstream, renamed, or a partial offline copy. The row would
// simply not exist, and applying would quietly remove it, so the summary
// says so instead.
func TestEditingSummaryNamesPackagesTheCatalogNoLongerHas(t *testing.T) {
	s := editWizard(t, "standard-effects", "some-pack-that-vanished")

	// Asserted on the pane's own lines: at 100 columns a pane is 25 wide,
	// so the rendered row is clipped, and what matters is that the line
	// exists at all.
	var found bool
	for _, line := range s.hubLines(stepShaders, wizardEnv(), 80) {
		if strings.Contains(line, "some-pack-that-vanished") && strings.Contains(line, "not in catalog") {
			found = true
		}
	}
	if !found {
		t.Errorf("the shaders pane should name what it cannot account for: %v",
			s.hubLines(stepShaders, wizardEnv(), 80))
	}
	// And applying would indeed drop it, which the Apply line must admit.
	if got := s.changes(); len(got) != 1 || got[0] != "-1 shader" {
		t.Errorf("changes() = %v, want the vanished package counted as a removal", got)
	}
}

// The focused pane must be the bright one. Styles.Panel's own border is
// dimmer than Styles.Faint, so dimming only the unfocused borders made
// them the ones that stood out — the inverse of what focus should mean.
func TestEditingSummaryHighlightsTheFocusedPane(t *testing.T) {
	s := editWizard(t)
	env := Env{Styles: NewStyles(true), Width: 100, Height: 24}
	accent := lipgloss.NewStyle().Foreground(env.Styles.Accent.GetForeground()).Render("─")
	accentSeq, _, _ := strings.Cut(strings.TrimPrefix(accent, "\x1b["), "m")

	body := s.View(env)
	var borders []string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "╭") {
			borders = append(borders, line)
		}
	}
	if len(borders) != 1 {
		t.Fatalf("expected one row of pane tops, found %d", len(borders))
	}
	// Exactly one of the three boxes carries the accent color.
	if n := strings.Count(borders[0], accentSeq); n != 1 {
		t.Errorf("%d of the panes are highlighted, want just the focused one:\n%q", n, borders[0])
	}

	// And the marker sits on the same pane as the highlight.
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "▸ ReShade") && !strings.Contains(line, accentSeq) {
			t.Errorf("the focused pane's header is not in the accent color:\n%q", line)
		}
	}
}

// The sections are side by side, so ←/→ is what moves between them.
func TestEditingSummaryMovesWithLeftAndRight(t *testing.T) {
	s := editWizard(t)
	env := Env{Styles: NewStyles(true), Width: 100, Height: 24}

	if got := s.hubCursor.Cursor(); got != 0 {
		t.Fatalf("cursor starts at %d, want 0", got)
	}
	s = pressSpecial(t, s, tea.KeyRight)
	if got := s.hubCursor.Cursor(); got != 1 {
		t.Errorf("→ moved to %d, want 1", got)
	}
	s = pressSpecial(t, s, tea.KeyLeft)
	if got := s.hubCursor.Cursor(); got != 0 {
		t.Errorf("← moved to %d, want 0", got)
	}
	if !strings.Contains(s.View(env), "←→ move") {
		t.Error("the footer should name the keys that actually move between panes")
	}

	// Stacked into a list on a narrow terminal, ↑/↓ is the natural pair —
	// and the hint follows the layout.
	narrow := Env{Styles: NewStyles(true), Width: 50, Height: 24}
	if !s.hubStacked(narrow) {
		t.Fatal("50 columns should not fit three panes")
	}
	if !strings.Contains(s.View(narrow), "↑↓ move") {
		t.Error("the stacked layout should offer ↑↓")
	}
	if s := pressSpecial(t, s, tea.KeyDown); s.hubCursor.Cursor() != 1 {
		t.Error("↓ should still move, whichever layout is showing")
	}
}

// A build the user did not pick on this screen has to say why it is
// picked. The case this comes from: a config written before the default
// changed still asked for the add-on build, so the wizard opened on it
// every time and looked simply wrong — the setting that caused it is in a
// file, or two screens away.
func TestReShadeStepSaysWhereAPreselectedBuildCameFrom(t *testing.T) {
	env := Env{Styles: NewStyles(true), Width: 96, Height: 20}
	entry := sampleEntries()[0]

	t.Run("from config", func(t *testing.T) {
		deps := fakeDeps()
		deps.Defaults = config.DefaultsConfig{ReshadeFlavor: "addon"}
		w := NewWizardScreen(entry, entry.Exes[0], deps)
		w.applyWizardData(wizardDataLoadedMsg{data: sampleWizardData()})

		body := w.View(env)
		if !strings.Contains(body, "defaults.reshade_flavor") {
			t.Errorf("the note should name the setting responsible:\n%s", body)
		}
		if !strings.Contains(body, "settings") {
			t.Errorf("the note should say where to change it:\n%s", body)
		}
	})

	t.Run("the safe default explains itself", func(t *testing.T) {
		deps := fakeDeps()
		deps.Defaults = config.DefaultsConfig{ReshadeFlavor: "normal"}
		w := NewWizardScreen(entry, entry.Exes[0], deps)
		w.applyWizardData(wizardDataLoadedMsg{data: sampleWizardData()})

		if body := w.View(env); strings.Contains(body, "reshade_flavor") {
			t.Errorf("the default build needs no note:\n%s", body)
		}
	})

	t.Run("from the existing install", func(t *testing.T) {
		installed := sampleEntries()[0]
		exe := installed.Exes[0]
		exe.Installed = &state.Install{
			Exe:     exe.Path,
			ReShade: state.ReShadeInfo{Version: "6.8.0", Flavor: "addon", DLL: "dxgi.dll"},
		}
		w := NewWizardScreen(installed, exe, fakeDeps())
		w.applyWizardData(wizardDataLoadedMsg{data: sampleWizardData()})
		w.step = stepReShade

		body := w.View(env)
		if !strings.Contains(body, "already has") {
			t.Errorf("editing an install should say the build came from it:\n%s", body)
		}
	})
}

// The note takes a row, so the panes must give one up rather than the
// page growing past the window.
func TestReShadeStepFitsWithTheNote(t *testing.T) {
	entry := sampleEntries()[0]
	deps := fakeDeps()
	deps.Defaults = config.DefaultsConfig{ReshadeFlavor: "addon"}
	w := NewWizardScreen(entry, entry.Exes[0], deps)
	w.applyWizardData(wizardDataLoadedMsg{data: sampleWizardData()})

	for _, height := range []int{16, 20, 30} {
		env := Env{Styles: NewStyles(true), Width: 96, Height: height}
		if got := countLines(w.View(env)); got > height {
			t.Errorf("height %d: rendered %d lines", height, got)
		}
	}
}
