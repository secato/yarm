package safetext

import "testing"

func TestClean(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain name is untouched", in: "ELDEN RING", want: "ELDEN RING"},
		{name: "accents and symbols survive", in: "Nier: Automata™ — Deluxe", want: "Nier: Automata™ — Deluxe"},
		{name: "SGR color sequence", in: "\x1b[31mRED\x1b[0m", want: "[31mRED[0m"},
		{name: "OSC 8 hyperlink", in: "\x1b]8;;https://evil.example\x07click\x1b]8;;\x07", want: "]8;;https://evil.exampleclick]8;;"},
		{name: "carriage return erases the row", in: "safe\rmalicious", want: "safemalicious"},
		{name: "newline forges a second line", in: "Game\n  Installed: yes", want: "Game  Installed: yes"},
		{name: "tab", in: "a\tb", want: "ab"},
		{name: "C1 single-byte CSI", in: "a\u009bb", want: "ab"},
		{name: "zero width space", in: "a\u200bb", want: "ab"},
		{name: "right-to-left override", in: "shader\u202egnp.xf", want: "shadergnp.xf"},
		{name: "bom mid string", in: "a\ufeffb", want: "ab"},
		{name: "empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Clean(tt.in); got != tt.want {
				t.Errorf("Clean(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Every C0 control goes, not just the ones with a well-known effect: the
// rule is that a display string holds printable text, and an exception
// list is how the next terminal's favorite control character gets in.
func TestCleanDropsEveryC0(t *testing.T) {
	for r := rune(0); r < 0x20; r++ {
		if got := Clean(string(r)); got != "" {
			t.Errorf("Clean(%#U) = %q, want empty", r, got)
		}
	}
	if got := Clean("\u007f"); got != "" {
		t.Errorf("Clean(DEL) = %q, want empty", got)
	}
}

// A clean string must come back as itself rather than a copy: Clean runs
// on every entry of every catalog that is parsed, and the overwhelmingly
// common case is a name with nothing to remove.
func TestCleanReturnsInputUnchanged(t *testing.T) {
	const in = "SweetFX by CeeJay.dk"
	if got := Clean(in); got != in {
		t.Errorf("Clean(%q) = %q", in, got)
	}
}
