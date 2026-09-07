// Command yarm is the YARM (Yet Another ReShade Manager) CLI and TUI.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/secato/yarm/internal/app"
	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/buildinfo"
	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/config"
	"github.com/secato/yarm/internal/fetch"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/paths"
	"github.com/secato/yarm/internal/platform"
	"github.com/secato/yarm/internal/platform/manual"
	"github.com/secato/yarm/internal/platform/steam"
	"github.com/secato/yarm/internal/state"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// bootstrap may fail before logging is set up, in which case this
		// falls back to the process-default slog handler (stderr only).
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var verbose, debug, noColor bool

	root := &cobra.Command{
		Use:          "yarm",
		Short:        "Yet Another ReShade Manager",
		SilenceUsage: true,
		Version:      buildinfo.Version,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Checked before bootstrap creates anything: config.yaml not
			// existing yet is what "first run" means here.
			firstRun := isFirstRun(paths.Resolve())

			dirs, cfg, closeLog, err := bootstrap(verbose, debug)
			if err != nil {
				return err
			}
			defer func() { _ = closeLog() }()

			slog.Info("running command", "name", "root", "first_run", firstRun)

			c := newCache(dirs, cfg)
			cl := newCatalogClient(dirs, cfg)
			customDir := filepath.Join(cacheRoot(dirs, cfg), "custom")

			return app.Run(cmd.Context(), app.Options{
				Loader: app.ProviderLoader{
					Providers: buildProviders(cfg),
					StateDir:  dirs.Data,
				},
				Deps: app.Deps{
					WizardData: app.Memoize(app.CatalogWizardData{Client: cl, CustomDir: customDir}),
					Installer: &app.RealInstaller{
						Cache:     c,
						Catalog:   cl,
						CustomDir: customDir,
						StateDir:  dirs.Data,
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
				NoColor:  noColor || envFlag("NO_COLOR"),
				FirstRun: firstRun,
			})
		},
	}

	root.PersistentFlags().BoolVarP(&verbose, "verbose", "V", false, "also stream logs to stderr")
	root.PersistentFlags().BoolVar(&debug, "debug", false, "log at debug level with source locations (also settable via YARM_DEBUG=1)")
	root.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable color output (also settable via NO_COLOR)")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newPathsCmd(&verbose, &debug))
	root.AddCommand(newGamesCmd(&verbose, &debug))
	root.AddCommand(newCatalogCmd(&verbose, &debug))
	root.AddCommand(newCacheCmd(&verbose, &debug))
	root.AddCommand(newFetchCmd(&verbose, &debug))
	root.AddCommand(newInstallCmd(&verbose, &debug))
	root.AddCommand(newUninstallCmd(&verbose, &debug))
	root.AddCommand(newInstallsCmd(&verbose, &debug))

	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), buildinfo.String())
			return nil
		},
	}
}

func newPathsCmd(verbose, debug *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "paths",
		Short: "Print the config, data and cache directories (creating them if needed)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, _, closeLog, err := bootstrap(*verbose, *debug)
			if err != nil {
				return err
			}
			defer func() { _ = closeLog() }()

			slog.Info("running command", "name", "paths")
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "config: %s\n", dirs.Config)
			_, _ = fmt.Fprintf(out, "data:   %s\n", dirs.Data)
			_, _ = fmt.Fprintf(out, "cache:  %s\n", dirs.Cache)
			return nil
		},
	}
}

func newGamesCmd(verbose, debug *bool) *cobra.Command {
	games := &cobra.Command{
		Use:   "games",
		Short: "Inspect discovered games",
	}
	games.AddCommand(newGamesLsCmd(verbose, debug))
	return games
}

// gameOutput and executableOutput are the games ls --json shape.
type gameOutput struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Root     string `json:"root"`
	// NativeBuild marks a game that ships a native ELF/Mach-O binary and no
	// Windows executable. ReShade cannot be installed into one, so the
	// empty Executables list is expected rather than a failed scan.
	NativeBuild bool               `json:"native_build"`
	Executables []executableOutput `json:"executables"`
}

type executableOutput struct {
	Path    string `json:"path"`
	Skipped bool   `json:"skipped"`
	Arch    string `json:"arch"`
	API     string `json:"api"`
}

