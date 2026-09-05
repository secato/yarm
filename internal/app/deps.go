package app

import (
	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/config"
)

// Deps bundles what game-focused screens need beyond their own data:
// access to the catalog and cache (through small interfaces, so tests can
// substitute fakes) and the user's install defaults. Threaded down from
// Run() through GamesScreen → GameDetailScreen → WizardScreen, so each
// screen only has to pass it along rather than rebuild its own wiring.
type Deps struct {
	WizardData  WizardDataLoader
	Installer   Installer
	Uninstaller UninstallRunner
	Adopter     AdoptRunner
	CacheStatus CacheStatus // may be nil: badges just show nothing
	Defaults    config.DefaultsConfig

	// Cache backs the cache manager screen (list/delete/total/refresh). It
	// is the concrete type, not an interface: *cache.Cache is already
	// small and tested with httptest in its own package, so there is
	// nothing an app-level fake would buy here.
	Cache *cache.Cache
	// CustomDir is cache/custom, scanned by the custom-content screen.
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
