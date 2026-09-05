// Command yarm is the YARM (Yet Another ReShade Manager) CLI and TUI.
package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/secato/yarm/internal/buildinfo"
	"github.com/secato/yarm/internal/config"
	"github.com/secato/yarm/internal/paths"
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
			_, closeLog, err := bootstrap(verbose, debug)
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
			dirs, closeLog, err := bootstrap(*verbose, *debug)
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

// bootstrap resolves the application directories, creates them, sets up
// logging and loads (or creates) config.yaml. The returned close function
// must be called to flush and close the log file.
func bootstrap(verbose, debug bool) (paths.Dirs, func() error, error) {
	dirs := paths.Resolve()
	if err := dirs.EnsureAll(); err != nil {
		return paths.Dirs{}, nil, fmt.Errorf("create directories: %w", err)
	}

	closeLog, err := setupLogging(dirs.Data, verbose, debug || envFlag("YARM_DEBUG"))
	if err != nil {
		return paths.Dirs{}, nil, fmt.Errorf("setup logging: %w", err)
	}

	if _, err := config.Load(dirs.Config); err != nil {
		_ = closeLog()
		return paths.Dirs{}, nil, fmt.Errorf("load config: %w", err)
	}

	slog.Debug("directories resolved", "config", dirs.Config, "data", dirs.Data, "cache", dirs.Cache)

	return dirs, closeLog, nil
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
