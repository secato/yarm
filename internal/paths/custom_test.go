package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeTree creates files at the given relative paths under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// Custom content lives with the data, not with the cache, and is not moved
// by the cache_dir setting: that setting is for putting a large disposable
// download cache on another disk.
func TestCustomLivesWithTheData(t *testing.T) {
	d := Dirs{Config: "/cfg", Data: "/data", Cache: "/cache"}
	if got, want := d.Custom(), filepath.Join("/data", "custom"); got != want {
		t.Errorf("Custom() = %q, want %q", got, want)
	}
}

// The whole point of the move: content a user placed by hand must survive
// the cache being cleared, so it has to end up out of the cache.
func TestMigrateMovesContentOutOfTheCache(t *testing.T) {
	tmp := t.TempDir()
	oldDir := filepath.Join(tmp, "cache", "custom")
	newDir := filepath.Join(tmp, "data", "custom")

	writeTree(t, oldDir, map[string]string{
		"shaders/MyPack/Shaders/Cool.fx":      "// cool",
		"shaders/MyPack/Textures/noise.png":   "png",
		"addons/MyAddon/thing.addon64":        "binary",
		"shaders/Deep/nested/deeper/file.fxh": "nested",
	})

	moved, err := MigrateCustom(oldDir, newDir)
	if err != nil {
		t.Fatalf("MigrateCustom: %v", err)
	}
	if !moved {
		t.Fatal("MigrateCustom reported nothing moved")
	}

	if got := readFile(t, filepath.Join(newDir, "shaders", "MyPack", "Shaders", "Cool.fx")); got != "// cool" {
		t.Errorf("moved file contains %q", got)
	}
	if got := readFile(t, filepath.Join(newDir, "shaders", "Deep", "nested", "deeper", "file.fxh")); got != "nested" {
		t.Errorf("nested file contains %q", got)
	}
	if _, err := os.Stat(filepath.Join(newDir, "addons", "MyAddon", "thing.addon64")); err != nil {
		t.Errorf("add-on did not come across: %v", err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Error("the old directory is still there after a successful move")
	}
}

// Called on every launch, so every launch after the first must be a no-op.
func TestMigrateIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	oldDir := filepath.Join(tmp, "cache", "custom")
	newDir := filepath.Join(tmp, "data", "custom")
	writeTree(t, oldDir, map[string]string{"shaders/A/Shaders/a.fx": "a"})

	for i := range 3 {
		moved, err := MigrateCustom(oldDir, newDir)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if want := i == 0; moved != want {
			t.Errorf("run %d: moved = %v, want %v", i, moved, want)
		}
	}
	if got := readFile(t, filepath.Join(newDir, "shaders", "A", "Shaders", "a.fx")); got != "a" {
		t.Errorf("content changed across runs: %q", got)
	}
}

// A user who already has both directories has been managing them by hand.
// Merging could resurrect content they deleted, so the new one wins and
// the old one is left untouched for them to deal with.
func TestMigrateDoesNotMergeIntoAnExistingFolder(t *testing.T) {
	tmp := t.TempDir()
	oldDir := filepath.Join(tmp, "cache", "custom")
	newDir := filepath.Join(tmp, "data", "custom")
	writeTree(t, oldDir, map[string]string{"shaders/Old/Shaders/old.fx": "old"})
	writeTree(t, newDir, map[string]string{"shaders/New/Shaders/new.fx": "new"})

	moved, err := MigrateCustom(oldDir, newDir)
	if err != nil {
		t.Fatalf("MigrateCustom: %v", err)
	}
	if moved {
		t.Error("it moved something into a directory that already existed")
	}
	if _, err := os.Stat(filepath.Join(newDir, "shaders", "Old")); !os.IsNotExist(err) {
		t.Error("the old content was merged in")
	}
	if _, err := os.Stat(filepath.Join(oldDir, "shaders", "Old", "Shaders", "old.fx")); err != nil {
		t.Errorf("the old content was not left where it was: %v", err)
	}
}

// A fresh install has nothing to move, which is the common case and must
// not be an error.
func TestMigrateWithNothingToMove(t *testing.T) {
	tmp := t.TempDir()
	moved, err := MigrateCustom(filepath.Join(tmp, "cache", "custom"), filepath.Join(tmp, "data", "custom"))
	if err != nil {
		t.Fatalf("MigrateCustom: %v", err)
	}
	if moved {
		t.Error("it claims to have moved something that did not exist")
	}
	if _, err := os.Stat(filepath.Join(tmp, "data", "custom")); !os.IsNotExist(err) {
		t.Error("it created an empty destination for nothing")
	}
}

