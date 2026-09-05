package app

import (
	"context"
	"errors"

	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/state"
)

// fakeLoader supplies a fixed games list, so screen tests never touch a
// real Steam library and always render the same thing.
type fakeLoader struct {
	entries []GameEntry
	err     error
}

func (f fakeLoader) LoadGames(context.Context) ([]GameEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

// errLoader always fails, for the error-path tests.
var errLoader = fakeLoader{err: errors.New("steam library is unreadable")}

// sampleEntries is the fixture games list: one game with an install, one
// without, one native build, and one whose only executables are skipped.
func sampleEntries() []GameEntry {
	installed := state.Install{
		Exe: "Game/eldenring.exe",
		ReShade: state.ReShadeInfo{
			Version: "6.8.0", Flavor: "addon", Arch: "x64", API: "d3d12", DLL: "dxgi.dll",
		},
	}

	return []GameEntry{
		{
			Game: game.Game{
				ID: "steam:870780", Name: "Control Ultimate Edition",
				Provider: "steam", Root: "/games/Control",
			},
			Exes: []Executable{
				{Executable: game.Executable{Path: "Control.exe", Arch: game.ArchX64, API: game.APIDXGI}},
				{Executable: game.Executable{
					Path: "VC_redist.x64.exe", Arch: game.ArchX86,
					API: game.APIUnknown, Skipped: true,
				}},
			},
		},
		{
			Game: game.Game{
				ID: "steam:570", Name: "Dota 2",
				Provider: "steam", Root: "/games/dota 2 beta",
			},
			NativeBuild: true,
		},
		{
			Game: game.Game{
				ID: "steam:1245620", Name: "ELDEN RING",
				Provider: "steam", Root: "/games/ELDEN RING",
			},
			Exes: []Executable{
				{
					Executable: game.Executable{
						Path: "Game/eldenring.exe", Arch: game.ArchX64, API: game.APID3D12,
					},
					Installed: &installed,
				},
				{Executable: game.Executable{
					Path: "Game/start_protected_game.exe", Arch: game.ArchX64, API: game.APIUnknown,
				}},
			},
		},
		{
			Game: game.Game{
				ID: "manual:abc123", Name: "My GOG Game",
				Provider: "manual", Root: "/games/gog",
			},
			Exes: []Executable{
				{Executable: game.Executable{
					Path: "unins000.exe", Arch: game.ArchX86, API: game.APIUnknown, Skipped: true,
				}},
			},
		},
	}
}