func newGamesLsCmd(verbose, debug *bool) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List games found via Steam and manually added folders",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cfg, closeLog, err := bootstrap(*verbose, *debug)
			if err != nil {
				return err
			}
			defer func() { _ = closeLog() }()

			slog.Info("running command", "name", "games ls")

			games, err := discoverGames(cmd.Context(), cfg)
			if err != nil {
				return err
			}

			if asJSON {
				return printGamesJSON(cmd.OutOrStdout(), games)
			}
			printGamesText(cmd.OutOrStdout(), games)
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

// buildProviders assembles the discovery providers from config. Shared by
// the CLI and the TUI so both see the same games.
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

func discoverGames(ctx context.Context, cfg config.Config) ([]gameOutput, error) {
	providers := buildProviders(cfg)

	found, err := platform.DiscoverAll(ctx, providers)
	if err != nil {
		slog.Error("provider discovery had errors", "error", err)
	}

	out := make([]gameOutput, 0, len(found))
	for _, g := range found {
		exes, err := game.Scan(g.Root)
		if err != nil {
			slog.Warn("scan failed", "game", g.Name, "root", g.Root, "error", err)
		}

		execs := make([]executableOutput, 0, len(exes))
		for _, e := range exes {
			arch, api := game.Inspect(filepath.Join(g.Root, e.Path))
			execs = append(execs, executableOutput{
				Path:    filepath.ToSlash(e.Path),
				Skipped: e.Skipped,
				Arch:    string(arch),
				API:     string(api),
			})
		}

		out = append(out, gameOutput{
			ID:          g.ID,
			Name:        g.Name,
			Provider:    g.Provider,
			Root:        g.Root,
			NativeBuild: len(execs) == 0 && game.HasNativeBuild(g.Root),
			Executables: execs,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func printGamesJSON(w io.Writer, games []gameOutput) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(games)
}

func printGamesText(w io.Writer, games []gameOutput) {
	if len(games) == 0 {
		_, _ = fmt.Fprintln(w, "no games found")
		return
	}
	for _, g := range games {
		_, _ = fmt.Fprintf(w, "%s  [%s]  %s\n", g.Name, g.ID, g.Root)
		if g.NativeBuild {
			_, _ = fmt.Fprintln(w, "    native build \u2014 ReShade supports Windows executables only")
		}
		for _, e := range g.Executables {
			mark := " "
			if e.Skipped {
				mark = "*"
			}
			_, _ = fmt.Fprintf(w, "  %s %-45s arch=%-8s api=%s\n", mark, e.Path, e.Arch, e.API)
		}
	}
}

// newCatalogCmd builds the hidden `catalog` debug command. It is not part
// of the documented CLI (§1 lists the public subcommands); it exists so
// each implementation step stays demoable before the TUI arrives.
func newCatalogCmd(verbose, debug *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "catalog",
		Short:  "Inspect the effect package, add-on and ReShade version catalogs",
		Hidden: true,
	}
	cmd.AddCommand(newCatalogLsCmd(verbose, debug))
	return cmd
}

func newCatalogLsCmd(verbose, debug *bool) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List ReShade versions, effect packages, add-ons and custom content",
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, cfg, closeLog, err := bootstrap(*verbose, *debug)
			if err != nil {
				return err
			}
			defer func() { _ = closeLog() }()

			slog.Info("running command", "name", "catalog ls")

			cacheDir := cfg.CacheDir
			if cacheDir == "" {
				cacheDir = dirs.Cache
			}

			cl := catalog.New(
				&http.Client{Timeout: 30 * time.Second},
				filepath.Join(cacheDir, "catalog"),
				time.Duration(cfg.CatalogTTLHours)*time.Hour,
				userAgent(),
			)

			out, err := collectCatalog(cmd.Context(), cl, filepath.Join(cacheDir, "custom"))
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}
			printCatalogText(cmd.OutOrStdout(), out)
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

// catalogOutput is the catalog ls --json shape.
type catalogOutput struct {
	Versions []catalog.Version `json:"reshade_versions"`
	Packages []catalog.Package `json:"packages"`
	Addons   []catalog.Addon   `json:"addons"`
	Custom   []catalog.Custom  `json:"custom"`
}

// collectCatalog gathers every catalog source. A failure in one source is
// reported but does not suppress the others: an expired GitHub rate limit
// should not hide the package list.
func collectCatalog(ctx context.Context, cl *catalog.Client, customDir string) (catalogOutput, error) {
	var out catalogOutput

	versions, err := cl.Versions(ctx)
	if err != nil {
		slog.Error("could not load ReShade versions", "error", err)
	}
	out.Versions = versions

	packages, err := cl.Packages(ctx)
	if err != nil {
		slog.Error("could not load effect packages", "error", err)
	}
	out.Packages = packages

	addons, err := cl.Addons(ctx)
	if err != nil {
		slog.Error("could not load add-ons", "error", err)
	}
	out.Addons = addons

	custom, err := catalog.ScanCustom(customDir)
	if err != nil {
		slog.Error("could not scan custom content", "dir", customDir, "error", err)
	}
	out.Custom = custom

	if len(out.Versions) == 0 && len(out.Packages) == 0 && len(out.Addons) == 0 {
		return out, fmt.Errorf("no catalog source could be loaded (offline with an empty cache?)")
	}
	return out, nil
}

func printCatalogText(w io.Writer, c catalogOutput) {
	_, _ = fmt.Fprintf(w, "ReShade versions (%d)\n", len(c.Versions))
	for _, v := range c.Versions {
		marker := ""
		if v.Latest {
			marker = "  (latest)"
		}
		_, _ = fmt.Fprintf(w, "  %s%s\n", v.Version, marker)
	}

	_, _ = fmt.Fprintf(w, "\nEffect packages (%d)\n", len(c.Packages))
	for _, p := range c.Packages {
		flags := ""
		switch {
		case p.Required:
			flags = "  [required]"
		case p.Enabled:
			flags = "  [default]"
		}
		_, _ = fmt.Fprintf(w, "  %-46s %s%s\n", p.ID, p.Name, flags)
	}

	_, _ = fmt.Fprintf(w, "\nAdd-ons (%d)\n", len(c.Addons))
	for _, a := range c.Addons {
		note := string(a.Kind())
		if !a.Installable() {
			note = "manual — see " + a.RepositoryURL
		} else if _, ok := a.SourceFor(game.ArchX86); !ok {
			note += ", x64 only"
		}
		_, _ = fmt.Fprintf(w, "  %-46s %-12s %s\n", a.ID, note, a.Name)
	}

	_, _ = fmt.Fprintf(w, "\nCustom content (%d)\n", len(c.Custom))
	for _, cu := range c.Custom {
		_, _ = fmt.Fprintf(w, "  %-46s %-12s %s\n", cu.ID, cu.Kind, cu.Path)
	}
}

// userAgent identifies YARM to reshade.me and GitHub, as §4.1 requires.
func userAgent() string {
	return fmt.Sprintf("yarm/%s (+https://github.com/secato/yarm)", buildinfo.Version)
}

// newCacheCmd builds the `cache` command group.
func newCacheCmd(verbose, debug *bool) *cobra.Command {
	c := &cobra.Command{
		Use:   "cache",
		Short: "Inspect and clean the download cache",
	}
	c.AddCommand(newCacheLsCmd(verbose, debug), newCacheCleanCmd(verbose, debug))
	return c
}

func newCacheLsCmd(verbose, debug *bool) *cobra.Command {
	var (
		asJSON bool
		sortBy string
		desc   bool
	)

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List cached artifacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, cfg, closeLog, err := bootstrap(*verbose, *debug)
			if err != nil {
				return err
			}
			defer func() { _ = closeLog() }()

			slog.Info("running command", "name", "cache ls")

			c := newCache(dirs, cfg)
			entries, err := c.List(cache.SortBy(sortBy), desc)
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(entries)
			}

			out := cmd.OutOrStdout()
			if len(entries) == 0 {
				_, _ = fmt.Fprintln(out, "cache is empty")
			}
			// Size the id column to the widest id present, so long
			// catalog-derived ids do not shear the columns apart.
			idWidth := 0
			for _, e := range entries {
				if n := len(e.ID); n > idWidth {
					idWidth = n
				}
			}

			var total int64
			for _, e := range entries {
				total += e.Size
				_, _ = fmt.Fprintf(out, "%-12s %-*s %10s  %s\n",
					e.Kind, idWidth, e.ID, humanBytes(e.Size), e.Name)
			}
			if len(entries) > 0 {
				_, _ = fmt.Fprintf(out, "\n%d entries, %s total\n", len(entries), humanBytes(total))
			}

			onDisk, err := c.Total()
			if err == nil {
				_, _ = fmt.Fprintf(out, "cache directory: %s (%s on disk)\n", c.Root, humanBytes(onDisk))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	cmd.Flags().StringVar(&sortBy, "sort", string(cache.SortByName), "sort by: name, size, date, used")
	cmd.Flags().BoolVar(&desc, "desc", false, "reverse the sort order")
	return cmd
}

func newCacheCleanCmd(verbose, debug *bool) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove every cached download",
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, cfg, closeLog, err := bootstrap(*verbose, *debug)
			if err != nil {
				return err
			}
			defer func() { _ = closeLog() }()

			slog.Info("running command", "name", "cache clean")

			c := newCache(dirs, cfg)
			entries, err := c.List(cache.SortByName, false)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "cache is already empty")
				return nil
			}

			if !yes {
				var total int64
				for _, e := range entries {
					total += e.Size
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"%d entries (%s) would be removed. Re-run with --yes to confirm.\n",
					len(entries), humanBytes(total))
				return nil
			}

			removed, err := c.Clean()
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "removed %d cache entries\n", removed)
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "actually delete, rather than reporting what would be deleted")
	return cmd
}

