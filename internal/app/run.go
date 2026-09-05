package app

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

// Options configure a TUI run.
type Options struct {
	// Loader supplies the games list.
	Loader GamesLoader
	// Deps wires the install wizard, progress screen and uninstall
	// confirm to the real cache, catalog and install engine.
	Deps Deps
	// NoColor forces monochrome output, for terminals or pipelines that
	// cannot render color.
	NoColor bool
}

// Run starts the TUI and blocks until the user quits.
func Run(ctx context.Context, opts Options) error {
	m := New(NewGamesScreen(opts.Loader, opts.Deps))

	programOpts := []tea.ProgramOption{tea.WithContext(ctx)}
	if opts.NoColor {
		programOpts = append(programOpts, tea.WithColorProfile(colorprofile.NoTTY))
	}

	_, err := tea.NewProgram(m, programOpts...).Run()
	return err
}
