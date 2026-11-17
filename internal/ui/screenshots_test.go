package ui

import (
	"os"
	"testing"

	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/core"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

// TestScreenshots writes docs/screenshot.png and docs/screenshot-light.png.
// It only runs when YARM_SCREENSHOTS names the directory to write to
// (scripts/screenshots.sh sets it): a picture is not an assertion.
func TestScreenshots(t *testing.T) {
	dir := os.Getenv("YARM_SCREENSHOTS")
	if dir == "" {
		t.Skip("set YARM_SCREENSHOTS to write the screenshots")
	}
	t.Setenv("YARM_SHOTS", dir)

	for _, look := range []struct{ mode, file string }{{"dark", "screenshot"}, {"light", "screenshot-light"}} {
		a := testApp(t)
		a.themeMode, a.cfg.Theme = "", look.mode
		a.deps.WizardData = fakeData{data: screenshotData()}
		games := screenshotGames()
		a.loader = fakeLoader{games: games}
		tt := open(t, a)
		for _, name := range []string{"Elden Ring", "Shaders", "iMMERSE", "METEOR"} {
			if err := tt.Click(name); err != nil {
				t.Fatal(err)
			}
		}
		tt.Frame()
		shot(t, tt, look.file)
	}
}

func screenshotData() core.WizardData {
	d := sampleData()
	d.Packages = []catalog.Package{
		{ID: "standard-effects", Name: "Standard effects", Required: true, Description: "ReShade's own shaders, which the others lean on."},
		{ID: "sweetfx-by-ceejay-dk", Name: "SweetFX", Description: "Classic tonemap, vibrance and sharpen effects."},
		{ID: "immerse-by-marty-mcfly", Name: "iMMERSE", Description: "Ray-traced lighting, ambient occlusion and sharpening."},
		{ID: "meteor-by-marty-mcfly", Name: "METEOR", Description: "Film grain, bloom and lens effects."},
		{ID: "quint-by-marty-mcfly", Name: "qUINT", Description: "Depth-of-field, bloom and global illumination."},
		{ID: "prod80-by-prod80", Name: "Prod80", Description: "Color grading and tone curves."},
	}
	d.CustomShaders = []catalog.Custom{{ID: "custom:shaders:film-grain-pack", Name: "Film Grain Pack", Description: "Your own shaders from the custom folder.", Kind: catalog.CustomShaders}}
	return d
}

func screenshotGames() []core.GameEntry {
	installed := func(exe, ver, flavor string, packs ...string) *state.Install {
		return &state.Install{
			Exe: exe, Packages: packs,
			ReShade: state.ReShadeInfo{Version: ver, Flavor: flavor, Arch: "x64", API: "d3d12", DLL: "dxgi.dll"},
		}
	}
	mk := func(id, name, provider, root string, in *state.Install, api game.API) core.GameEntry {
		return core.GameEntry{
			Game: game.Game{ID: id, Name: name, Provider: provider, Root: root},
			Exes: []core.Executable{{
				Executable: game.Executable{Path: "Game/" + name + ".exe", Arch: game.ArchX64, API: api},
				Installed:  in,
			}},
		}.WithGroups()
	}
	cp := mk("gog:3", "Cyberpunk 2077", "gog", "/games/Cyberpunk 2077", nil, game.APID3D12)
	cp.Groups[0].Unmanaged = &install.AdoptCandidate{DLLName: "dxgi.dll"}
	er := mk("steam:1245620", "Elden Ring", "steam", "/games/ELDEN RING", installed("Game/Elden Ring.exe", "6.8.0", "addon", "standard-effects", "sweetfx-by-ceejay-dk"), game.APID3D12)
	return []core.GameEntry{
		er,
		mk("steam:1145360", "Hades II", "steam", "/games/Hades II", installed("Game/Hades II.exe", "6.8.0", "normal", "standard-effects"), game.APID3D11),
		cp,
		mk("gog:1", "Baldur's Gate 3", "gog", "/games/BG3", nil, game.APIVulkan),
		mk("epic:1", "Control", "epic", "/games/Control", nil, game.APID3D12),
		mk("battlenet:1", "Diablo IV", "battlenet", "/games/Diablo IV", nil, game.APID3D12),
		mk("manual:1", "My Emulated Game", "manual", "/games/emulated", nil, game.APID3D9),
	}
}
