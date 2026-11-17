package core

import (
	"slices"
	"testing"

	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/config"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
)

// freshEntry is a game with nothing installed, owned by the fixture's
// Vantage Point.
func freshEntry() (GameEntry, Executable) {
	e := sampleEntries()[0]
	return e, e.Exes[0]
}

// installedEntry is the fixture game that already has an add-on build.
func installedEntry() (GameEntry, Executable) {
	e := sampleEntries()[2]
	return e, e.Exes[0]
}

func newTestSession(t *testing.T, e GameEntry, exe Executable, deps Deps) *Session {
	t.Helper()
	return NewSession(e, exe, e.Groups, deps, sampleWizardData())
}

// A fresh install starts on the normal build and the latest version, with
// the required pack ticked and the configured defaults added.
func TestSessionFreshInstallDefaults(t *testing.T) {
	e, exe := freshEntry()
	deps := fakeDeps()
	deps.Defaults = config.DefaultsConfig{ReshadeFlavor: "normal", Packages: []string{"sweetfx-by-ceejay-dk"}}
	s := newTestSession(t, e, exe, deps)

	if s.Installed() {
		t.Error("a fresh game should not be Installed")
	}
	if s.Flavor != install.FlavorNormal {
		t.Errorf("Flavor = %q, want normal: the add-on build is opted into, never defaulted", s.Flavor)
	}
	if s.Version != "6.8.0" {
		t.Errorf("Version = %q, want the latest", s.Version)
	}
	if s.DLL != "dxgi.dll" {
		t.Errorf("DLL = %q, want the one recommended for a DXGI executable", s.DLL)
	}
	want := []string{"standard-effects", "sweetfx-by-ceejay-dk"}
	if got := s.SelectedPackages(); !slices.Equal(got, want) {
		t.Errorf("SelectedPackages() = %v, want %v", got, want)
	}
	if !s.Dirty() {
		t.Error("a fresh install is always something to apply")
	}
}

// The configured add-on default only applies to a fresh install: editing an
// install keeps the build it has.
func TestSessionFlavorFromConfigOnlyWhenFresh(t *testing.T) {
	deps := fakeDeps()
	deps.Defaults.ReshadeFlavor = "addon"

	e, exe := freshEntry()
	if s := newTestSession(t, e, exe, deps); s.Flavor != install.FlavorAddon {
		t.Errorf("fresh Flavor = %q, want addon from config", s.Flavor)
	}

	e, exe = installedEntry()
	exe.Installed.ReShade.Flavor = "normal"
	if s := newTestSession(t, e, exe, deps); s.Flavor != install.FlavorNormal {
		t.Errorf("edited Flavor = %q, want the installed normal build", s.Flavor)
	}
}

// Editing starts from what is installed and is not dirty until something
// differs from it.
func TestSessionEditStartsFromTheInstall(t *testing.T) {
	e, exe := installedEntry()
	exe.Installed.Packages = []string{"standard-effects", "sweetfx-by-ceejay-dk"}
	exe.Installed.Addons = []string{"swap-chain-override-by-crosire"}
	s := newTestSession(t, e, exe, fakeDeps())

	if !s.Installed() || s.Flavor != install.FlavorAddon || s.Version != "6.8.0" {
		t.Fatalf("session = installed %v flavor %q version %q", s.Installed(), s.Flavor, s.Version)
	}
	if !s.PackageSelected("sweetfx-by-ceejay-dk") || !s.AddonSelected("swap-chain-override-by-crosire") {
		t.Error("what is installed should start ticked")
	}
	if s.Dirty() {
		t.Errorf("an untouched edit should not be dirty: %v", s.Changes())
	}
	if got := s.Headline(); got != "Up to date" {
		t.Errorf("Headline() = %q, want Up to date", got)
	}

	s.TogglePackage("sweetfx-by-ceejay-dk")
	s.SetVersion("6.7.3")
	if !s.Dirty() {
		t.Fatal("changes should make the edit dirty")
	}
	var texts []string
	for _, c := range s.Changes() {
		texts = append(texts, c.Kind+":"+c.Text)
	}
	want := []string{"swap:ReShade 6.8.0 → 6.7.3", "remove:SweetFX by CeeJay.dk"}
	if !slices.Equal(texts, want) {
		t.Errorf("Changes() = %v, want %v", texts, want)
	}
}

