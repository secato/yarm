//go:build darwin

package heroic

import (
	"os"
	"path/filepath"
)

// DefaultConfigDirs returns the candidate Heroic config directory on
// macOS. yarm does not ship darwin builds; this exists so the package
// keeps compiling there, like the steam provider's darwin roots.
func DefaultConfigDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, "Library", "Application Support", "heroic")}
}
