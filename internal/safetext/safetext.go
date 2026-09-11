// Package safetext strips the characters that let a string do something to
// a terminal instead of being read in it.
//
// Almost every name yarm shows comes from a document it does not control:
// a game name out of a Steam .acf, a package name and description out of
// crosire's ini files, a RenoDX mod title out of a JSON index, a version
// banner out of a log a third-party DLL wrote inside a game. Those reach a
// renderer that honors SGR colors and OSC 8 hyperlinks, where an ESC or a
// bare \r is not text: it can repaint other rows, erase the line yarm just
// drew, or turn a name into a link to somewhere else. The same strings are
// also written to yarm's log file and into installs.json.
//
// Cleaning happens where the untrusted document is parsed rather than at
// each place a string is displayed, so everything downstream — the TUI, the
// log, the manifest — holds text that can only be read.
package safetext

import "strings"

// Clean removes control and formatting characters from a string meant to
// be displayed as a single line, leaving every printable rune untouched.
//
// Removed rather than escaped: there is no legitimate name behind an
// escape sequence to preserve, and a visible "\x1b" in a row is noise that
// looks like corruption. What goes:
//
//   - C0 controls and DEL, including \n, \r and \t — a display string is
//     one line, and the three that look harmless are exactly the ones that
//     move a cursor off it.
//   - C1 controls (U+0080–U+009F), which some terminals accept as the
//     single-byte form of the same escape sequences.
//   - Zero-width and bidirectional-override characters, which do not draw
//     anything but change what the rest of the string looks like — an
//     RTL override reverses the visible order of a name without changing
//     what it is.
//
// A string with nothing to remove is returned as is, which is the usual
// case: no allocation for the names that make up every ordinary catalog.
func Clean(s string) string {
	if !strings.ContainsFunc(s, unsafeRune) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if unsafeRune(r) {
			return -1
		}
		return r
	}, s)
}

func unsafeRune(r rune) bool {
	switch {
	case r < 0x20, r == 0x7f: // C0 and DEL
		return true
	case r >= 0x80 && r <= 0x9f: // C1
		return true
	case r >= 0x200b && r <= 0x200f: // zero-width, LRM/RLM
		return true
	case r >= 0x202a && r <= 0x202e: // bidi embedding and overrides
		return true
	case r >= 0x2060 && r <= 0x2064: // word joiner and invisible operators
		return true
	case r >= 0x2066 && r <= 0x2069: // bidi isolates
		return true
	case r == 0xfeff: // BOM used mid-string as zero-width no-break space
		return true
	}
	return false
}
