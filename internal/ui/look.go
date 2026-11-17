package ui

import (
	"log/slog"
	"os"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/secato/yarm/internal/config"
)

// The look follows the desktop. Dark or light, in order of say:
//  1. YARM_THEME=dark|light, for when detection is wrong;
//  2. the choice saved from the toggle: dark, light, or system;
//  3. Omarchy's active theme, when Omarchy is there;
//  4. the system's appearance, as MyGo reads it (GTK setting, desktop portal).
//
// The accent does not follow the desktop: it is the brand violet everywhere,
// because it is what tells a YARM window apart and what "will be installed"
// is drawn in.

// ThemeFromEnv reads YARM_THEME: "dark", "light", or "" for the saved choice.
func ThemeFromEnv() string {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv("YARM_THEME"))); v {
	case "dark", "light":
		return v
	}
	return ""
}

// themeChoice is "dark", "light" or "system".
func (a *App) themeChoice() string {
	if a.themeMode != "" {
		return a.themeMode
	}
	switch a.cfg.Theme {
	case "dark", "light":
		return a.cfg.Theme
	}
	return "system"
}

// look is the palette for this frame.
func (a *App) look(c *ui.Context) palette {
	dark := c.Theme().Dark // the system's, before yarm sets its own theme
	if a.omarchy != nil && a.omarchy.HasMode {
		dark = a.omarchy.Dark
	}
	switch a.themeChoice() {
	case "dark":
		dark = true
	case "light":
		dark = false
	}
	return paletteFor(dark)
}

// setTheme saves the look and applies it at once.
func (a *App) setTheme(choice string) {
	if a.cfg.Theme == choice {
		return
	}
	prev := a.cfg
	a.cfg.Theme = choice
	if a.deps.ConfigDir != "" {
		if err := config.Save(a.deps.ConfigDir, a.cfg); err != nil {
			slog.Warn("could not save the look", "error", err)
			a.cfg = prev
			a.toast("Couldn't save the look: " + err.Error())
		}
	}
}
