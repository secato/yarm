// Package desktop reads what the desktop says about its look that the window toolkit does not
// pass on. For now that is Omarchy's active theme, which it keeps in plain files.
package desktop

import (
	"bufio"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Look is the part of an Omarchy theme yarm follows: whether it is dark and its accent color.
type Look struct {
	Dark      bool // from `mode`, or from the background when the theme has no mode
	HasMode   bool // Dark means something
	Accent    [3]uint8
	HasAccent bool
}

// ParseColors reads Omarchy's colors.toml, a flat file of `key = "#rrggbb"` lines. Only mode,
// accent and background are used; unknown keys and malformed lines are ignored, because themes
// differ in what they ship.
func ParseColors(r io.Reader) Look {
	var l Look
	var bg [3]uint8
	var hasBG bool
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, val, ok := splitLine(sc.Text())
		if !ok {
			continue
		}
		switch key {
		case "mode":
			switch strings.ToLower(val) {
			case "dark":
				l.Dark, l.HasMode = true, true
			case "light":
				l.Dark, l.HasMode = false, true
			}
		case "accent":
			l.Accent, l.HasAccent = parseHex(val)
		case "background":
			bg, hasBG = parseHex(val)
		}
	}
	if !l.HasMode && hasBG {
		l.Dark, l.HasMode = luminance(bg) < 0.4, true
	}
	return l
}

// splitLine splits `key = "value"  # comment`; the # of a color inside the quotes is kept.
func splitLine(line string) (key, val string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' || line[0] == '[' {
		return "", "", false
	}
	k, v, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	key, v = strings.TrimSpace(k), strings.TrimSpace(v)
	if v != "" && (v[0] == '"' || v[0] == '\'') {
		end := strings.IndexByte(v[1:], v[0])
		if end < 0 {
			return "", "", false
		}
		return key, v[1 : 1+end], true
	}
	v, _, _ = strings.Cut(v, "#")
	return key, strings.TrimSpace(v), true
}

func parseHex(s string) ([3]uint8, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return [3]uint8{}, false
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return [3]uint8{}, false
	}
	return [3]uint8{uint8(n >> 16), uint8(n >> 8), uint8(n)}, true
}

func luminance(c [3]uint8) float64 {
	f := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c[0]) + 0.7152*f(c[1]) + 0.0722*f(c[2])
}

// currentDirs are where Omarchy keeps the active theme, newest layout first.
func currentDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" || !filepath.IsAbs(state) {
		state = filepath.Join(home, ".local", "state")
	}
	return []string{
		filepath.Join(state, "omarchy", "current"),
		filepath.Join(home, ".config", "omarchy", "current"),
	}
}

// ReadOmarchy reads the active Omarchy theme. The stamp changes with every theme switch (even
// to the same theme), so the caller can tell when to read again. ok is false when Omarchy is
// not there, or while a switch is half done (the theme directory is replaced, not edited):
// keep the last good look and try again.
func ReadOmarchy() (l Look, stamp string, ok bool) {
	for _, dir := range currentDirs() {
		f, err := os.Open(filepath.Join(dir, "theme", "colors.toml"))
		if err != nil {
			continue
		}
		l = ParseColors(f)
		_ = f.Close()
		if !l.HasMode && !l.HasAccent {
			continue
		}
		return l, stampOf(dir), true
	}
	return Look{}, "", false
}

// stampOf names the state of a theme switch: theme.generation is written after the new theme is
// in place; the theme's name and the file's time stand in for older Omarchy versions.
func stampOf(dir string) string {
	for _, name := range []string{"theme.generation", "theme.name"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return name + ":" + strings.TrimSpace(string(b))
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, "theme", "colors.toml")); err == nil {
		return "mtime:" + fi.ModTime().String()
	}
	return ""
}
