package app

import (
	"context"
	"errors"

	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/config"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

// fakeLoader supplies a fixed games list, so screen tests never touch a
// real Steam library and always render the same thing.
type fakeLoader struct {
	entries []GameEntry
	err     error
}

func (f fakeLoader) LoadGames(context.Context) ([]GameEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

// errLoader always fails, for the error-path tests.
var errLoader = fakeLoader{err: errors.New("steam library is unreadable")}

// sampleEntries is the fixture games list: one game with an install, one
// without, one native build, and one whose only executables are skipped.
func sampleEntries() []GameEntry {
	installed := state.Install{
		Exe: "Game/eldenring.exe",
		ReShade: state.ReShadeInfo{
			Version: "6.8.0", Flavor: "addon", Arch: "x64", API: "d3d12", DLL: "dxgi.dll",
		},
	}

	return []GameEntry{
		{
			Game: game.Game{
				ID: "steam:870780", Name: "Control Ultimate Edition",
				Provider: "steam", Root: "/games/Control",
			},
			Exes: []Executable{
				{Executable: game.Executable{Path: "Control.exe", Arch: game.ArchX64, API: game.APIDXGI}},
				{Executable: game.Executable{
					Path: "VC_redist.x64.exe", Arch: game.ArchX86,
					API: game.APIUnknown, Skipped: true,
				}},
			},
		},
		{
			Game: game.Game{
				ID: "steam:570", Name: "Dota 2",
				Provider: "steam", Root: "/games/dota 2 beta",
			},
			NativeBuild: true,
		},
		{
			Game: game.Game{
				ID: "steam:1245620", Name: "ELDEN RING",
				Provider: "steam", Root: "/games/ELDEN RING",
			},
			Exes: []Executable{
				{
					Executable: game.Executable{
						Path: "Game/eldenring.exe", Arch: game.ArchX64, API: game.APID3D12,
					},
					Installed: &installed,
				},
				{Executable: game.Executable{
					Path: "Game/start_protected_game.exe", Arch: game.ArchX64, API: game.APIUnknown,
				}},
			},
		},
		{
			Game: game.Game{
				ID: "manual:abc123", Name: "My GOG Game",
				Provider: "manual", Root: "/games/gog",
			},
			Exes: []Executable{
				{Executable: game.Executable{
					Path: "unins000.exe", Arch: game.ArchX86, API: game.APIUnknown, Skipped: true,
				}},
			},
		},
	}
}

// fakeWizardData supplies fixed catalog/custom-content data, so wizard
// tests never reach the network or disk.
type fakeWizardData struct {
	data WizardData
	err  error
}

func (f fakeWizardData) LoadWizardData(context.Context) (WizardData, error) {
	if f.err != nil {
		return WizardData{}, f.err
	}
	return f.data, nil
}

// sampleWizardData is a small but representative catalog: one required
// package, one optional one, a manual-only add-on and an installable one,
// and a version marked latest.
func sampleWizardData() WizardData {
	return WizardData{
		Versions: []catalog.Version{
			{Version: "6.8.0", Latest: true},
			{Version: "6.7.3"},
		},
		Packages: []catalog.Package{
			{ID: "standard-effects", Name: "Standard effects", Required: true},
			{ID: "sweetfx-by-ceejay-dk", Name: "SweetFX by CeeJay.dk"},
		},
		Addons: []catalog.Addon{
			{
				ID: "swapchain-override-by-crosire", Name: "Swap chain override by crosire",
				URL64: "https://example.invalid/swapchain.addon64",
				URL32: "https://example.invalid/swapchain.addon32",
			},
			{ID: "renodx-by-shortfuse", Name: "RenoDX by ShortFuse"}, // manual-only: no URLs at all
		},
	}
}

// fakeCacheStatus lets tests assert exact "cached" badge behavior without
// touching a real cache directory.
type fakeCacheStatus struct {
	reshade     map[string]bool
	packages    map[string]bool
	addons      map[string]bool
	d3dcompiler bool
}

func (f fakeCacheStatus) HasReShade(version string, addon bool) bool {
	flavor := "normal"
	if addon {
		flavor = "addon"
	}
	return f.reshade[version+":"+flavor]
}
func (f fakeCacheStatus) HasPackage(id string) bool     { return f.packages[id] }
func (f fakeCacheStatus) HasAddon(id string) bool       { return f.addons[id] }
func (f fakeCacheStatus) HasD3DCompiler(game.Arch) bool { return f.d3dcompiler }

// fakeUninstaller lets tests control an uninstall's outcome without
// touching a real install or the filesystem.
type fakeUninstaller struct {
	result install.UninstallResult
	err    error
}

func (f fakeUninstaller) Uninstall(install.UninstallRequest) (install.UninstallResult, error) {
	return f.result, f.err
}

// fakeAdopter lets tests control an adopt call's outcome without touching
// installs.json.
type fakeAdopter struct {
	result state.Install
	err    error
}

func (f fakeAdopter) Adopt(game.Game, game.Executable, install.AdoptCandidate) (state.Install, error) {
	return f.result, f.err
}

// fakeDeps returns a Deps wired entirely to in-memory fakes, for tests
// that drive the wizard, progress or uninstall-confirm flows.
func fakeDeps() Deps {
	return Deps{
		WizardData:  fakeWizardData{data: sampleWizardData()},
		Installer:   &fakeInstaller{result: install.Result{Written: []string{"Game/dxgi.dll"}}},
		Uninstaller: fakeUninstaller{result: install.UninstallResult{Removed: []string{"Game/dxgi.dll"}}},
		Adopter:     fakeAdopter{result: state.Install{Files: []state.File{{Path: "dxgi.dll"}, {Path: "ReShade.ini"}}}},
		CacheStatus: fakeCacheStatus{},
		Defaults:    config.DefaultsConfig{ReshadeFlavor: "addon", Packages: []string{"standard"}},
	}
}
