package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const lastHorizon = `mode = "dark"

accent = "#b59790"   # muted rose
selection = "#584e51"
background = "#0c0b0c"
hyprland_active_border = "rgba(8a8588ee) rgba(e2dddcee)"
red = "#c38b7b"
`

func TestParseColors(t *testing.T) {
	l := ParseColors(strings.NewReader(lastHorizon))
	if !l.HasMode || !l.Dark {
		t.Errorf("mode: %+v", l)
	}
	if !l.HasAccent || l.Accent != [3]uint8{0xb5, 0x97, 0x90} {
		t.Errorf("accent: %+v (the # inside the quotes must stay, the comment after must go)", l)
	}
}

func TestParseColorsWithoutModeUsesTheBackground(t *testing.T) {
	light := ParseColors(strings.NewReader("accent = \"#336699\"\nbackground = \"#fafafa\"\n"))
	if !light.HasMode || light.Dark {
		t.Errorf("a pale background is a light theme: %+v", light)
	}
	dark := ParseColors(strings.NewReader("background = '#101010'\n"))
	if !dark.HasMode || !dark.Dark || dark.HasAccent {
		t.Errorf("dark background, no accent: %+v", dark)
	}
}

func TestParseColorsIgnoresWhatItDoesNotKnow(t *testing.T) {
	l := ParseColors(strings.NewReader("[palette]\nmode = \"sepia\"\naccent = \"red\"\nnonsense\naccent = \"#12\"\n"))
	if l.HasMode || l.HasAccent {
		t.Errorf("nothing valid in there: %+v", l)
	}
}

func TestReadOmarchyFollowsTheSwitch(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("HOME", t.TempDir())
	if _, _, ok := ReadOmarchy(); ok {
		t.Fatal("no Omarchy, no look")
	}
	cur := filepath.Join(state, "omarchy", "current")
	write := func(gen, colors string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(cur, "theme"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cur, "theme", "colors.toml"), []byte(colors), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cur, "theme.generation"), []byte(gen+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("1", lastHorizon)
	l, s1, ok := ReadOmarchy()
	if !ok || !l.Dark || s1 == "" {
		t.Fatalf("first read: %+v %q %v", l, s1, ok)
	}
	write("2", "mode = \"light\"\naccent = \"#336699\"\n")
	l, s2, ok := ReadOmarchy()
	if !ok || l.Dark || s2 == s1 {
		t.Fatalf("after the switch: %+v %q %v", l, s2, ok)
	}
	// the same theme applied again still counts as a switch
	write("3", "mode = \"light\"\naccent = \"#336699\"\n")
	if _, s3, _ := ReadOmarchy(); s3 == s2 {
		t.Error("the stamp must change on every switch")
	}
	// half done: the theme directory is gone
	if err := os.RemoveAll(filepath.Join(cur, "theme")); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := ReadOmarchy(); ok {
		t.Error("a missing theme must not read as a look")
	}
}