// A required pack cannot be unticked, and a manual-only add-on cannot be
// ticked.
func TestSessionRequiredAndDisabledRowsDoNotToggle(t *testing.T) {
	e, exe := freshEntry()
	s := newTestSession(t, e, exe, fakeDeps())

	s.TogglePackage("standard-effects")
	if !s.PackageSelected("standard-effects") {
		t.Error("the required pack came off")
	}
	s.SetFlavor(install.FlavorAddon)
	s.ToggleAddon("renodx-by-shortfuse")
	if s.AddonSelected("renodx-by-shortfuse") {
		t.Error("a manual-only add-on was ticked")
	}
}

// Add-ons ticked while the add-on build was on offer do not ride along into
// a request for the normal build, which cannot load them.
func TestSessionAddonsDropWithTheNormalBuild(t *testing.T) {
	e, exe := freshEntry()
	s := newTestSession(t, e, exe, fakeDeps())
	s.SetFlavor(install.FlavorAddon)
	s.ToggleAddon("swap-chain-override-by-crosire")
	s.ChooseRenoDX("emberhollow")

	req, ok := s.Request(exe)
	if !ok || len(req.Addons) != 1 {
		t.Fatalf("Request() = %+v, %v; want the add-on", req, ok)
	}

	s.SetFlavor(install.FlavorNormal)
	req, _ = s.Request(exe)
	if len(req.Addons) != 0 || req.RenoDX != "" {
		t.Errorf("normal build kept add-ons %v and RenoDX %q", req.Addons, req.RenoDX)
	}
	if err := req.Validate(); err != nil {
		t.Errorf("the normal-build request should be valid: %v", err)
	}
}

// Ticking a pack that needs another turns the other on and says so.
func TestSessionTickingPullsInRequirements(t *testing.T) {
	data := sampleWizardData()
	data.Packages = append(data.Packages,
		catalog.Package{ID: "immerse-by-marty-mcfly", Name: "iMMERSE"},
		catalog.Package{ID: "shades-by-jakobpcoder", Name: "Shades"})
	e, exe := freshEntry()
	s := NewSession(e, exe, e.Groups, fakeDeps(), data)

	pulled := s.TogglePackage("shades-by-jakobpcoder")
	if !slices.Equal(pulled, []string{"iMMERSE"}) {
		t.Errorf("pulled = %v, want iMMERSE", pulled)
	}
	if !s.PackageSelected("immerse-by-marty-mcfly") {
		t.Error("the requirement was not ticked")
	}

	// Unticking the dependent leaves the dependency alone.
	s.TogglePackage("shades-by-jakobpcoder")
	if !s.PackageSelected("immerse-by-marty-mcfly") {
		t.Error("unticking should leave the dependency")
	}

	var note string
	for _, r := range s.PackageRows(true) {
		if r.ID == "immerse-by-marty-mcfly" {
			note = r.Note
		}
	}
	if note != "" {
		t.Errorf("a dependency nobody wants any more should carry no note, got %q", note)
	}
}

// RenoDX leads with the mod upstream published for this game and does not
// tick it on its own.
func TestSessionRenoDXMatchAndChoice(t *testing.T) {
	e := sampleEntries()[2] // steam:700110
	s := newTestSession(t, e, e.Exes[0], fakeDeps())
	s.SetFlavor(install.FlavorAddon)

	rows := s.RenoDXRows("")
	if len(rows) == 0 || rows[0].ID != "emberhollow" || rows[0].Note != "matches this game" {
		t.Fatalf("rows[0] = %+v, want the matched mod first", rows[0])
	}
	if rows[0].Selected {
		t.Error("the matched mod must not be pre-ticked")
	}
	if m, ok := s.RenoDXMatch(); !ok || m.ID != "emberhollow" {
		t.Errorf("RenoDXMatch() = %v, %v", m.ID, ok)
	}

	if got := s.RenoDXRows("wobb"); len(got) != 1 || got[0].ID != "wobbly" {
		t.Errorf("search wobb = %+v", got)
	}

	s.ChooseRenoDX("vulkangame") // yarm does not support Vulkan mods
	if s.RenoDX != "" {
		t.Errorf("an unsupported mod was chosen: %q", s.RenoDX)
	}
	s.ChooseRenoDX("emberhollow")
	if s.RenoDXChoice() != "emberhollow" || s.RenoDXTitle() != "Ember Hollow" {
		t.Errorf("choice = %q title %q", s.RenoDXChoice(), s.RenoDXTitle())
	}
	s.ChooseRenoDX("")
	if s.RenoDX != "" {
		t.Error("choosing none should clear the mod")
	}
}

