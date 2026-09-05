package app

import "github.com/secato/yarm/internal/config"

// Deps bundles what game-focused screens need beyond their own data:
// access to the catalog and cache (through small interfaces, so tests can
// substitute fakes) and the user's install defaults. Threaded down from
// Run() through GamesScreen → GameDetailScreen → WizardScreen, so each
// screen only has to pass it along rather than rebuild its own wiring.
type Deps struct {
	WizardData  WizardDataLoader
	Installer   Installer
	Uninstaller UninstallRunner
	CacheStatus CacheStatus // may be nil: badges just show nothing
	Defaults    config.DefaultsConfig
}
