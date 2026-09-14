// Package epic implements a platform.Provider that discovers games from
// the Epic Games Store. On Windows that is the launcher's per-game
// manifest files; on Linux, where the store has no client, the games
// are managed by launchers, so the sources are Heroic and Lutris.
package epic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/secato/yarm/internal/fsutil"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/platform/sources"
	"github.com/secato/yarm/internal/platform/sources/heroic"
	"github.com/secato/yarm/internal/platform/sources/lutris"
)

// ProviderName identifies this provider in game.Game.Provider and IDs.
const ProviderName = sources.StoreEpic

// maxItemBytes bounds one manifest file. They are written by the Epic
// launcher in a directory yarm does not own and are a few kilobytes.
const maxItemBytes = 1 << 20

// Provider discovers Epic Games Store games.
type Provider struct {
	// manifestDirs are the Epic launcher's manifest directories.
	manifestDirs []string
	// heroicDirs are Heroic config directories to read.
	heroicDirs []string
	// lutrisDBs are Lutris pga.db paths to read.
	lutrisDBs []string
}

// New returns a Provider using this OS's defaults.
func New() *Provider {
	return NewWith(defaultManifestDirs(), heroic.DefaultConfigDirs(), lutris.DefaultDBPaths())
}

// NewWith reads exactly the sources given. Exported for tests, which
// point it at fixture directories; production code should use New.
func NewWith(manifestDirs, heroicDirs, lutrisDBs []string) *Provider {
	return &Provider{
		manifestDirs: manifestDirs,
		heroicDirs:   heroicDirs,
		lutrisDBs:    lutrisDBs,
	}
}

// Name implements platform.Provider.
func (p *Provider) Name() string { return ProviderName }

// Discover implements platform.Provider. A source that is not present
// is the normal case and yields nothing; one that is present but
// unreadable is logged and skipped, the same deal the other providers
// give their sources.
func (p *Provider) Discover(_ context.Context) ([]game.Game, error) {
	// Order is deliberate: the launcher's own manifests are
	// authoritative, and Dedupe keeps the first entry per game id.
	var entries []sources.Entry

	for _, dir := range p.manifestDirs {
		found, err := scanManifests(dir)
		entries = append(entries, found...)
		if err != nil {
			slog.Debug("epic: manifests unreadable", "dir", dir, "error", err)
		}
	}
	for _, dir := range p.heroicDirs {
		found, err := heroic.Read(dir)
		entries = append(entries, sources.OfStore(found, ProviderName)...)
		if err != nil {
			slog.Debug("epic: heroic unreadable", "dir", dir, "error", err)
		}
	}
	for _, db := range p.lutrisDBs {
		found, err := lutris.Games(db)
		entries = append(entries, sources.OfStore(found, ProviderName)...)
		if err != nil {
			slog.Debug("epic: lutris unreadable", "db", db, "error", err)
		}
	}

	return sources.Games(entries), nil
}

// scanManifests reads every .item manifest in one of the launcher's
// manifest directories. A missing directory yields nothing without an
// error — Epic not installed is the normal case — while a manifest
// that exists but cannot be parsed is an error, so corruption surfaces
// instead of silently hiding a game.
func scanManifests(dir string) ([]sources.Entry, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.item"))
	if err != nil {
		return nil, err
	}

	var (
		entries []sources.Entry
		errs    []error
	)
	for _, path := range matches {
		e, err := parseItem(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if e != nil {
			entries = append(entries, *e)
		}
	}
	return entries, errors.Join(errs...)
}

// parseItem reads one manifest, returning nil when it describes
// something that is not an installed game.
func parseItem(path string) (*sources.Entry, error) {
	data, err := fsutil.ReadFileMax(path, maxItemBytes)
	if err != nil {
		return nil, fmt.Errorf("epic: %w", err)
	}

	var item epicItem
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, fmt.Errorf("epic: %s: %w", path, err)
	}

	// An incomplete install is not a game yet; a manifest without
	// bIsApplication is one of the launcher's own tools. The flags are
	// pointers so an older manifest that omits them entirely is not
	// read as "false".
	if item.Incomplete != nil && *item.Incomplete {
		return nil, nil
	}
	if item.Application != nil && !*item.Application {
		return nil, nil
	}
	// DLC manifests carry the base game's AppName here; the base
	// game's own manifest repeats its own.
	if item.MainGameAppName != "" && item.MainGameAppName != item.AppName {
		return nil, nil
	}
	if item.AppName == "" || item.DisplayName == "" || item.InstallLocation == "" {
		return nil, fmt.Errorf("epic: %s: manifest without app name, title or install location", path)
	}

	return &sources.Entry{
		Store:   ProviderName,
		StoreID: item.AppName,
		Title:   item.DisplayName,
		Root:    item.InstallLocation,
	}, nil
}

// epicItem is the subset of the launcher's manifest that discovery
// needs, with the launcher's own field names.
type epicItem struct {
	DisplayName     string `json:"DisplayName"`
	AppName         string `json:"AppName"`
	InstallLocation string `json:"InstallLocation"`
	Incomplete      *bool  `json:"bIsIncompleteInstall"`
	Application     *bool  `json:"bIsApplication"`
	MainGameAppName string `json:"MainGameAppName"`
}