// newFetchCmd builds the hidden `fetch` command group used to smoke-test
// the download and extraction paths before the TUI exists.
func newFetchCmd(verbose, debug *bool) *cobra.Command {
	f := &cobra.Command{
		Use:    "fetch",
		Short:  "Download artifacts into the cache",
		Hidden: true,
	}
	f.AddCommand(
		newFetchReShadeCmd(verbose, debug),
		newFetchD3DCompilerCmd(verbose, debug),
		newFetchPackageCmd(verbose, debug),
	)
	return f
}

func newFetchReShadeCmd(verbose, debug *bool) *cobra.Command {
	var addon bool

	cmd := &cobra.Command{
		Use:   "reshade <version>",
		Short: "Download and extract a ReShade release into the cache",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, cfg, closeLog, err := bootstrap(*verbose, *debug)
			if err != nil {
				return err
			}
			defer func() { _ = closeLog() }()

			slog.Info("running command", "name", "fetch reshade", "version", args[0], "addon", addon)

			c := newCache(dirs, cfg)
			dir, err := c.EnsureReShade(cmd.Context(), args[0], addon, progressPrinter(cmd.ErrOrStderr()))
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", dir)
			return listDir(cmd.OutOrStdout(), dir)
		},
	}

	cmd.Flags().BoolVar(&addon, "addon", false, "fetch the add-on-enabled build")
	return cmd
}

