//go:build linux

package heroic

import (
	"os"
	"path/filepath"
)

// DefaultConfigDirs returns the candidate Heroic config directories on
// Linux: the native install and the Flatpak one. The native path
// honors XDG_CONFIG_HOME because Heroic, an Electron app, does.
func DefaultConfigDirs() []string {
	var dirs []string
	if configHome := xdgConfigHome(); configHome != "" {
		dirs = append(dirs, filepath.Join(configHome, "heroic"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		// The Flatpak sandbox keeps its config under ~/.var/app
		// regardless of XDG settings.
		dirs = append(dirs, filepath.Join(home, ".var", "app", "com.heroicgameslauncher.hgl", "config", "heroic"))
	}
	return dirs
}

// xdgConfigHome returns the directory Electron would treat as the
// user's config directory, or "" if none can be resolved. A relative
// XDG_CONFIG_HOME is ignored, per the spec: honoring it would resolve
// against yarm's working directory.
func xdgConfigHome() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" && filepath.IsAbs(xdg) {
		return xdg
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config")
}
