//go:build windows

package paths

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

// osDirs resolves the default directories on Windows.
//
// xdg.ConfigHome and xdg.DataHome both resolve to %LOCALAPPDATA% here, so
// config and state would otherwise collide in the same folder; everything
// is kept under one app folder instead, with cache as an explicit
// subfolder.
func osDirs() Dirs {
	base := filepath.Join(xdg.ConfigHome, appName)
	return Dirs{Config: base, Data: base, Cache: filepath.Join(base, "cache")}
}
