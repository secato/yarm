// Command yarm is the YARM (Yet Another ReShade Manager) terminal UI.
//
// There are no subcommands: running yarm opens the interface, and
// everything it can do is done from there. The handful of flags below
// exist because a binary that ignores --version or --help is a nuisance
// to package and to file bugs against — not because there is a second
// way to drive it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/secato/yarm/internal/app"
	"github.com/secato/yarm/internal/buildinfo"
	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/config"
	"github.com/secato/yarm/internal/fetch"
	"github.com/secato/yarm/internal/paths"
	"github.com/secato/yarm/internal/platform"
	"github.com/secato/yarm/internal/platform/manual"
	"github.com/secato/yarm/internal/platform/steam"
)

func main() {
	err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
	if err == nil {
		return
	}
	_, _ = fmt.Fprintln(os.Stderr, "yarm:", err)

	// A usage error happens before there is a log file to write to, and
	// slog's default handler writes to stderr — which would print the
	// same line twice. Exit 2 is the conventional code for being called
	// wrong, as opposed to failing while running.
	var ue usageError
	if errors.As(err, &ue) {
		os.Exit(2)
	}

	// Otherwise both, deliberately: by the time this runs the TUI has
	// given the terminal back, and the log file is no use to someone
	// watching a window that just closed — but it is where the detail
	// lives for whoever comes back to it afterwards.
	slog.Error("yarm exited with an error", "error", err)
	os.Exit(1)
}

// usageError marks a problem with how yarm was invoked, as opposed to a
// failure while it was running. The two are reported differently.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// options is everything the command line can say. Deliberately small.
type options struct {
	debug       bool
	noColor     bool
	showVersion bool
}

