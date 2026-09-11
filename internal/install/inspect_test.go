package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectRuntimeReadsVersionFromLog(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")
	f.GameFile("Game/"+LogName,
		"12:34:56:789 [1234] | INFO  | Initializing crosire's ReShade version '6.8.0' (64-bit) "+
			"loaded from 'dxgi.dll' into 'emberhollow.exe' ...\n")

	info := InspectRuntime(f.GameDir, "Game/emberhollow.exe")
	if info.Version != "6.8.0" {
		t.Errorf("Version = %q, want 6.8.0", info.Version)
	}
}

func TestInspectRuntimeNoLogIsEmpty(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")

	info := InspectRuntime(f.GameDir, "Game/emberhollow.exe")
	if info.Version != "" {
		t.Errorf("Version = %q, want empty (no log yet)", info.Version)
	}
	if info.ActiveTechniques != nil {
		t.Errorf("ActiveTechniques = %v, want nil", info.ActiveTechniques)
	}
	if info.AvailableEffects != nil {
		t.Errorf("AvailableEffects = %v, want nil", info.AvailableEffects)
	}
}

func TestInspectRuntimeReadsActiveTechniquesFromPreset(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")
	f.GameFile("Game/"+ININame, "[GENERAL]\nPresetPath=.\\"+PresetName+"\n")
	// A literal comma in a technique name is escaped as two commas in a
	// row, per ReShade's own ini_file.cpp — must not be split in half.
	f.GameFile("Game/"+PresetName,
		"Techniques=Deband@Deband.fx,Clarity@Clarity.fx,Comma,,Name@Odd.fx\n"+
			"TechniqueSorting=Deband@Deband.fx,Clarity@Clarity.fx\n")

	info := InspectRuntime(f.GameDir, "Game/emberhollow.exe")
	want := []string{"Deband", "Clarity", "Comma,Name"}
	if len(info.ActiveTechniques) != len(want) {
		t.Fatalf("ActiveTechniques = %v, want %v", info.ActiveTechniques, want)
	}
	for i, w := range want {
		if info.ActiveTechniques[i] != w {
			t.Errorf("ActiveTechniques[%d] = %q, want %q", i, info.ActiveTechniques[i], w)
		}
	}
}

func TestInspectRuntimeIgnoresAbsolutePresetPath(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")
	f.GameFile("Game/"+ININame, `[GENERAL]`+"\n"+`PresetPath=C:\Users\x\ReShadePreset.ini`+"\n")

	info := InspectRuntime(f.GameDir, "Game/emberhollow.exe")
	if info.ActiveTechniques != nil {
		t.Errorf("ActiveTechniques = %v, want nil for an absolute Windows path", info.ActiveTechniques)
	}
}

func TestInspectRuntimeListsAvailableEffects(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")
	f.GameFile("Game/reshade-shaders/Shaders/Deband.fx", "// deband")
	f.GameFile("Game/reshade-shaders/Shaders/Clarity.fx", "// clarity")
	f.GameFile("Game/reshade-shaders/Shaders/ReShade.fxh", "// header, not an effect")

	info := InspectRuntime(f.GameDir, "Game/emberhollow.exe")
	want := []string{"Clarity.fx", "Deband.fx"} // sorted
	if len(info.AvailableEffects) != len(want) {
		t.Fatalf("AvailableEffects = %v, want %v", info.AvailableEffects, want)
	}
	for i, w := range want {
		if info.AvailableEffects[i] != w {
			t.Errorf("AvailableEffects[%d] = %q, want %q", i, info.AvailableEffects[i], w)
		}
	}
}

func TestSplitReshadeListHandlesEscapedCommas(t *testing.T) {
	got := splitReshadeList("a,b,,c,d")
	want := []string{"a", "b,c", "d"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestSplitReshadeListEmpty(t *testing.T) {
	if got := splitReshadeList(""); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// ReShade.ini is written by a third-party DLL running inside a game, so a
// PresetPath climbing out of the game folder must not be followed: the
// strings it yields end up on screen.
func TestInspectRuntimeRefusesPresetPathOutsideTheGame(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")
	f.GameFile("Game/"+ININame, `[GENERAL]`+"\n"+`PresetPath=..\..\secrets.ini`+"\n")

	// A readable file where the traversal points, so only the containment
	// check can be what stops it being read.
	outside := filepath.Join(filepath.Dir(f.GameDir), "secrets.ini")
	if err := os.WriteFile(outside, []byte("Techniques=Leaked@Secret.fx\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	info := InspectRuntime(f.GameDir, "Game/emberhollow.exe")
	if info.ActiveTechniques != nil {
		t.Errorf("ActiveTechniques = %v, want nil: a preset outside the game root was read",
			info.ActiveTechniques)
	}
}

// A preset one level up but still inside the game folder is legitimate —
// ReShade records it that way when it is picked through the overlay.
func TestInspectRuntimeFollowsPresetPathInsideTheGame(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")
	f.GameFile("Game/"+ININame, `[GENERAL]`+"\n"+`PresetPath=..\presets\mine.ini`+"\n")
	f.GameFile("presets/mine.ini", "Techniques=Deband@Deband.fx\n")

	info := InspectRuntime(f.GameDir, "Game/emberhollow.exe")
	want := []string{"Deband"}
	if len(info.ActiveTechniques) != 1 || info.ActiveTechniques[0] != want[0] {
		t.Errorf("ActiveTechniques = %v, want %v", info.ActiveTechniques, want)
	}
}

// The log is written by that same DLL and grows for as long as the game
// runs, and it is read once per folder every time the games screen loads.
func TestInspectRuntimeBoundsTheLogRead(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/emberhollow.exe", "the game")

	banner := "12:34:56:789 [1234] | INFO  | Initializing crosire's ReShade version '6.8.0' " +
		"(64-bit) loaded from 'dxgi.dll' into 'emberhollow.exe' ...\n"
	f.GameFile("Game/"+LogName, banner+strings.Repeat("x", 4*maxLogPrefix))

	info := InspectRuntime(f.GameDir, "Game/emberhollow.exe")
	if info.Version != "6.8.0" {
		t.Errorf("Version = %q, want 6.8.0 from the banner at the top", info.Version)
	}

	// The banner past the cap is not found, which is what proves the read
	// stopped rather than scanning the whole file.
	f2 := newFixture(t)
	f2.GameFile("Game/emberhollow.exe", "the game")
	f2.GameFile("Game/"+LogName, strings.Repeat("x", maxLogPrefix)+banner)
	if v := InspectRuntime(f2.GameDir, "Game/emberhollow.exe").Version; v != "" {
		t.Errorf("Version = %q, want empty: the whole log was read", v)
	}
}
