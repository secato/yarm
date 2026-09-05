// Command yarm is the YARM (Yet Another ReShade Manager) CLI and TUI.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/secato/yarm/internal/buildinfo"
	"github.com/secato/yarm/internal/config"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/paths"
	"github.com/secato/yarm/internal/platform"
	"github.com/secato/yarm/internal/platform/manual"
	"github.com/secato/yarm/internal/platform/steam"
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
	var verbose, debug bool

	root := &cobra.Command{
		Use:          "yarm",
		Short:        "Yet Another ReShade Manager",
		SilenceUsage: true,
		Version:      buildinfo.Version,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, closeLog, err := bootstrap(verbose, debug)
			if err != nil {
				return err
			}
			defer func() { _ = closeLog() }()

			slog.Info("running command", "name", "root")
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "TUI not ready")
			return nil
		},
	}

	root.PersistentFlags().BoolVarP(&verbose, "verbose", "V", false, "also stream logs to stderr")
	root.PersistentFlags().BoolVar(&debug, "debug", false, "log at debug level with source locations (also settable via YARM_DEBUG=1)")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newPathsCmd(&verbose, &debug))
	root.AddCommand(newGamesCmd(&verbose, &debug))

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

func discoverGames(ctx context.Context, cfg config.Config) ([]gameOutput, error) {
	var providers []platform.Provider
	if cfg.Steam.Enabled {
		providers = append(providers, steam.New(cfg.Steam.ExtraLibraryPaths))
	}
	manualEntries := make([]manual.Entry, len(cfg.ManualGames))
	for i, g := range cfg.ManualGames {
		manualEntries[i] = manual.Entry{Name: g.Name, Path: g.Path}
	}
	providers = append(providers, manual.New(manualEntries))

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

// bootstrap resolves the application directories, creates them, sets up
// logging and loads (or creates) config.yaml. The returned close function
// must be called to flush and close the log file.
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
