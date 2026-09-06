package install

import (
	"testing"
)

func TestInspectRuntimeReadsVersionFromLog(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/eldenring.exe", "the game")
	f.GameFile("Game/"+LogName,
		"12:34:56:789 [1234] | INFO  | Initializing crosire's ReShade version '6.8.0' (64-bit) "+
			"loaded from 'dxgi.dll' into 'eldenring.exe' ...\n")

	info := InspectRuntime(f.GameDir, "Game/eldenring.exe")
	if info.Version != "6.8.0" {
		t.Errorf("Version = %q, want 6.8.0", info.Version)
	}
}

func TestInspectRuntimeNoLogIsEmpty(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/eldenring.exe", "the game")

	info := InspectRuntime(f.GameDir, "Game/eldenring.exe")
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
	f.GameFile("Game/eldenring.exe", "the game")
	f.GameFile("Game/"+ININame, "[GENERAL]\nPresetPath=.\\"+PresetName+"\n")
	// A literal comma in a technique name is escaped as two commas in a
	// row, per ReShade's own ini_file.cpp — must not be split in half.
	f.GameFile("Game/"+PresetName,
		"Techniques=Deband@Deband.fx,Clarity@Clarity.fx,Comma,,Name@Odd.fx\n"+
			"TechniqueSorting=Deband@Deband.fx,Clarity@Clarity.fx\n")

	info := InspectRuntime(f.GameDir, "Game/eldenring.exe")
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
	f.GameFile("Game/eldenring.exe", "the game")
	f.GameFile("Game/"+ININame, `[GENERAL]`+"\n"+`PresetPath=C:\Users\x\ReShadePreset.ini`+"\n")

	info := InspectRuntime(f.GameDir, "Game/eldenring.exe")
	if info.ActiveTechniques != nil {
		t.Errorf("ActiveTechniques = %v, want nil for an absolute Windows path", info.ActiveTechniques)
	}
}

func TestInspectRuntimeListsAvailableEffects(t *testing.T) {
	f := newFixture(t)
	f.GameFile("Game/eldenring.exe", "the game")
	f.GameFile("Game/reshade-shaders/Shaders/Deband.fx", "// deband")
	f.GameFile("Game/reshade-shaders/Shaders/Clarity.fx", "// clarity")
	f.GameFile("Game/reshade-shaders/Shaders/ReShade.fxh", "// header, not an effect")

	info := InspectRuntime(f.GameDir, "Game/eldenring.exe")
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