// YARM_HOME points cache and data at the same root; the two paths still
// differ, but a configuration that made them identical must not delete the
// directory by renaming it onto itself.
func TestMigrateIgnoresIdenticalPaths(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "custom")
	writeTree(t, dir, map[string]string{"shaders/A/Shaders/a.fx": "a"})

	moved, err := MigrateCustom(dir, dir)
	if err != nil {
		t.Fatalf("MigrateCustom: %v", err)
	}
	if moved {
		t.Error("it reported moving a directory onto itself")
	}
	if got := readFile(t, filepath.Join(dir, "shaders", "A", "Shaders", "a.fx")); got != "a" {
		t.Errorf("content is %q after a no-op migration", got)
	}
}

// copyTree is the fallback when the cache is on another disk. Exercised
// directly, since a test cannot readily arrange two filesystems.
func TestCopyTreePreservesTheTree(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	writeTree(t, src, map[string]string{
		"shaders/A/Shaders/a.fx": "a",
		"addons/B/b.addon32":     "b",
	})
	// A symlink is skipped rather than followed: what it points at is
	// outside the tree being copied.
	if err := os.Symlink(filepath.Join(src, "shaders"), filepath.Join(src, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}
	if got := readFile(t, filepath.Join(dst, "shaders", "A", "Shaders", "a.fx")); got != "a" {
		t.Errorf("copied file contains %q", got)
	}
	if got := readFile(t, filepath.Join(dst, "addons", "B", "b.addon32")); got != "b" {
		t.Errorf("copied add-on contains %q", got)
	}
	if _, err := os.Lstat(filepath.Join(dst, "link")); !os.IsNotExist(err) {
		t.Error("the symlink was copied")
	}
}

// When the directory cannot simply be renamed — the real case is a
// cache_dir on another filesystem — the content is copied instead, and
// the user still ends up with it in the new location. Simulated here by
// taking write permission off the parent, which is what stops a rename.
func TestMigrateFallsBackToCopyingWhenItCannotRename(t *testing.T) {
	// Skipped rather than run on Windows, where os.Chmod only toggles a
	// read-only attribute and does not stop a rename. The test would still
	// pass there — by taking the rename fast path, which is the one thing it
	// is not meant to exercise — and a test that passes without testing
	// anything is worse than one that says it was skipped.
	if runtime.GOOS == "windows" {
		t.Skip("windows does not enforce POSIX permission bits, so the rename would succeed")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	tmp := t.TempDir()
	oldParent := filepath.Join(tmp, "cache")
	oldDir := filepath.Join(oldParent, "custom")
	newDir := filepath.Join(tmp, "data", "custom")
	writeTree(t, oldDir, map[string]string{"shaders/A/Shaders/a.fx": "a"})

	if err := os.Chmod(oldParent, 0o555); err != nil {
		t.Skipf("cannot make the parent read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(oldParent, 0o755) })

	moved, err := MigrateCustom(oldDir, newDir)
	if err != nil {
		t.Fatalf("MigrateCustom: %v", err)
	}
	if !moved {
		t.Fatal("MigrateCustom reported nothing moved")
	}
	if got := readFile(t, filepath.Join(newDir, "shaders", "A", "Shaders", "a.fx")); got != "a" {
		t.Errorf("copied file contains %q", got)
	}
	// Cleanup of the old location is best-effort and may be partial — here
	// the files inside come away but the directory itself cannot be
	// unlinked from its read-only parent. That is untidy, not harmful: the
	// content is already safely in its new home, so it must not be
	// reported as a failure.
}

// A destination that cannot be created is reported rather than silently
// leaving the content behind.
func TestMigrateReportsAnUnusableDestination(t *testing.T) {
	tmp := t.TempDir()
	oldDir := filepath.Join(tmp, "cache", "custom")
	writeTree(t, oldDir, map[string]string{"shaders/A/Shaders/a.fx": "a"})

	// A file where the destination's parent directory needs to be.
	blocker := filepath.Join(tmp, "data")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	moved, err := MigrateCustom(oldDir, filepath.Join(blocker, "custom"))
	if err == nil {
		t.Fatal("want an error when the destination cannot be created")
	}
	if moved {
		t.Error("it reported moving something after failing")
	}
	if got := readFile(t, filepath.Join(oldDir, "shaders", "A", "Shaders", "a.fx")); got != "a" {
		t.Errorf("the content was disturbed by a failed migration: %q", got)
	}
}
