//go:build windows

package epic

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// launcherKey is where the Epic launcher records its data directory.
const launcherKey = `SOFTWARE\WOW6432Node\Epic Games\EpicGamesLauncher`

// defaultManifestDirs returns the candidate manifest directories: the
// registry-recorded data path first, the conventional location as a
// fallback for when the registry is unhelpful.
func defaultManifestDirs() []string {
	var dirs []string
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, launcherKey, registry.QUERY_VALUE); err == nil {
		if p, _, err := k.GetStringValue("AppDataPath"); err == nil && p != "" {
			dirs = append(dirs, filepath.Join(p, "Manifests"))
		}
		_ = k.Close()
	}
	if programData := os.Getenv("ProgramData"); programData != "" {
		dirs = append(dirs, filepath.Join(programData, "Epic", "EpicGamesLauncher", "Data", "Manifests"))
	}
	return dirs
}
