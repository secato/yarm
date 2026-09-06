package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/artifacts"
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
		Game: game.Game{ID: "steam:1245620", Name: "ELDEN RING", Provider: "steam", Root: "/games/ER"},
		Exes: []Executable{
			{Executable: game.Executable{Path: "Game/eldenring.exe", Arch: game.ArchX64, API: game.APID3D12}},
			{Executable: game.Executable{Path: "Game/launcher.exe", Arch: game.ArchX86, API: game.APIVulkan}},
		},
	}
}

// press drives one key through the wizard's Update, using the wizard's
// own HandleBack for the Back binding (mirroring how the shell routes it)
// and Update for everything else.
func press(t *testing.T, s *WizardScreen, key rune) *WizardScreen {
	t.Helper()
	msg := tea.KeyPressMsg{Code: key, Text: string(key)}
	next, _ := s.Update(msg, wizardEnv())
	return next.(*WizardScreen)
}

func pressEsc(t *testing.T, s *WizardScreen) (*WizardScreen, bool) {
	t.Helper()
	next, _, handled := s.HandleBack()
	return next.(*WizardScreen), handled
}

func pressSpecial(t *testing.T, s *WizardScreen, code rune) *WizardScreen {
	t.Helper()
	next, _ := s.Update(tea.KeyPressMsg{Code: code}, wizardEnv())
	return next.(*WizardScreen)
}

