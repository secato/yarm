//go:build windows

package heroic

import (
	"os"
	"path/filepath"
)

// DefaultConfigDirs returns the candidate Heroic config directory on
// Windows: Heroic keeps its state under %APPDATA%\heroic.
func DefaultConfigDirs() []string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return nil
	}
	return []string{filepath.Join(appData, "heroic")}
}
