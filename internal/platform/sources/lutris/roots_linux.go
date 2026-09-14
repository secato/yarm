//go:build linux

package lutris

import (
	"os"
	"path/filepath"
)

// DefaultDBPaths returns the candidate pga.db locations on Linux: the
// native install and the Flatpak one. Lutris itself is Linux-only, and
// the native path honors XDG_DATA_HOME because Lutris, through GLib,
// does.
func DefaultDBPaths() []string {
	var dirs []string
	if dataHome := xdgDataHome(); dataHome != "" {
		dirs = append(dirs, filepath.Join(dataHome, "lutris", "pga.db"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		// The Flatpak sandbox keeps its data under ~/.var/app
		// regardless of XDG settings.
		dirs = append(dirs, filepath.Join(home, ".var", "app", "net.lutris.Lutris", "data", "lutris", "pga.db"))
	}
	return dirs
}

// xdgDataHome returns the directory GLib would treat as the user's
// data directory, or "" if none can be resolved. A relative
// XDG_DATA_HOME is ignored, per the spec: honoring it would resolve
// against yarm's working directory.
func xdgDataHome() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" && filepath.IsAbs(xdg) {
		return xdg
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share")
}
