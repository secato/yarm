// Package paths resolves the application's config, data (state) and cache
// directories, honoring the YARM_HOME override.
package paths

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/adrg/xdg"
)

const appName = "yarm"

// Dirs holds the resolved configuration, data (state) and cache directories.
type Dirs struct {
	Config string
	Data   string
	Cache  string
}

// Resolve determines the three application directories.
//
// If YARM_HOME is set, all three live under it (config/, data/, cache/
// subdirectories) — useful for tests and portable installs. Otherwise they
// follow OS convention: XDG dirs on Linux/macOS, and a single
// %LOCALAPPDATA%\yarm folder (with a cache subfolder) on Windows.
func Resolve() Dirs {
	if home := os.Getenv("YARM_HOME"); home != "" {
		return Dirs{
			Config: filepath.Join(home, "config"),
			Data:   filepath.Join(home, "data"),
			Cache:  filepath.Join(home, "cache"),
		}
	}
	return osDirs()
}

func osDirs() Dirs {
	if runtime.GOOS == "windows" {
		// xdg.ConfigHome and xdg.DataHome both resolve to %LOCALAPPDATA%
		// on Windows; keep everything under one app folder there.
		base := filepath.Join(xdg.ConfigHome, appName)
		return Dirs{Config: base, Data: base, Cache: filepath.Join(base, "cache")}
	}
	return Dirs{
		Config: filepath.Join(xdg.ConfigHome, appName),
		Data:   filepath.Join(xdg.DataHome, appName),
		Cache:  filepath.Join(xdg.CacheHome, appName),
	}
}

// EnsureAll creates the three directories, including any missing parents.
func (d Dirs) EnsureAll() error {
	for _, dir := range []string{d.Config, d.Data, d.Cache} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}