// parseOptions reads the flags, applying the environment fallbacks that
// each one has. Split out from run so the argument handling can be tested
// without starting a terminal program.
func parseOptions(args []string, out io.Writer) (options, error) {
	var o options

	fs := flag.NewFlagSet("yarm", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.BoolVar(&o.debug, "debug", false, "log at debug level, with source locations (or YARM_DEBUG=1)")
	fs.BoolVar(&o.noColor, "no-color", false, "render without color (or NO_COLOR=1)")
	fs.BoolVar(&o.showVersion, "version", false, "print version information and exit")
	fs.Usage = func() {
		_, _ = fmt.Fprint(out, "yarm — Yet Another ReShade Manager\n\n"+
			"Install ReShade, shaders and add-ons per game, from a terminal interface.\n"+
			"Run yarm with no arguments to open it.\n\nUsage:\n  yarm [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return options{}, err
		}
		return options{}, usageError{err}
	}
	// A leftover argument is almost always someone reaching for a
	// subcommand that no longer exists, so say that rather than "flag
	// provided but not defined".
	if fs.NArg() > 0 {
		return options{}, usageError{fmt.Errorf(
			"unexpected argument %q — yarm takes no commands; run it with no arguments to open the interface",
			fs.Arg(0))}
	}

	o.debug = o.debug || envFlag("YARM_DEBUG")
	o.noColor = o.noColor || envFlag("NO_COLOR")
	return o, nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	opts, err := parseOptions(args, stderr)
	switch {
	case errors.Is(err, flag.ErrHelp):
		return nil // -h already printed the usage
	case err != nil:
		return err
	}

	if opts.showVersion {
		_, _ = fmt.Fprintln(stdout, buildinfo.String())
		return nil
	}

	// Checked before bootstrap creates anything: config.yaml not existing
	// yet is what "first run" means here.
	firstRun := isFirstRun(paths.Resolve())

	dirs, cfg, closeLog, err := bootstrap(opts.debug)
	if err != nil {
		return err
	}
	defer func() { _ = closeLog() }()

	slog.Info("starting", "version", buildinfo.Version, "first_run", firstRun)

	c := newCache(dirs, cfg)
	if err := c.CleanPartials(); err != nil {
		slog.Warn("could not clear interrupted downloads", "error", err)
	}
	cl := newCatalogClient(dirs, cfg)
	customDir := dirs.Custom()

	// One memoized catalog load shared by the wizard and the install
	// pipeline, so an install resolves exactly what the wizard showed.
	wizardData := app.Memoize(app.CatalogWizardData{Client: cl, CustomDir: customDir})

	return app.Run(ctx, app.Options{
		Loader: app.ProviderLoader{
			Providers: buildProviders(cfg),
			StateDir:  dirs.Data,
		},
		Deps: app.Deps{
			WizardData:     wizardData,
			CatalogRefresh: cl,
			Installer: &app.RealInstaller{
				Cache:      c,
				Catalog:    cl,
				WizardData: wizardData,
				CustomDir:  customDir,
				StateDir:   dirs.Data,
			},
			Uninstaller: app.RealUninstaller{StateDir: dirs.Data},
			Adopter:     app.RealAdopter{StateDir: dirs.Data},
			CacheStatus: c,
			Defaults:    cfg.Defaults,
			Cache:       c,
			StateDir:    dirs.Data,
			CustomDir:   customDir,
			Config:      cfg,
			ConfigDir:   dirs.Config,
		},
		NoColor:  opts.noColor,
		FirstRun: firstRun,
	})
}

// buildProviders assembles the discovery providers from config. The one
// place providers are registered: adding a launcher means implementing
// platform.Provider and appending it here.
func buildProviders(cfg config.Config) []platform.Provider {
	var providers []platform.Provider
	if cfg.Steam.Enabled {
		providers = append(providers, steam.New(cfg.Steam.ExtraLibraryPaths))
	}
	manualEntries := make([]manual.Entry, len(cfg.ManualGames))
	for i, g := range cfg.ManualGames {
		manualEntries[i] = manual.Entry{Name: g.Name, Path: g.Path}
	}
	return append(providers, manual.New(manualEntries))
}

// newCache builds a Cache from the resolved directories and config.
func newCache(dirs paths.Dirs, cfg config.Config) *cache.Cache {
	return cache.New(cacheRoot(dirs, cfg), fetch.New(userAgent()))
}

func newCatalogClient(dirs paths.Dirs, cfg config.Config) *catalog.Client {
	cl := catalog.New(
		&http.Client{Timeout: 30 * time.Second, Transport: fetch.DefaultTransport()},
		filepath.Join(cacheRoot(dirs, cfg), "catalog"),
		time.Duration(cfg.RenodxTTLHours)*time.Hour,
		userAgent(),
	)
	cl.RenoDXTTL = time.Duration(cfg.RenodxTTLHours) * time.Hour
	return cl
}

// cacheRoot resolves the cache directory, honoring the config override.
func cacheRoot(dirs paths.Dirs, cfg config.Config) string {
	if cfg.CacheDir != "" {
		return cfg.CacheDir
	}
	return dirs.Cache
}

// userAgent identifies yarm to the hosts it fetches from. These are other
// people's endpoints being polled by a tool their maintainers did not
// write, so the request says plainly what it is and where to complain.
func userAgent() string {
	return fmt.Sprintf("yarm/%s (+https://github.com/secato/yarm)", buildinfo.Version)
}

// isFirstRun reports whether config.yaml does not exist yet under dirs —
// the signal that this is the first time yarm has run for this user, used
// only to decide whether to show the welcome banner. Must be checked
// before bootstrap runs: both dirs.EnsureAll and config.Load create things
// on disk as a side effect.
func isFirstRun(dirs paths.Dirs) bool {
	_, err := os.Stat(config.Path(dirs.Config))
	return os.IsNotExist(err)
}

// bootstrap resolves the application directories, creates them, sets up
// logging and loads (or creates) config.yaml. The returned close function
// must be called to flush and close the log file.
func bootstrap(debug bool) (paths.Dirs, config.Config, func() error, error) {
	dirs := paths.Resolve()
	if err := dirs.EnsureAll(); err != nil {
		return paths.Dirs{}, config.Config{}, nil, fmt.Errorf("create directories: %w", err)
	}

	closeLog, err := setupLogging(dirs.Data, debug)
	if err != nil {
		return paths.Dirs{}, config.Config{}, nil, fmt.Errorf("setup logging: %w", err)
	}

	cfg, err := config.Load(dirs.Config)
	if err != nil {
		_ = closeLog()
		return paths.Dirs{}, config.Config{}, nil, fmt.Errorf("load config: %w", err)
	}

	// Logged at info, not debug: this is the answer to "where does yarm
	// keep my things", and with no `yarm paths` command to ask, the log
	// is where someone helping with a bug report will look for it.
	slog.Info("directories resolved",
		"config", dirs.Config, "data", dirs.Data, "cache", dirs.Cache, "custom", dirs.Custom())

	// Custom content moved out of the cache and into the data directory.
	// A failure here is logged rather than fatal: the content is still
	// wherever it was, and refusing to start over a directory yarm only
	// reads would be worse than starting without it.
	oldCustom := filepath.Join(cacheRoot(dirs, cfg), paths.DirCustom)
	switch moved, err := paths.MigrateCustom(oldCustom, dirs.Custom()); {
	case err != nil:
		slog.Warn("could not move custom content out of the cache",
			"from", oldCustom, "to", dirs.Custom(), "error", err)
	case moved:
		slog.Info("moved custom content out of the cache",
			"from", oldCustom, "to", dirs.Custom())
	}

	return dirs, cfg, closeLog, nil
}

// setupLogging installs the default slog logger, appending to yarm.log in
// stateDir. debug raises the level from Info to Debug and adds source
// locations to each record.
//
// The log never goes to stderr: yarm runs in the alternate screen, and
// anything written to the terminal underneath it corrupts the display.
// Failures are also printed to stderr by main, after the program has
// given the terminal back.
func setupLogging(stateDir string, debug bool) (func() error, error) {
	logPath := filepath.Join(stateDir, "yarm.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
		Level:     level,
		AddSource: debug,
	})))

	return f.Close, nil
}

// envFlag reports whether the named environment variable is set to a
// truthy value ("1" or "true", case-insensitive).
func envFlag(name string) bool {
	v := strings.ToLower(os.Getenv(name))
	return v == "1" || v == "true"
}