func newFetchD3DCompilerCmd(verbose, debug *bool) *cobra.Command {
	var arch string

	cmd := &cobra.Command{
		Use:   "d3dcompiler",
		Short: "Download and extract d3dcompiler_47.dll into the cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, cfg, closeLog, err := bootstrap(*verbose, *debug)
			if err != nil {
				return err
			}
			defer func() { _ = closeLog() }()

			slog.Info("running command", "name", "fetch d3dcompiler", "arch", arch)

			c := newCache(dirs, cfg)
			dll, err := c.EnsureD3DCompiler(cmd.Context(), game.Arch(arch), progressPrinter(cmd.ErrOrStderr()))
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", dll)
			return nil
		},
	}

	cmd.Flags().StringVar(&arch, "arch", string(game.ArchX64), "architecture: x64 or x86")
	return cmd
}

func newFetchPackageCmd(verbose, debug *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "package <id-or-alias>",
		Short: "Download and normalize an effect package into the cache",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, cfg, closeLog, err := bootstrap(*verbose, *debug)
			if err != nil {
				return err
			}
			defer func() { _ = closeLog() }()

			want := catalog.ResolveAlias(args[0])
			slog.Info("running command", "name", "fetch package", "id", want)

			cacheDir := cfg.CacheDir
			if cacheDir == "" {
				cacheDir = dirs.Cache
			}
			cl := catalog.New(
				&http.Client{Timeout: 30 * time.Second},
				filepath.Join(cacheDir, "catalog"),
				time.Duration(cfg.CatalogTTLHours)*time.Hour,
				userAgent(),
			)

			packages, err := cl.Packages(cmd.Context())
			if err != nil {
				return err
			}

			for _, p := range packages {
				if p.ID != want {
					continue
				}
				dir, err := newCache(dirs, cfg).EnsurePackage(
					cmd.Context(), p, progressPrinter(cmd.ErrOrStderr()))
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", dir)
				return summarizePackage(cmd.OutOrStdout(), dir)
			}
			return fmt.Errorf("no package with id %q (try `yarm catalog ls`)", want)
		},
	}
}