// loadWizard drives a freshly constructed wizard through Init so its
// catalog-derived state (packages, addons, version/dll cursors) is
// populated, exactly as the real Bubble Tea loop would before the user
// can press anything.
func loadWizard(t *testing.T, entry GameEntry, preselected int, deps Deps) *WizardScreen {
	t.Helper()
	s := NewWizardScreen(entry, preselected, deps)
	cmd := s.Init()
	if cmd == nil {
		t.Fatal("Init() returned a nil command; nothing would ever load")
	}
	msg := cmd()
	next, _ := s.Update(msg, wizardEnv())
	return next.(*WizardScreen)
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

// Re-opening the wizard on an already-installed executable must edit what
// is there — flavor, version, DLL and packages preselected from the
// recorded install — rather than silently resetting to the configured
// defaults.
func TestWizardEditingExistingInstallPreselectsWhatIsThere(t *testing.T) {
	entry := sampleGameEntry()
	entry.Exes[0].Installed = &state.Install{
		Exe:      "Game/eldenring.exe",
		ReShade:  state.ReShadeInfo{Version: "6.7.3", Flavor: "normal", DLL: "dxgi.dll"},
		Packages: []string{"sweetfx-by-ceejay-dk"},
	}

	deps := fakeDeps()
	deps.Defaults.Packages = nil // would preselect nothing if defaults were used instead
	s := loadWizard(t, entry, 0, deps)

	if s.flavor != install.FlavorNormal {
		t.Errorf("flavor = %q, want normal (from the existing install, not the addon default)", s.flavor)
	}
	if got := s.data.Versions[s.versionCursor.Cursor()].Version; got != "6.7.3" {
		t.Errorf("preselected version = %q, want 6.7.3 (the recorded one, not latest)", got)
	}
	if !s.packages.selected["sweetfx-by-ceejay-dk"] {
		t.Error("the existing install's package should be preselected")
	}
	if got := s.Title(); !strings.Contains(got, "update ReShade") {
		t.Errorf("Title() = %q, want it to say \"update\" while editing an existing install", got)
	}
}

// An adopted install's version is recorded as "unknown (adopted)", which
// never matches a catalog entry — the version step must fall back to
// latest rather than leaving the cursor on nothing.
func TestWizardEditingAdoptedInstallFallsBackToLatestVersion(t *testing.T) {
	entry := sampleGameEntry()
	entry.Exes[0].Installed = &state.Install{
		ReShade: state.ReShadeInfo{Version: "unknown (adopted)", Flavor: "addon", DLL: "dxgi.dll"},
	}
	s := loadWizard(t, entry, 0, fakeDeps())

	if got := s.data.Versions[s.versionCursor.Cursor()]; !got.Latest {
		t.Errorf("preselected version = %+v, want the latest one", got)
	}
}

// The full forward path: Exe -> Version -> DLL -> Packages -> Addons ->
// Review, ending with a Request that reflects every selection made along
// the way.
func TestWizardFullForwardFlowBuildsRequest(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())

	if s.step != stepExe {
		t.Fatalf("step = %v, want stepExe", s.step)
	}
	s = press(t, s, 'k') // no-op, just exercise Up at the top
	s = pressSpecial(t, s, tea.KeyEnter)
	if s.step != stepVersion {
		t.Fatalf("step = %v, want stepVersion", s.step)
	}

	s = pressSpecial(t, s, tea.KeyEnter) // accept the preselected (latest) version
	if s.step != stepDLL {
		t.Fatalf("step = %v, want stepDLL", s.step)
	}
	if got := s.selectedDLL(); got != "dxgi.dll" {
		t.Fatalf("recommended DLL = %q, want dxgi.dll for a D3D12 exe", got)
	}

	s = pressSpecial(t, s, tea.KeyEnter)
	if s.step != stepPackages {
		t.Fatalf("step = %v, want stepPackages", s.step)
	}

	// Select the second package (SweetFX) in addition to the preselected
	// Standard effects.
	s = press(t, s, 'j')
	s = pressSpecial(t, s, ' ')
	s = pressSpecial(t, s, tea.KeyEnter)
	if s.step != stepAddons {
		t.Fatalf("step = %v, want stepAddons (flavor is addon by default)", s.step)
	}

	// Try to select the manual-only add-on (must be refused), then the
	// installable one.
	s = pressSpecial(t, s, ' ') // cursor starts on swapchain-override; select it
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
	if req.Exe.Path != "Game/eldenring.exe" {
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
	if len(req.Addons) != 1 || req.Addons[0] != "swapchain-override-by-crosire" {
		t.Errorf("Addons = %v, want just the installable one", req.Addons)
	}
	if req.TargetOS != artifacts.CurrentTargetOS() {
		t.Errorf("TargetOS = %q, want the current platform", req.TargetOS)
	}
}

// A manual-only add-on must never end up in the built Request, however
// forcefully the user tries to select it.
func TestWizardCannotSelectManualOnlyAddon(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s.step = stepAddons

	// Move to and try to toggle the manual-only add-on (index 1).
	s = press(t, s, 'j')
	s = pressSpecial(t, s, ' ')

	if s.addons.isSelected(1) {
		t.Fatal("a manual-only add-on must not become selectable")
	}
}

// Choosing the "normal" flavor must skip the add-ons step entirely, both
// going forward and coming back.
func TestWizardNormalFlavorSkipsAddonsStep(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s.step = stepVersion
	s = pressSpecial(t, s, tea.KeyTab) // addon -> normal
	if s.flavor != install.FlavorNormal {
		t.Fatalf("flavor = %q after tab, want normal", s.flavor)
	}

	s = pressSpecial(t, s, tea.KeyEnter) // -> DLL
	s = pressSpecial(t, s, tea.KeyEnter) // -> Packages
	s = pressSpecial(t, s, tea.KeyEnter) // should skip Addons -> Review
	if s.step != stepReview {
		t.Fatalf("step = %v, want stepReview (Addons skipped)", s.step)
	}

	back, handled := pressEsc(t, s)
	if !handled {
		t.Fatal("HandleBack() should handle esc mid-wizard")
	}
	if back.step != stepPackages {
		t.Fatalf("stepping back from Review with normal flavor landed on %v, want stepPackages", back.step)
	}
}

// Addons selected while on the addon flavor must not survive a request
// built after switching to normal (the flavor validation in
// install.Request.Validate would otherwise reject it).
func TestWizardSwitchingToNormalDropsAddonsFromRequest(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s.step = stepPackages
	s = pressSpecial(t, s, tea.KeyEnter) // -> Addons
	s = pressSpecial(t, s, ' ')          // select the installable add-on
	s = pressSpecial(t, s, tea.KeyEnter) // -> Review

	// Step back to Version and switch to normal. The flavor is still
	// addon while stepping back, so the immediate previous step is Addons
	// itself, not Packages.
	s, _ = pressEsc(t, s) // Review  -> Addons
	s, _ = pressEsc(t, s) // Addons  -> Packages
	s, _ = pressEsc(t, s) // Packages -> DLL
	s, _ = pressEsc(t, s) // DLL     -> Version
	if s.step != stepVersion {
		t.Fatalf("step = %v, want stepVersion", s.step)
	}
	s = pressSpecial(t, s, tea.KeyTab)

	req, ok := s.buildRequest()
	if !ok {
		t.Fatal("buildRequest() ok = false")
	}
	if err := req.Validate(); err != nil {
		t.Errorf("Request.Validate() = %v, want nil", err)
	}
	if len(req.Addons) != 0 {
		t.Errorf("Addons = %v, want none: the normal flavor cannot carry them", req.Addons)
	}
}

// The add-on build can trip anti-cheat detection — a real account-ban
// risk in an online game — so the version step must warn about it
// whenever addon is the current flavor, and not when normal is.
func TestWizardVersionStepWarnsAboutAddonAntiCheatRisk(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps()) // fakeDeps defaults to addon
	s.step = stepVersion
	if !s.flavor.Addon() {
		t.Fatal("setup: expected the addon flavor by default")
	}
	if body := s.View(wizardEnv()); !strings.Contains(body, anticheatWarning) {
		t.Errorf("addon flavor should show the anti-cheat warning:\n%s", body)
	}

	s = pressSpecial(t, s, tea.KeyTab) // -> normal
	if body := s.View(wizardEnv()); strings.Contains(body, anticheatWarning) {
		t.Errorf("normal flavor should not show the anti-cheat warning:\n%s", body)
	}
}

