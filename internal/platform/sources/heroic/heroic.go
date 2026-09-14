// Package heroic reads the Heroic Games Launcher's config directory.
// Heroic manages GOG games (with its own downloader, gogdl) and Epic
// games on platforms where neither store has a client, and is itself a
// Windows launcher too, so it is a discovery source for the gog and
// epic providers on every OS yarm runs on.
package heroic

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/secato/yarm/internal/fsutil"
	"github.com/secato/yarm/internal/platform/sources"
)

// maxJSONBytes bounds the files read here. They are written by a
// third-party launcher in a directory yarm does not own; the installed
// lists are a few kilobytes and even the full owned-games library
// stays well under this.
const maxJSONBytes = 16 << 20

// Read returns every store game one heroic config directory reports as
// installed. A missing directory yields nothing without an error
// (callers probe the native and Flatpak locations); a corrupt
// installed-games file is an error, while a corrupt title cache only
// costs the titles — the install paths are the part that matters and
// they come from the other file.
func Read(dir string) ([]sources.Entry, error) {
	gogEntries, gogErr := readGOGInstalled(dir)

	// Heroic tracks its Epic games through the legendary client it
	// embeds, whose installed-games file lives under heroic's own
	// config directory — so reading it is part of reading Heroic, not
	// support for standalone legendary.
	epicEntries, epicErr := readEpicInstalled(dir)

	entries := append(gogEntries, epicEntries...)
	return entries, errors.Join(gogErr, epicErr)
}

// readGOGInstalled reads gog_store/installed.json — the authoritative
// list of what Heroic itself installed (the is_installed flag in the
// store-cache library is known to lag behind reality) — and looks the
// titles up in the library cache.
func readGOGInstalled(dir string) ([]sources.Entry, error) {
	installed, err := readGOGInstallList(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	titles := gogTitles(dir)

	var entries []sources.Entry
	for _, g := range installed {
		// DLC installs under the base game's folder; it is not another
		// game to list.
		if g.AppName == "" || g.InstallPath == "" || g.IsDLC {
			continue
		}
		entries = append(entries, sources.Entry{
			Store:   sources.StoreGOG,
			StoreID: g.AppName,
			Title:   titles[g.AppName],
			Root:    g.InstallPath,
		})
	}
	return entries, nil
}

// readGOGInstallList decodes gog_store/installed.json. Modern Heroic
// wraps the array in an object under "installed"; older versions wrote
// the bare array. Both are accepted, and anything else is an error —
// guessing further than that would turn corruption into silence.
func readGOGInstallList(dir string) ([]gogInstall, error) {
	path := filepath.Join(dir, "gog_store", "installed.json")

	data, err := fsutil.ReadFileMax(path, maxJSONBytes)
	if err != nil {
		return nil, fmt.Errorf("heroic: %w", err)
	}

	var wrapped struct {
		Installed []gogInstall `json:"installed"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Installed != nil {
		return wrapped.Installed, nil
	}
	var bare []gogInstall
	if err := json.Unmarshal(data, &bare); err != nil {
		return nil, fmt.Errorf("heroic: %s: not an installed-games file: %w", path, err)
	}
	return bare, nil
}

// gogTitles reads the GOG library cache for display names. Heroic moved
// it between config layouts over its versions, so both the current and
// the legacy location are probed. Any failure returns no titles — the
// caller falls back to the install folder's name.
func gogTitles(dir string) map[string]string {
	for _, rel := range []string{
		filepath.Join("store_cache", "gog_library.json"),
		filepath.Join("store", "gog_library.json"),
	} {
		if titles, ok := readGogLibrary(filepath.Join(dir, rel)); ok {
			return titles
		}
	}
	return nil
}

func readGogLibrary(path string) (map[string]string, bool) {
	data, err := fsutil.ReadFileMax(path, maxJSONBytes)
	if err != nil {
		return nil, false
	}

	var lib struct {
		Games []struct {
			AppName string `json:"app_name"`
			Title   string `json:"title"`
		} `json:"games"`
	}
	if err := json.Unmarshal(data, &lib); err != nil {
		return nil, false
	}

	titles := make(map[string]string, len(lib.Games))
	for _, g := range lib.Games {
		if g.AppName != "" {
			titles[g.AppName] = g.Title
		}
	}
	return titles, true
}

// gogInstall is the subset of Heroic's InstalledInfo that discovery
// needs, with Heroic's own field names.
type gogInstall struct {
	AppName     string `json:"appName"`
	InstallPath string `json:"install_path"`
	IsDLC       bool   `json:"is_dlc"`
}

// readEpicInstalled reads the embedded legendary client's
// installed.json under heroic's config directory: a top-level object
// keyed by app_name, each value carrying the fields of legendary's
// InstalledGame. A missing file is not an error — a Heroic without
// Epic games does not write one — but an oversized or unparsable one
// is.
func readEpicInstalled(dir string) ([]sources.Entry, error) {
	path := filepath.Join(dir, "legendaryConfig", "legendary", "installed.json")

	data, err := fsutil.ReadFileMax(path, maxJSONBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("heroic: %w", err)
	}

	var installed map[string]epicInstall
	if err := json.Unmarshal(data, &installed); err != nil {
		return nil, fmt.Errorf("heroic: %s: %w", path, err)
	}

	var entries []sources.Entry
	for _, g := range installed {
		// DLC has no separate install, and an empty install_path means
		// the entry is stale rather than installed.
		if g.AppName == "" || g.InstallPath == "" || g.IsDLC {
			continue
		}
		entries = append(entries, sources.Entry{
			Store:   sources.StoreEpic,
			StoreID: g.AppName,
			Title:   g.Title,
			Root:    g.InstallPath,
		})
	}
	return entries, nil
}

// epicInstall is the subset of legendary's InstalledGame that
// discovery needs, with legendary's own field names.
type epicInstall struct {
	AppName     string `json:"app_name"`
	Title       string `json:"title"`
	InstallPath string `json:"install_path"`
	IsDLC       bool   `json:"is_dlc"`
}