// RenoDX beside AutoHDR, and RenoDX on a ReShade that is too old, are
// reported rather than blocked.
func TestSessionUnmetReportsRenoDXProblems(t *testing.T) {
	data := sampleWizardData()
	data.Addons = append(data.Addons, catalog.Addon{
		ID: autoHDRAddonID, Name: "AutoHDR", URL64: "https://example.invalid/a.addon64",
	})
	data.Versions = append(data.Versions, catalog.Version{Version: "5.0.0"})
	e := sampleEntries()[2]
	s := NewSession(e, e.Exes[0], e.Groups, fakeDeps(), data)
	s.SetFlavor(install.FlavorAddon)
	s.ChooseRenoDX("emberhollow")
	s.ToggleAddon(autoHDRAddonID)
	s.SetVersion("5.0.0")

	got := s.Unmet()
	if len(got) < 2 {
		t.Errorf("Unmet() = %v, want the tone-mapping clash and the old ReShade", got)
	}
}

// Custom content goes in the request's Custom list, not among the catalog
// packs, because the pipeline resolves the two from different places.
func TestSessionSplitsCustomContent(t *testing.T) {
	data := sampleWizardData()
	data.CustomShaders = []catalog.Custom{{ID: "custom:shaders:mine", Name: "mine", Kind: catalog.CustomShaders}}
	data.CustomAddons = []catalog.Custom{{ID: "custom:addons:tool", Name: "tool", Kind: catalog.CustomAddons}}
	e, exe := freshEntry()
	s := NewSession(e, exe, e.Groups, fakeDeps(), data)
	s.SetFlavor(install.FlavorAddon)
	s.TogglePackage("custom:shaders:mine")
	s.ToggleAddon("custom:addons:tool")

	req, _ := s.Request(exe)
	if !slices.Equal(req.Custom, []string{"custom:shaders:mine", "custom:addons:tool"}) {
		t.Errorf("Custom = %v", req.Custom)
	}
	for _, id := range slices.Concat(req.Packages, req.Addons) {
		if isCustomID(id) {
			t.Errorf("custom id %q leaked into the catalog lists", id)
		}
	}
	if got := s.Headline(); got != "Installs ReShade 6.8.0, 2 shader packs, 1 add-on" {
		// standard-effects + custom shaders = 2 packs, tool = 1 add-on.
		t.Errorf("Headline() = %q", got)
	}
}

// The shortlist hides catalog noise but never a ticked row, and "all"
// shows everything.
func TestSessionShortlistKeepsTickedRows(t *testing.T) {
	e, exe := freshEntry()
	s := newTestSession(t, e, exe, fakeDeps())

	all := s.PackageRows(true)
	if len(all) != s.PackageTotal() {
		t.Errorf("all rows = %d, total %d", len(all), s.PackageTotal())
	}
	short := s.PackageRows(false)
	for _, r := range short {
		if r.Selected {
			return
		}
	}
	t.Error("the ticked required pack must stay visible on the shortlist")
}

// One Op per checked folder, and an uninstall for a folder that was
// unchecked while holding an install.
func TestSessionOpsMoveAnInstallBetweenFolders(t *testing.T) {
	e := twoFolderGame()
	exe := e.Exes[0] // Release/Game.exe, installed
	s := NewSession(e, exe, e.Groups, fakeDeps(), sampleWizardData())

	ops, ok := s.Ops()
	if !ok || len(ops) != 1 || ops[0].Install == nil || ops[0].Dir != "Release" {
		t.Fatalf("Ops() = %+v, %v; want one install in Release", ops, ok)
	}

	s.ToggleFolder("Ship")
	s.ToggleFolder("Release")
	ops, _ = s.Ops()
	if len(ops) != 2 || ops[0].Dir != "Ship" || ops[0].Install == nil || ops[1].Dir != "Release" || ops[1].Uninstall == nil {
		t.Fatalf("Ops() = %+v, want an install in Ship then an uninstall in Release", ops)
	}
	if got := ops[0].Install.Exe.Path; got != "Ship/Game.exe" {
		t.Errorf("the Ship install targets %q", got)
	}
	if !s.Dirty() {
		t.Error("moving an install is a change")
	}
}

