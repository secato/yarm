package paths

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Custom content — shaders and add-ons the user assembled by hand — used
// to live in the cache, next to the catalog packages it appears beside in
// the wizard. That was the wrong reading of what it is. Everything else
// under the cache root can be deleted and re-downloaded; custom content
// cannot be re-anything. It is the only thing there yarm could not put
// back, and ~/.cache is a directory the XDG spec, distribution cleanup
// tools and plenty of users all treat as disposable.
//
// So it lives in the data directory now, beside installs.json, which is
// the other thing whose loss cannot be undone.

// DirCustom is the directory name holding custom shaders and add-ons.
const DirCustom = "custom"

// Custom returns where custom content lives: <data>/custom.
//
// Deliberately not affected by the cache_dir setting. That setting exists
// to put a large, disposable download cache on another disk, and custom
// content is neither large nor disposable.
func (d Dirs) Custom() string {
	return filepath.Join(d.Data, DirCustom)
}

// MigrateCustom moves an existing custom-content directory from the cache
// to the data directory, returning whether it moved anything.
//
// It does nothing when the destination already exists, so it is safe to
// call on every launch, and it never merges: a user who has both has been
// managing them by hand, and quietly combining them could resurrect
// content they deleted. It never deletes either, either — the worst case
// is that the old directory is left where it was.
func MigrateCustom(oldDir, newDir string) (moved bool, err error) {
	if oldDir == "" || newDir == "" || oldDir == newDir {
		return false, nil
	}
	if _, err := os.Stat(newDir); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("check %s: %w", newDir, err)
	}
	info, err := os.Stat(oldDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil // nothing to move: a fresh install
	case err != nil:
		return false, fmt.Errorf("check %s: %w", oldDir, err)
	case !info.IsDir():
		return false, nil
	}

	if err := os.MkdirAll(filepath.Dir(newDir), 0o755); err != nil {
		return false, fmt.Errorf("create %s: %w", filepath.Dir(newDir), err)
	}

	// The fast path. A rename fails when the cache has been pointed at
	// another disk — which is exactly the setup that makes a rename
	// impossible and a copy necessary. Rather than test for that errno
	// (EXDEV on unix, ERROR_NOT_SAME_DEVICE on Windows, and neither is
	// the only way this can fail), just try the copy and report both
	// errors if that fails too, so the real cause is never hidden.
	renameErr := os.Rename(oldDir, newDir)
	if renameErr == nil {
		return true, nil
	}

	if err := copyTree(oldDir, newDir); err != nil {
		// Leave the original alone and take the copy away, so a failure
		// halfway through does not present a partial directory as if it
		// were the whole of the user's content.
		_ = os.RemoveAll(newDir)
		return false, fmt.Errorf("move %s to %s: %w (copying instead: %w)", oldDir, newDir, renameErr, err)
	}
	// The content is safely in its new home; the old copy lingering is
	// untidy, not harmful, so a failure to remove it is not reported.
	_ = os.RemoveAll(oldDir)
	return true, nil
}

// copyTree copies a directory recursively, following the same rule the
// rest of yarm uses: regular files and directories only. A symlink in
// custom content would be pointing somewhere the copy cannot preserve
// meaningfully, so it is skipped rather than followed.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		switch {
		case entry.IsDir():
			info, err := entry.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm())
		case entry.Type()&fs.ModeSymlink != 0:
			return nil
		case !entry.Type().IsRegular():
			return nil
		}
		return copyFile(path, target, entry)
	})
}

func copyFile(src, dst string, entry fs.DirEntry) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
