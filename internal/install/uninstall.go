package install

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/secato/yarm/internal/state"
)

// UninstallRequest names an install to remove.
type UninstallRequest struct {
	GameID string
	// Exe is the executable's path relative to the game root, as recorded
	// in the manifest.
	Exe string
	// RemoveUserData also deletes ReShadePreset.ini and ReShade.log.
	// Those are the user's own work, so they survive by default.
	RemoveUserData bool
}

// UninstallResult reports what an uninstall did.
type UninstallResult struct {
	// Removed lists the game-relative paths deleted.
	Removed []string
	// Kept lists files left in place because their content no longer
	// matches what YARM installed, so they may hold the user's edits.
	Kept []string
	// Restored lists files put back from a backup.
	Restored []string
	// Missing lists manifest entries that were already gone.
	Missing []string
}

// Uninstaller removes an install using the manifest recorded when it was
// created.
type Uninstaller struct {
	// StateDir holds installs.json.
	StateDir string
}

// NewUninstaller returns an Uninstaller reading from stateDir.
func NewUninstaller(stateDir string) *Uninstaller { return &Uninstaller{StateDir: stateDir} }

// Run removes an install.
//
// Only files the manifest lists are touched, and only when their content
// still matches what was installed. A file whose hash has changed is the
// user's now — an edited shader, a tweaked ini — so it is kept and
// reported rather than deleted. This is what lets uninstall be safe to
// run without the user auditing it first.
func (u *Uninstaller) Run(req UninstallRequest) (UninstallResult, error) {
	reg, err := state.Load(u.StateDir)
	if err != nil {
		return UninstallResult{}, err
	}

	g, ok := reg.Games[req.GameID]
	if !ok {
		return UninstallResult{}, fmt.Errorf("no installs recorded for game %q", req.GameID)
	}
	in, ok := reg.FindInstall(req.GameID, req.Exe)
	if !ok {
		return UninstallResult{}, fmt.Errorf("no install recorded for %q in %s", req.Exe, req.GameID)
	}

	var result UninstallResult
	root := g.Root

	for _, f := range in.Files {
		if isUserData(f.Path) && !req.RemoveUserData {
			result.Kept = append(result.Kept, f.Path)
			continue
		}

		abs, err := safeDest(root, f.Path)
		if err != nil {
			return UninstallResult{}, err
		}

		sum, err := hashFile(abs)
		if os.IsNotExist(err) {
			result.Missing = append(result.Missing, f.Path)
			continue
		}
		if err != nil {
			return UninstallResult{}, err
		}

		if sum != f.SHA256 {
			result.Kept = append(result.Kept, f.Path)
			continue
		}
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return UninstallResult{}, fmt.Errorf("remove %s: %w", f.Path, err)
		}
		result.Removed = append(result.Removed, f.Path)
	}

	// User data that was never in the manifest, only cleared on request.
	if req.RemoveUserData {
		exeDir := path.Dir(strings.TrimSuffix(filepath.ToSlash(in.Exe), path.Base(in.Exe)))
		exeDir = strings.TrimSuffix(exeDir, "/")
		for _, name := range []string{PresetName, LogName} {
			rel := join(exeDir, name)
			abs, err := safeDest(root, rel)
			if err != nil {
				continue
			}
			if err := os.Remove(abs); err == nil {
				result.Removed = append(result.Removed, rel)
			}
		}
	}

	// Put back anything this install displaced.
	for _, b := range in.Backups {
		dst, err := safeDest(root, b.Path)
		if err != nil {
			return UninstallResult{}, err
		}
		src, err := safeDest(root, b.Backup)
		if err != nil {
			return UninstallResult{}, err
		}
		if _, err := os.Stat(src); err != nil {
			continue
		}
		// Only restore over an absent file: if something is there, the
		// entry was kept above because the user changed it, and their
		// version wins over a stale backup.
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			return UninstallResult{}, fmt.Errorf("restore %s: %w", b.Path, err)
		}
		result.Restored = append(result.Restored, b.Path)
	}

	pruneEmptyDirs(root, result.Removed)

	reg.Remove(req.GameID, req.Exe)
	if err := state.Save(u.StateDir, reg); err != nil {
		return UninstallResult{}, fmt.Errorf("update manifest: %w", err)
	}

	sort.Strings(result.Removed)
	sort.Strings(result.Kept)
	return result, nil
}

// isUserData reports whether a path is content the user owns, which
// survives an ordinary uninstall.
func isUserData(rel string) bool {
	base := path.Base(rel)
	return strings.EqualFold(base, PresetName) || strings.EqualFold(base, LogName)
}
