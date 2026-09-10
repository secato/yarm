package app

import (
	"context"

	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/config"
)

// CatalogRefresher force-refreshes the on-disk catalogs, bypassing their
// cache. Implemented by *catalog.Client; a nil refresher means catalogs
// only ever load, never refresh.
type CatalogRefresher interface {
	RefreshCatalog(ctx context.Context) error
}

// Deps bundles what game-focused screens need beyond their own data:
// access to the catalog and cache (through small interfaces, so tests can
// substitute fakes) and the user's install defaults. Threaded down from
// Run() through GamesScreen and the folder picker to the wizard, so each
// screen only has to pass it along rather than rebuild its own wiring.
type Deps struct {
	WizardData  WizardDataLoader
	Installer   Installer
	Uninstaller UninstallRunner
	Adopter     AdoptRunner
	// CatalogRefresh force-refreshes the on-disk catalogs for the rescan
	// key (nil in tests that never rescan).
	CatalogRefresh CatalogRefresher
	CacheStatus    CacheStatus // may be nil: badges just show nothing
	Defaults       config.DefaultsConfig

	// Cache backs the resources screen (browse/download/delete/refresh)
	// and the cache-status badges elsewhere. It is the concrete type, not
	// an interface: *cache.Cache is already small and tested with
	// httptest in its own package, so there is nothing an app-level fake
	// would buy here.
	Cache *cache.Cache
	// StateDir holds installs.json — read directly (not through Installer
	// etc.) by the resources screen, to cross-reference a cached artifact
	// against every recorded install without needing its own interface.
	StateDir string
	// CustomDir is <data>/custom, scanned by the custom-content and
	// resources screens.
	CustomDir string
	// Config is the configuration as loaded at startup, and ConfigDir is
	// where config.yaml lives. The settings screen edits a copy of Config
	// and writes it back to ConfigDir; every change takes effect on the
	// next launch, not the running session (a manual game added there
	// would need the provider list itself rebuilt to be found by a scan,
	// and the cache directory is read once at startup) — the settings
	// screen says so.
	Config    config.Config
	ConfigDir string
}
