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

	// The root must be a directory holding the recorded install: an empty
	// root, a file, or a folder without the recorded executable (or its
	// directory — a game patch may rename the exe, but not the folder) is
	// a registry pointing somewhere it should not, and deleting by it
	// could remove another folder's files.
	if root == "" {
		return UninstallResult{}, fmt.Errorf("game %q has an empty root", req.GameID)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		if os.IsNotExist(err) {
			// The whole game folder is gone: nothing on disk to remove,
			// just report everything missing and drop the record.
			for _, f := range in.Files {
				result.Missing = append(result.Missing, f.Path)
			}
			reg.Remove(req.GameID, req.Exe)
			if err := state.Save(u.StateDir, reg); err != nil {
				return UninstallResult{}, fmt.Errorf("update manifest: %w", err)
			}
			return result, nil
		}
		return UninstallResult{}, fmt.Errorf("game root %q is not a directory", root)
	}
	exeDir := exeDirOf(in.Exe)
	exeAbs := filepath.Join(root, filepath.FromSlash(in.Exe))
	if st, err := os.Stat(exeAbs); err != nil || st.IsDir() {
		dirAbs := filepath.Join(root, filepath.FromSlash(exeDir))
		if st, err := os.Stat(dirAbs); err != nil || !st.IsDir() {
			return UninstallResult{}, fmt.Errorf(
				"game root %q does not contain the recorded install (%q)", root, in.Exe)
		}
	}

	for _, f := range in.Files {
		if isUserData(f.Path) && !req.RemoveUserData {
			result.Kept = append(result.Kept, f.Path)
			continue
		}

		// A manifest entry shaped like nothing yarm writes (a savegame, a
		// config elsewhere) is skipped and reported, however its hash
		// reads: the hash gate alone cannot catch an entry whose content
		// the forger supplied too.
		if !deletableShape(exeDir, f) {
			result.Kept = append(result.Kept, f.Path)
			continue
		}

		abs, err := safeDest(root, f.Path)
		if err != nil {
			return UninstallResult{}, err
		}

		// Regular files only: never a symlink, directory, or special
		// file. yarm only ever writes regular files, so anything else is
		// the user's (or an attacker's) — and removing a symlink would
		// only remove the link, hiding what it pointed at from the
		// report.
		if st, err := os.Lstat(abs); err == nil && !st.Mode().IsRegular() {
			result.Kept = append(result.Kept, f.Path)
			continue
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
		// The file a backup restores over must itself be one yarm could
		// have displaced. Without this a forged manifest could drop the
		// content of any .yarm-bak it planted anywhere under the root.
		if !deletableShape(exeDir, state.File{Path: b.Path, Origin: state.OriginAdopted}) {
			continue
		}
		dst, err := safeDest(root, b.Path)
		if err != nil {
			return UninstallResult{}, err
		}
		src, err := safeDest(root, b.Backup)
		if err != nil {
			return UninstallResult{}, err
		}
		// The backup must still be a regular file: Load's validation
		// guarantees the suffixed name, but nothing stops the file itself
		// being swapped for a link between installs. Anything else is
		// not restored over the game folder.
		if st, err := os.Lstat(src); err != nil || !st.Mode().IsRegular() {
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

// exeDirOf returns the directory holding exe, relative to the game root
// ("" for the root itself) — the same rule Request.ExeDir applies, since
// the manifest's Exe is recorded in the same form.
func exeDirOf(exe string) string {
	if d := path.Dir(filepath.ToSlash(exe)); d != "." {
		return d
	}
	return ""
}

// deletableShape reports whether a manifest entry is shaped like a file
// yarm writes: the proxy DLL, compiler, ini or add-ons beside the
// executable, or shaders and textures under reshade-shaders/. Adopted
// installs accept the union of those shapes. Anything else fails, however
// its hash reads.
func deletableShape(exeDir string, f state.File) bool {
	dir, base := path.Split(f.Path)
	dir = strings.TrimSuffix(dir, "/")
	lower := strings.ToLower(base)
	inExeDir := dir == exeDir
	under := func(sub string) bool {
		prefix := sub + "/"
		if exeDir != "" {
			prefix = exeDir + "/" + prefix
		}
		return strings.HasPrefix(f.Path, prefix)
	}
	addon := strings.HasSuffix(lower, ".addon32") || strings.HasSuffix(lower, ".addon64")

	switch {
	case f.Origin == state.OriginReShade:
		return inExeDir && strings.HasSuffix(lower, ".dll")
	case f.Origin == state.OriginD3DCompiler:
		return inExeDir && lower == "d3dcompiler_47.dll"
	case f.Origin == state.OriginINI:
		return inExeDir && lower == "reshade.ini"
	case strings.HasPrefix(string(f.Origin), "package:"),
		strings.HasPrefix(string(f.Origin), "custom:"):
		return under(ShadersDir) || under(TexturesDir)
	case strings.HasPrefix(string(f.Origin), "addon:"),
		strings.HasPrefix(string(f.Origin), "renodx:"):
		return inExeDir && addon
	case f.Origin == state.OriginAdopted:
		return (inExeDir && strings.HasSuffix(lower, ".dll")) ||
			(inExeDir && lower == "d3dcompiler_47.dll") ||
			(inExeDir && lower == "reshade.ini") ||
			under(ShadersDir) || under(TexturesDir) ||
			(inExeDir && addon)
	}
	return false
}

// isUserData reports whether a path is content the user owns, which
// survives an ordinary uninstall.
func isUserData(rel string) bool {
	base := path.Base(rel)
	return strings.EqualFold(base, PresetName) || strings.EqualFold(base, LogName)
}