// What would still have to be downloaded follows the cache.
func TestSessionMissingFollowsTheCache(t *testing.T) {
	e, exe := freshEntry()
	deps := fakeDeps()
	deps.CacheStatus = fakeCacheStatus{}
	s := newTestSession(t, e, exe, deps)

	if m := s.Missing(); len(m) == 0 {
		t.Fatal("nothing is cached, so something must be missing")
	}
	if got := s.DownloadNote(); got == "Nothing to download" {
		t.Errorf("DownloadNote() = %q", got)
	}

	deps.CacheStatus = fakeCacheStatus{
		reshade:     map[string]bool{"6.8.0:addon": true},
		packages:    map[string]bool{"standard-effects": true},
		d3dcompiler: true,
	}
	s = newTestSession(t, e, exe, deps)
	if m := s.Missing(); len(m) != 0 {
		t.Errorf("Missing() = %v, want nothing with everything cached", m)
	}
	if got := s.DownloadNote(); got != "Nothing to download" {
		t.Errorf("DownloadNote() = %q", got)
	}
}

// The versions on offer are capped at ten, plus the installed one when it
// has aged out of the window.
func TestSessionVersionsAreCappedKeepingTheInstalledOne(t *testing.T) {
	data := sampleWizardData()
	data.Versions = nil
	for i := 12; i > 0; i-- {
		data.Versions = append(data.Versions, catalog.Version{Version: "6." + string(rune('a'+i)) + ".0", Latest: i == 12})
	}
	e, exe := installedEntry()
	exe.Installed.ReShade.Version = data.Versions[11].Version
	s := NewSession(e, exe, e.Groups, fakeDeps(), data)

	if len(s.Versions()) != 11 {
		t.Errorf("versions = %d, want the ten newest plus the installed one", len(s.Versions()))
	}
	if s.Version != exe.Installed.ReShade.Version {
		t.Errorf("Version = %q, want the installed one", s.Version)
	}
}

func TestSessionDLLOptionsAndRecommendation(t *testing.T) {
	if got := DLLOptions(); len(got) != 6 || got[0].Name != "dxgi.dll" {
		t.Errorf("DLLOptions() = %v", got)
	}
	e := sampleEntries()[0]
	exe := e.Exes[0]
	exe.API = game.APID3D9
	s := newTestSession(t, e, exe, fakeDeps())
	if s.DLL != "d3d9.dll" || s.RecommendedDLL() != "d3d9.dll" {
		t.Errorf("DLL = %q, recommended %q", s.DLL, s.RecommendedDLL())
	}
	s.SetDLL("opengl32.dll")
	if s.DLL != "opengl32.dll" {
		t.Errorf("SetDLL did not stick: %q", s.DLL)
	}
}

func TestDescribeConflict(t *testing.T) {
	foreign := install.Conflict{Path: "Game/dxgi.dll", Kind: install.ConflictForeign, Size: 5 << 20}
	tests := []struct {
		c                 install.Conflict
		overwrite, backup bool
		want              string
	}{
		{install.Conflict{Path: "Game/ReShade.ini", Kind: install.ConflictConfig}, false, false, "ReShade.ini — your settings, kept"},
		{install.Conflict{Path: "Game/dxgi.dll", Kind: install.ConflictManaged}, true, true, "dxgi.dll — yours, replaced"},
		{foreign, true, true, "dxgi.dll (5.0 MiB) — replaced, original saved"},
		{foreign, true, false, "dxgi.dll (5.0 MiB) — replaced, original discarded"},
		{foreign, false, true, "dxgi.dll (5.0 MiB) — kept, so ReShade will not load"},
	}
	for _, tt := range tests {
		if got := DescribeConflict(tt.c, tt.overwrite, tt.backup); got != tt.want {
			t.Errorf("DescribeConflict(%v) = %q, want %q", tt.c.Kind, got, tt.want)
		}
	}
}
