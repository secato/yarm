//go:build linux || darwin

package paths

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

// osDirs resolves the default directories on Linux and macOS: standard
// XDG config/data/cache locations.
func osDirs() Dirs {
	return Dirs{
		Config: filepath.Join(xdg.ConfigHome, appName),
		Data:   filepath.Join(xdg.DataHome, appName),
		Cache:  filepath.Join(xdg.CacheHome, appName),
	}
}