// summarizePackage counts what a normalized package entry holds.
func summarizePackage(w io.Writer, dir string) error {
	counts := map[string]int{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		top, _, ok := strings.Cut(filepath.ToSlash(rel), "/")
		if !ok {
			top = rel
		}
		counts[top]++
		return nil
	})
	if err != nil {
		return err
	}
	for _, k := range []string{"Shaders", "Textures", "package.json"} {
		if n, ok := counts[k]; ok {
			_, _ = fmt.Fprintf(w, "  %-14s %d file(s)\n", k, n)
		}
	}
	return nil
}

// newCache builds a Cache from the resolved directories and config.
func newCache(dirs paths.Dirs, cfg config.Config) *cache.Cache {
	root := cfg.CacheDir
	if root == "" {
		root = dirs.Cache
	}
	return cache.New(root, fetch.New(userAgent()))
}

// progressPrinter renders download progress as a single rewritten line.
func progressPrinter(w io.Writer) fetch.ProgressFunc {
	return func(p fetch.Progress) {
		if pct := p.Percent(); pct >= 0 {
			_, _ = fmt.Fprintf(w, "\r  %s / %s (%.0f%%)   ",
				humanBytes(p.Downloaded), humanBytes(p.Total), pct)
			return
		}
		_, _ = fmt.Fprintf(w, "\r  %s   ", humanBytes(p.Downloaded))
	}
}

// listDir prints the files in a cache entry directory.
func listDir(w io.Writer, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(w, "  %-24s %10s\n", e.Name(), humanBytes(info.Size()))
	}
	return nil
}

// humanBytes formats a byte count for display.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// newInstallCmd builds the hidden `install` command, which drives the
// engine end to end before the TUI exists.
func newInstallCmd(verbose, debug *bool) *cobra.Command {
	var (
		gameID    string
		exeRel    string
		version   string
		addon     bool
		dllName   string
		packages  []string
		addons    []string
		custom    []string
		overwrite bool
		noBackup  bool
		dryRun    bool
	)

	cmd := &cobra.Command{
		Use:    "install",
		Short:  "Install ReShade into a game executable",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, cfg, closeLog, err := bootstrap(*verbose, *debug)
			if err != nil {
				return err
			}
			defer func() { _ = closeLog() }()

			slog.Info("running command", "name", "install", "game", gameID, "exe", exeRel)

			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			g, exe, err := findGameExe(ctx, cfg, gameID, exeRel)
			if err != nil {
				return err
			}

			flavor := install.FlavorNormal
			if addon {
				flavor = install.FlavorAddon
			}
			if dllName == "" {
				dllName = exe.API.RecommendedDLL()
				if dllName == "" {
					return fmt.Errorf("could not guess a DLL for api %q; pass --dll", exe.API)
				}
				_, _ = fmt.Fprintf(out, "using --dll %s (guessed from api=%s)\n", dllName, exe.API)
			}

			req := install.Request{
				Game:      g,
				Exe:       exe,
				Version:   version,
				Flavor:    flavor,
				DLLName:   dllName,
				Packages:  packages,
				Addons:    addons,
				Custom:    custom,
				Overwrite: overwrite,
				NoBackup:  noBackup,
				TargetOS:  artifacts.CurrentTargetOS(),
			}

			art, err := resolveArtifacts(ctx, dirs, cfg, req, cmd.ErrOrStderr())
			if err != nil {
				return err
			}

			reg, err := state.Load(dirs.Data)
			if err != nil {
				return err
			}

			plan, err := install.Planner{Registry: reg}.Plan(req, art)
			if err != nil {
				return err
			}
			printPlan(out, plan)

			if dryRun {
				_, _ = fmt.Fprintln(out, "\ndry run: nothing was changed")
				return nil
			}

			res, err := install.NewExecutor(dirs.Data).Run(ctx, plan, func(ev install.Event) {
				if ev.Kind == install.StepCopy && ev.Total > 0 {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "\r  [%d/%d] %-50s", ev.Done, ev.Total, ev.Description)
				}
			})
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.ErrOrStderr())

			_, _ = fmt.Fprintf(out, "\ninstalled %d file(s) into %s\n", len(res.Written), g.Root)
			for _, w := range res.Written {
				_, _ = fmt.Fprintf(out, "  + %s\n", w)
			}
			for _, s := range res.Skipped {
				_, _ = fmt.Fprintf(out, "  = %s (unchanged)\n", s)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&gameID, "game", "", "game id, e.g. steam:700110 (required)")
	cmd.Flags().StringVar(&exeRel, "exe", "", "executable path relative to the game root (required)")
	cmd.Flags().StringVar(&version, "version", "", "ReShade version, e.g. 6.8.0 (required)")
	cmd.Flags().BoolVar(&addon, "addon", false, "use the add-on-enabled ReShade build")
	cmd.Flags().StringVar(&dllName, "dll", "", "proxy DLL name; guessed from the executable's API when omitted")
	cmd.Flags().StringSliceVar(&packages, "packages", nil, "effect package ids or aliases")
	cmd.Flags().StringSliceVar(&addons, "addons", nil, "add-on ids")
	cmd.Flags().StringSliceVar(&custom, "custom", nil, "custom content ids")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace files yarm does not own, backing them up first")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false, "with --overwrite, discard the replaced files instead of saving them as .yarm-bak")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the plan without changing anything")
	_ = cmd.MarkFlagRequired("game")
	_ = cmd.MarkFlagRequired("exe")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

