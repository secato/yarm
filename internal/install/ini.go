package install

import (
	"os"
	"path/filepath"
	"strings"
)

// defaultINI is the ReShade.ini YARM writes when a game has none. The
// search paths match the layout the installer creates, and "**" makes
// ReShade recurse, which packages with nested effect directories rely on.
//
// It is written with CRLF line endings: ReShade is a Windows program and
// its own installer writes CRLF, so a file created here for a Proton game
// looks the same as one created natively.
var defaultINISections = []string{
	"[GENERAL]",
	`EffectSearchPaths=.\reshade-shaders\Shaders\**`,
	`TextureSearchPaths=.\reshade-shaders\Textures\**`,
	`PresetPath=.\` + PresetName,
	"",
	"[SCREENSHOT]",
	`SavePath=.\screenshots`,
	"",
}

// DefaultINI returns the contents of a freshly generated ReShade.ini.
func DefaultINI() []byte {
	return []byte(strings.Join(defaultINISections, "\r\n"))
}

// writeINIIfAbsent creates ReShade.ini at path when nothing is there.
//
// An existing ini is the user's configuration — their effect toggles,
// key bindings and preset path — so it is never overwritten. It reports
// whether it wrote the file.
func writeINIIfAbsent(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		// Lost a race with something else creating it: leave theirs.
		if os.IsExist(err) {
			return false, nil
		}
		return false, err
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(DefaultINI()); err != nil {
		return false, err
	}
	return true, f.Sync()
}
