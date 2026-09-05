// Package steam implements a platform.Provider that discovers games from
// Steam libraries by parsing libraryfolders.vdf and appmanifest_*.acf.
package steam

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/andygrunwald/vdf"

	"github.com/secato/yarm/internal/game"
)

// ProviderName identifies this provider in game.Game.Provider and IDs.
const ProviderName = "steam"

// skipNamePrefixes match Valve-installed helpers, not games, that still
// show up as "apps" in a Steam library (docs/plan/04-external-sources.md §4.5).
var skipNamePrefixes = []string{
	"Proton",
	"Steam Linux Runtime",
	"Steamworks Common Redistributables",
	"SteamVR",
}

// skipAppIDs are the same kind of non-game helper apps, identified by id
// rather than name.
var skipAppIDs = map[string]bool{
	"228980":  true,
	"1070560": true,
	"1391110": true,
	"1628350": true,
	"2180100": true,
	"1493710": true,
}

// Provider discovers Steam games across every Steam install found among
// roots, plus any extraLibraryPaths (from config.Steam.ExtraLibraryPaths)
// that auto-detection might have missed.
type Provider struct {
	roots             []string
	extraLibraryPaths []string
}

// New returns a Provider using this OS's default Steam install roots.
func New(extraLibraryPaths []string) *Provider {
	return &Provider{roots: defaultRoots(), extraLibraryPaths: extraLibraryPaths}
}

// NewWithRoots returns a Provider that probes exactly roots instead of the
// OS defaults. Exported for tests that point Discover at fixture
// directories; production code should use New.
func NewWithRoots(roots, extraLibraryPaths []string) *Provider {
	return &Provider{roots: roots, extraLibraryPaths: extraLibraryPaths}
}

// Name implements platform.Provider.
func (p *Provider) Name() string { return ProviderName }

// Discover implements platform.Provider. Steam not being installed, or a
// root not being a real Steam install, is not an error — Discover simply
// tries the next root and, if none pan out, returns an empty slice.
func (p *Provider) Discover(_ context.Context) ([]game.Game, error) {
	libraries := make(map[string]bool)

	for _, root := range p.roots {
		vdfPath := filepath.Join(root, "steamapps", "libraryfolders.vdf")
		paths, err := parseLibraryFolders(vdfPath)
		if err != nil {
			continue
		}
		for _, lp := range paths {
			libraries[filepath.Clean(lp)] = true
		}
	}
	for _, lp := range p.extraLibraryPaths {
		libraries[filepath.Clean(lp)] = true
	}

	var games []game.Game
	for lib := range libraries {
		games = append(games, scanLibrary(lib)...)
	}
	return games, nil
}

// scanLibrary reads every appmanifest_*.acf in a library and returns the
// installed, non-skipped games it describes whose install directory
// actually exists on disk.
func scanLibrary(libraryRoot string) []game.Game {
	matches, err := filepath.Glob(filepath.Join(libraryRoot, "steamapps", "appmanifest_*.acf"))
	if err != nil {
		return nil
	}

	var games []game.Game
	for _, manifestPath := range matches {
		m, err := parseAppManifest(manifestPath)
		if err != nil || isSkippedApp(m) {
			continue
		}

		root := filepath.Join(libraryRoot, "steamapps", "common", m.installDir)
		if _, err := os.Stat(root); err != nil {
			continue
		}

		games = append(games, game.Game{
			ID:       ProviderName + ":" + m.appID,
			Name:     m.name,
			Provider: ProviderName,
			Root:     root,
		})
	}
	return games
}

func isSkippedApp(m appManifest) bool {
	if skipAppIDs[m.appID] {
		return true
	}
	for _, prefix := range skipNamePrefixes {
		if strings.HasPrefix(m.name, prefix) {
			return true
		}
	}
	return false
}

// parseLibraryFolders reads a libraryfolders.vdf and returns each entry's
// "path" field — one absolute filesystem path per Steam library.
func parseLibraryFolders(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	root, err := vdf.NewParser(f).Parse()
	if err != nil {
		return nil, err
	}

	top, ok := root["libraryfolders"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("steam: %s: missing top-level \"libraryfolders\" key", path)
	}

	var paths []string
	for _, v := range top {
		entry, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		if p, ok := entry["path"].(string); ok && p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

type appManifest struct {
	appID      string
	name       string
	installDir string
}

// parseAppManifest reads one appmanifest_<appid>.acf.
func parseAppManifest(path string) (appManifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return appManifest{}, err
	}
	defer func() { _ = f.Close() }()

	root, err := vdf.NewParser(f).Parse()
	if err != nil {
		return appManifest{}, err
	}

	state, ok := root["AppState"].(map[string]interface{})
	if !ok {
		return appManifest{}, fmt.Errorf("steam: %s: missing top-level \"AppState\" key", path)
	}

	str := func(key string) string {
		s, _ := state[key].(string)
		return s
	}

	m := appManifest{
		appID:      str("appid"),
		name:       str("name"),
		installDir: str("installdir"),
	}
	if m.appID == "" || m.name == "" || m.installDir == "" {
		return appManifest{}, fmt.Errorf("steam: %s: incomplete AppState (appid/name/installdir)", path)
	}
	return m, nil
}