// newUninstallCmd builds the hidden `uninstall` command.
func newUninstallCmd(verbose, debug *bool) *cobra.Command {
	var (
		gameID     string
		exeRel     string
		removeUser bool
	)

	cmd := &cobra.Command{
		Use:    "uninstall",
		Short:  "Remove a ReShade install recorded in the manifest",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, _, closeLog, err := bootstrap(*verbose, *debug)
			if err != nil {
				return err
			}
			defer func() { _ = closeLog() }()

			slog.Info("running command", "name", "uninstall", "game", gameID, "exe", exeRel)

			res, err := install.NewUninstaller(dirs.Data).Run(install.UninstallRequest{
				GameID:         gameID,
				Exe:            filepath.ToSlash(exeRel),
				RemoveUserData: removeUser,
			})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "removed %d file(s)\n", len(res.Removed))
			for _, r := range res.Removed {
				_, _ = fmt.Fprintf(out, "  - %s\n", r)
			}
			for _, k := range res.Kept {
				_, _ = fmt.Fprintf(out, "  ! %s (modified since install, kept)\n", k)
			}
			for _, r := range res.Restored {
				_, _ = fmt.Fprintf(out, "  ~ %s (restored from backup)\n", r)
			}
			for _, m := range res.Missing {
				_, _ = fmt.Fprintf(out, "  ? %s (already gone)\n", m)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&gameID, "game", "", "game id (required)")
	cmd.Flags().StringVar(&exeRel, "exe", "", "executable path relative to the game root (required)")
	cmd.Flags().BoolVar(&removeUser, "remove-user-data", false, "also delete ReShadePreset.ini and ReShade.log")
	_ = cmd.MarkFlagRequired("game")
	_ = cmd.MarkFlagRequired("exe")
	return cmd
}

