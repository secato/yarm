package core

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/config"
)

// CustomFound is what a scan of the custom content folder turned up.
type CustomFound struct {
	Dir     string
	Shaders []catalog.Custom
	Addons  []catalog.Custom
}

// ScanCustomContent lists the user's own shader packs and add-ons under dir.
func ScanCustomContent(dir string) (CustomFound, error) {
	found, err := catalog.ScanCustom(dir)
	if err != nil {
		return CustomFound{Dir: dir}, err
	}
	out := CustomFound{Dir: dir}
	for _, c := range found {
		switch c.Kind {
		case catalog.CustomShaders:
			out.Shaders = append(out.Shaders, c)
		case catalog.CustomAddons:
			out.Addons = append(out.Addons, c)
		}
	}
	return out, nil
}

// ExpandPath resolves a leading ~ to the user's home directory, which is how
// people actually type paths.
func ExpandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// FolderCheck is what yarm found out about a folder someone wants to add as
// a game.
type FolderCheck struct {
	Path string
	// Problem is why the path cannot be used, or "".
	Problem string
	// Exes counts the executables a scan would offer.
	Exes int
}

// OK reports whether the folder can be added.
func (f FolderCheck) OK() bool { return f.Path != "" && f.Problem == "" && f.Exes > 0 }

// Message says in a sentence what is wrong, or "".
func (f FolderCheck) Message() string {
	switch {
	case f.Problem != "":
		return f.Problem
	case f.Path != "" && f.Exes == 0:
		return "No game executable found here. Pick the folder that holds the .exe."
	}
	return ""
}

// CheckGameFolder validates a typed or picked folder as the person types, so
// a typo shows at once instead of after a round trip. Empty input is not a
// problem, just nothing yet.
func CheckGameFolder(raw string) FolderCheck {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return FolderCheck{}
	}
	path := ExpandPath(raw)
	if !filepath.IsAbs(path) {
		return FolderCheck{Path: path, Problem: "Enter an absolute path."}
	}
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return FolderCheck{Path: path, Problem: "No such folder."}
	case err != nil:
		return FolderCheck{Path: path, Problem: err.Error()}
	case !info.IsDir():
		return FolderCheck{Path: path, Problem: "That is a file, not a folder."}
	}
	return FolderCheck{Path: path, Exes: countExecutables(path)}
}

// AddManualGame records a folder as a game in the config and writes it. A
// path that is already there is not added twice. It returns the new config.
func AddManualGame(configDir string, cfg config.Config, name, path string) (config.Config, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(path)
	}
	next := cfg
	next.ManualGames = slices.Clone(cfg.ManualGames)
	for _, g := range next.ManualGames {
		if g.Path == path {
			return cfg, fmt.Errorf("%s is already in the list", path)
		}
	}
	next.ManualGames = append(next.ManualGames, config.ManualGame{Name: name, Path: path})
	if err := config.Save(configDir, next); err != nil {
		return cfg, err
	}
	return next, nil
}

// RemoveManualGame drops a folder added by hand from the config.
func RemoveManualGame(configDir string, cfg config.Config, path string) (config.Config, error) {
	next := cfg
	next.ManualGames = slices.DeleteFunc(slices.Clone(cfg.ManualGames), func(g config.ManualGame) bool {
		return g.Path == path
	})
	if err := config.Save(configDir, next); err != nil {
		return cfg, err
	}
	return next, nil
}