// HandleBack at the very first step must defer to the shell (pop back to
// the game detail screen), not try to step further back.
func TestWizardBackAtFirstStepDefers(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	_, _, handled := s.HandleBack()
	if handled {
		t.Error("HandleBack() at stepExe should return handled=false")
	}
}

// The DLL step's recommendation follows the selected exe: an unsupported
// API (Vulkan here) gets no safe recommendation and must fall back to
// dxgi.dll while still letting the user pick freely.
func TestWizardUnsupportedAPIStillOffersADLLChoice(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 1, fakeDeps()) // the Vulkan/x86 exe
	s.step = stepExe
	s = pressSpecial(t, s, tea.KeyEnter) // -> Version
	s = pressSpecial(t, s, tea.KeyEnter) // -> DLL

	if got := s.selectedDLL(); got != "dxgi.dll" {
		t.Errorf("fallback DLL = %q, want dxgi.dll", got)
	}
	body := s.View(wizardEnv())
	if !strings.Contains(strings.ToLower(body), "not supported in v1") {
		t.Errorf("the DLL step should explain that vulkan is unsupported:\n%s", body)
	}
}

// A user's manual DLL pick must survive a trip back to the exe step and
// forward again, rather than being silently recomputed.
func TestWizardManualDLLChoiceSurvivesRevisit(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s = pressSpecial(t, s, tea.KeyEnter) // -> Version
	s = pressSpecial(t, s, tea.KeyEnter) // -> DLL

	s = press(t, s, 'j') // move off the recommendation
	if got := s.selectedDLL(); got == "dxgi.dll" {
		t.Fatal("test setup: cursor did not move")
	}
	picked := s.selectedDLL()

	s, _ = pressEsc(t, s)                // -> Version
	s, _ = pressEsc(t, s)                // -> Exe
	s = pressSpecial(t, s, tea.KeyEnter) // -> Version
	s = pressSpecial(t, s, tea.KeyEnter) // -> DLL

	if got := s.selectedDLL(); got != picked {
		t.Errorf("DLL choice = %q after revisiting, want the earlier manual pick %q", got, picked)
	}
}

func TestWizardReviewOverwriteToggle(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s.step = stepReview
	if s.overwrite {
		t.Fatal("overwrite should start off")
	}
	s = press(t, s, 'o')
	if !s.overwrite {
		t.Error("'o' should toggle overwrite on")
	}
	s = press(t, s, 'o')
	if s.overwrite {
		t.Error("'o' should toggle overwrite back off")
	}
}

// Pressing enter at Review must push a ProgressScreen carrying the built
// Request.
func TestWizardReviewEnterPushesProgress(t *testing.T) {
	s := loadWizard(t, sampleGameEntry(), 0, fakeDeps())
	s.step = stepReview

	_, cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, wizardEnv())
	if cmd == nil {
		t.Fatal("enter at Review should return a command")
	}
	msg := cmd()
	push, ok := msg.(pushScreenMsg)
	if !ok {
		t.Fatalf("message = %T, want pushScreenMsg", msg)
	}
	if _, ok := push.screen.(*ProgressScreen); !ok {
		t.Fatalf("pushed screen = %T, want *ProgressScreen", push.screen)
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
}

// A catalog load failure should not crash the wizard; Async routes it
// through the shell's error overlay instead, and the wizard's own state
// stays sane (still on the exe step) so the user is not stuck.
func TestWizardCatalogLoadErrorDoesNotBreakWizard(t *testing.T) {
	deps := fakeDeps()
	deps.WizardData = fakeWizardData{err: errors.New("network unreachable")}
	s := NewWizardScreen(sampleGameEntry(), 0, deps)

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
	// ...but the wizard itself must not get stuck loading forever, and
	// must remain usable (still on the exe step, not stranded).
	if s.loading {
		t.Error("loading should be false once the (failed) load has been handled")
	}
	if s.step != stepExe {
		t.Errorf("step = %v, want stepExe (still usable) after a load error", s.step)
	}
}

// describeMissing is what tells the user, at Review, what an install is
// actually about to download — getting it wrong would either alarm them
// over something already cached or hide a real ~40 MB d3dcompiler fetch.
func TestDescribeMissing(t *testing.T) {
	cache := fakeCacheStatus{
		reshade:  map[string]bool{"6.8.0:addon": true},
		packages: map[string]bool{"standard-effects": true},
		addons:   map[string]bool{},
	}

	got := describeMissing(cache, "6.8.0", true,
		[]string{"standard-effects", "sweetfx-by-ceejay-dk"},
		[]string{"swapchain-override-by-crosire"},
		game.ArchX64, true)

	want := []string{
		"package sweetfx-by-ceejay-dk",
		"add-on swapchain-override-by-crosire",
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