// newInstallsCmd lists what the manifest records.
func newInstallsCmd(verbose, debug *bool) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "installs",
		Short: "List ReShade installations yarm has made",
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, _, closeLog, err := bootstrap(*verbose, *debug)
			if err != nil {
				return err
			}
			defer func() { _ = closeLog() }()

			reg, err := state.Load(dirs.Data)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(reg)
			}

			all := reg.Installs()
			if len(all) == 0 {
				_, _ = fmt.Fprintln(out, "no installs recorded")
				return nil
			}
			for _, gi := range all {
				_, _ = fmt.Fprintf(out, "%s  [%s]\n", gi.Game.Name, gi.GameID)
				_, _ = fmt.Fprintf(out, "  exe:     %s\n", gi.Install.Exe)
				_, _ = fmt.Fprintf(out, "  reshade: %s (%s) as %s\n",
					gi.Install.ReShade.Version, gi.Install.ReShade.Flavor, gi.Install.ReShade.DLL)
				_, _ = fmt.Fprintf(out, "  files:   %d\n", len(gi.Install.Files))
				if len(gi.Install.Packages) > 0 {
					_, _ = fmt.Fprintf(out, "  packages: %s\n", strings.Join(gi.Install.Packages, ", "))
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

// printPlan renders a plan for review.
func printPlan(w io.Writer, plan install.Plan) {
	_, _ = fmt.Fprintf(w, "\nplan for %s (%s)\n", plan.Request.Exe.Path, plan.Request.Game.Name)
	if plan.Upgrade {
		_, _ = fmt.Fprintln(w, "  (upgrading an existing install)")
	}
	for _, s := range plan.Steps {
		_, _ = fmt.Fprintf(w, "  step: %s\n", s.Description)
	}
	_, _ = fmt.Fprintf(w, "  %d file(s), %s\n", plan.WriteCount(), humanBytes(plan.TotalBytes()))
	for _, rel := range plan.Removed {
		_, _ = fmt.Fprintf(w, "  - %s\n", rel)
	}
	for _, warn := range plan.Warnings {
		_, _ = fmt.Fprintf(w, "  ! %s\n", warn)
	}
}

// findGameExe locates a discovered game and one of its executables.
func findGameExe(ctx context.Context, cfg config.Config, gameID, exeRel string) (game.Game, game.Executable, error) {
	games, err := discoverGames(ctx, cfg)
	if err != nil {
		return game.Game{}, game.Executable{}, err
	}

	for _, g := range games {
		if g.ID != gameID {
			continue
		}
		want := filepath.ToSlash(exeRel)
		for _, e := range g.Executables {
			if e.Path == want {
				return game.Game{ID: g.ID, Name: g.Name, Provider: g.Provider, Root: g.Root},
					game.Executable{
						Path: filepath.FromSlash(e.Path),
						Arch: game.Arch(e.Arch),
						API:  game.API(e.API),
					}, nil
			}
		}
		return game.Game{}, game.Executable{}, fmt.Errorf(
			"game %s has no executable %q (see `yarm games ls`)", gameID, exeRel)
	}
	return game.Game{}, game.Executable{}, fmt.Errorf("no game with id %q (see `yarm games ls`)", gameID)
}

// resolveArtifacts downloads and locates everything the request needs.
func resolveArtifacts(ctx context.Context, dirs paths.Dirs, cfg config.Config, req install.Request, progressOut io.Writer) (install.Artifacts, error) {
	c := newCache(dirs, cfg)
	art := install.Artifacts{
		Packages: map[string]string{},
		Addons:   map[string]string{},
		Custom:   map[string]string{},
	}

	progress := progressPrinter(progressOut)

	reshadeDir, err := c.EnsureReShade(ctx, req.Version, req.Flavor.Addon(), progress)
	if err != nil {
		return install.Artifacts{}, fmt.Errorf("reshade %s: %w", req.Version, err)
	}
	art.ReShadeDir = reshadeDir

	if artifacts.NeedsD3DCompiler(req.TargetOS) {
		dll, err := c.EnsureD3DCompiler(ctx, req.Exe.Arch, progress)
		if err != nil {
			return install.Artifacts{}, fmt.Errorf("d3dcompiler: %w", err)
		}
		art.D3DCompiler = dll
	}

	if len(req.Packages) > 0 || len(req.Addons) > 0 {
		cl := newCatalogClient(dirs, cfg)

		if len(req.Packages) > 0 {
			pkgs, err := cl.Packages(ctx)
			if err != nil {
				return install.Artifacts{}, err
			}
			byID := make(map[string]catalog.Package, len(pkgs))
			for _, p := range pkgs {
				byID[p.ID] = p
			}
			for i, id := range req.Packages {
				resolved := catalog.ResolveAlias(id)
				p, ok := byID[resolved]
				if !ok {
					return install.Artifacts{}, fmt.Errorf("unknown package %q", id)
				}
				dir, err := c.EnsurePackage(ctx, p, progress)
				if err != nil {
					return install.Artifacts{}, fmt.Errorf("package %s: %w", resolved, err)
				}
				art.Packages[resolved] = dir
				req.Packages[i] = resolved
			}
		}

		if len(req.Addons) > 0 {
			list, err := cl.Addons(ctx)
			if err != nil {
				return install.Artifacts{}, err
			}
			byID := make(map[string]catalog.Addon, len(list))
			for _, a := range list {
				byID[a.ID] = a
			}
			for _, id := range req.Addons {
				a, ok := byID[id]
				if !ok {
					return install.Artifacts{}, fmt.Errorf("unknown add-on %q", id)
				}
				if !a.Installable() {
					return install.Artifacts{}, fmt.Errorf(
						"add-on %q is manual only; see %s", id, a.RepositoryURL)
				}
				dir, err := c.EnsureAddon(ctx, a, req.Exe.Arch, progress)
				if err != nil {
					return install.Artifacts{}, fmt.Errorf("addon %s: %w", id, err)
				}
				art.Addons[id] = dir
			}
		}
	}

	for _, id := range req.Custom {
		found, err := catalog.ScanCustom(filepath.Join(cacheRoot(dirs, cfg), "custom"))
		if err != nil {
			return install.Artifacts{}, err
		}
		var matched bool
		for _, cu := range found {
			if cu.ID == id || cu.Name == id {
				art.Custom[id] = cu.Path
				matched = true
				break
			}
		}
		if !matched {
			return install.Artifacts{}, fmt.Errorf("unknown custom content %q", id)
		}
	}

	return art, nil
}

// newCatalogClient builds a catalog client from the resolved dirs.
func newCatalogClient(dirs paths.Dirs, cfg config.Config) *catalog.Client {
	return catalog.New(
		&http.Client{Timeout: 30 * time.Second},
		filepath.Join(cacheRoot(dirs, cfg), "catalog"),
		time.Duration(cfg.CatalogTTLHours)*time.Hour,
		userAgent(),
	)
}

// cacheRoot resolves the cache directory, honoring the config override.
func cacheRoot(dirs paths.Dirs, cfg config.Config) string {
	if cfg.CacheDir != "" {
		return cfg.CacheDir
	}
	return dirs.Cache
}

// bootstrap resolves the application directories, creates them, sets up
// logging and loads (or creates) config.yaml. The returned close function
// must be called to flush and close the log file.
// isFirstRun reports whether config.yaml does not exist yet under dirs —
// the signal that this is the first time yarm has run for this user, used
// only to decide whether to show the welcome banner. Must be checked
// before bootstrap runs: both dirs.EnsureAll and config.Load create things
// on disk as a side effect.
func isFirstRun(dirs paths.Dirs) bool {
	_, err := os.Stat(config.Path(dirs.Config))
	return os.IsNotExist(err)
}

func bootstrap(verbose, debug bool) (paths.Dirs, config.Config, func() error, error) {
	dirs := paths.Resolve()
	if err := dirs.EnsureAll(); err != nil {
		return paths.Dirs{}, config.Config{}, nil, fmt.Errorf("create directories: %w", err)
	}

	closeLog, err := setupLogging(dirs.Data, verbose, debug || envFlag("YARM_DEBUG"))
	if err != nil {
		return paths.Dirs{}, config.Config{}, nil, fmt.Errorf("setup logging: %w", err)
	}

	cfg, err := config.Load(dirs.Config)
	if err != nil {
		_ = closeLog()
		return paths.Dirs{}, config.Config{}, nil, fmt.Errorf("load config: %w", err)
	}

	slog.Debug("directories resolved", "config", dirs.Config, "data", dirs.Data, "cache", dirs.Cache)

	return dirs, cfg, closeLog, nil
}

// setupLogging installs the default slog logger, appending to yarm.log in
// stateDir. debug raises the level from Info to Debug and adds source
// locations to each record; verbose additionally mirrors everything to
// stderr. Errors returned by commands are always logged (see main), so the
// log file captures failures even when neither flag is set.
func setupLogging(stateDir string, verbose, debug bool) (func() error, error) {
	logPath := filepath.Join(stateDir, "yarm.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	var w io.Writer = f
	if verbose {
		w = io.MultiWriter(f, os.Stderr)
	}

	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level:     level,
		AddSource: debug,
	})
	slog.SetDefault(slog.New(handler))

	return f.Close, nil
}

// envFlag reports whether the named environment variable is set to a
// truthy value ("1" or "true", case-insensitive).
func envFlag(name string) bool {
	v := strings.ToLower(os.Getenv(name))
	return v == "1" || v == "true"
}
