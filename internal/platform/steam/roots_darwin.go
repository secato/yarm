//go:build darwin

package steam

import (
	"os"
	"path/filepath"
)

// defaultRoots returns candidate Steam install directories on macOS.
func defaultRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, "Library", "Application Support", "Steam"),
	}
}
