package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/fetch"
	"github.com/secato/yarm/internal/fsutil"
	"github.com/secato/yarm/internal/game"
)

// Subdirectories of the cache root.
const (
	DirReShade     = "reshade"
	DirPackages    = "packages"
	DirAddons      = "addons"
	DirD3DCompiler = "d3dcompiler"
	DirDownloads   = "downloads"
	DirCatalog     = "catalog"
	DirCustom      = "custom"
)

// Flavor names the two ReShade builds.
const (
	FlavorNormal = "normal"
	FlavorAddon  = "addon"
)

// Cache stores artifacts under Root, downloading them on demand.
type Cache struct {
	Root  string
	Fetch *fetch.Client
	// SetupURL builds the ReShade setup executable's URL. Overridable so
	// tests can serve a fixture without reaching reshade.me.
	SetupURL func(version string, addon bool) string
	// Now is overridable for tests.
	Now func() time.Time
}

// New returns a Cache rooted at root.
func New(root string, f *fetch.Client) *Cache {
	return &Cache{
		Root:     root,
		Fetch:    f,
		SetupURL: catalog.SetupURL,
		Now:      time.Now,
	}
}

// setupURL resolves the ReShade download URL, defaulting to reshade.me.
func (c *Cache) setupURL(version string, addon bool) string {
	if c.SetupURL != nil {
		return c.SetupURL(version, addon)
	}
	return catalog.SetupURL(version, addon)
}

func (c *Cache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// abs turns a cache-relative path into an absolute one.
func (c *Cache) abs(rel string) string { return filepath.Join(c.Root, rel) }

// HasReShade reports whether a ReShade build is already cached, for
// UIs that want to flag a version as "cached" without triggering a
// download to find out.
func (c *Cache) HasReShade(version string, addon bool) bool {
	flavor := FlavorNormal
	if addon {
		flavor = FlavorAddon
	}
	dir := c.abs(filepath.Join(DirReShade, version, flavor))
	complete, err := hasFiles(dir, artifacts.ReShade32, artifacts.ReShade64)
	return err == nil && complete
}

// HasPackage reports whether any cached version of a package exists.
// Package cache keys carry a download date and content hash, so
// this checks for the id's directory rather than one exact version.
func (c *Cache) HasPackage(id string) bool {
	return nonEmpty(c.abs(filepath.Join(DirPackages, id)))
}

// HasAddon reports whether any cached version of an add-on exists.
func (c *Cache) HasAddon(id string) bool {
	return nonEmpty(c.abs(filepath.Join(DirAddons, id)))
}

// HasD3DCompiler reports whether a verified d3dcompiler_47.dll for arch is
// already cached, using the same size check EnsureD3DCompiler does.
func (c *Cache) HasD3DCompiler(arch game.Arch) bool {
	src, err := artifacts.D3DSourceFor(arch)
	if err != nil {
		return false
	}
	dll := c.abs(filepath.Join(DirD3DCompiler, string(arch), artifacts.D3DCompiler))
	st, err := os.Stat(dll)
	return err == nil && st.Size() == src.DLLSize
}

// EnsureReShade returns the directory holding ReShade32.dll and
// ReShade64.dll for a version and flavor, downloading and extracting the
// setup executable if it is not cached yet.
//
// The setup executable is deleted once extracted: it is ~4 MB of installer
// wrapped around the two DLLs we actually keep.
func (c *Cache) EnsureReShade(ctx context.Context, version string, addon bool, onProgress fetch.ProgressFunc) (string, error) {
	flavor := FlavorNormal
	if addon {
		flavor = FlavorAddon
	}
	rel := filepath.Join(DirReShade, version, flavor)
	dir := c.abs(rel)
	id := "reshade:" + version + ":" + flavor

	if complete, err := hasFiles(dir, artifacts.ReShade32, artifacts.ReShade64); err != nil {
		return "", err
	} else if complete {
		return dir, c.touch(id)
	}

	url := c.setupURL(version, addon)
	setup := c.abs(filepath.Join(DirDownloads, fmt.Sprintf("ReShade_Setup_%s_%s.exe", version, flavor)))
	defer func() { _ = os.Remove(setup) }()

	if err := c.Fetch.Download(ctx, url, setup, onProgress, ""); err != nil {
		return "", err
	}
	if err := artifacts.ExtractReShade(setup, dir); err != nil {
		return "", err
	}

	return dir, c.record(Entry{
		ID:        id,
		Kind:      KindReShade,
		Path:      rel,
		SourceURL: url,
		Version:   version,
		Name:      "ReShade " + version + " (" + flavor + ")",
	})
}

// EnsurePackage returns the directory holding a normalized effect package.
//
// GitHub branch archives are not versioned, so the cache key is the
// download date plus the first 7 hex characters of the zip's SHA-256.
// Re-downloading an unchanged branch therefore reuses the same directory
// on the same day, and a changed branch gets a new one.
func (c *Cache) EnsurePackage(ctx context.Context, pkg catalog.Package, onProgress fetch.ProgressFunc) (string, error) {
	zipPath := c.abs(filepath.Join(DirDownloads, pkg.ID+".zip"))
	defer func() { _ = os.Remove(zipPath) }()

	if err := c.Fetch.Download(ctx, pkg.DownloadURL, zipPath, onProgress, ""); err != nil {
		return "", err
	}

	sum, err := hashFile(zipPath)
	if err != nil {
		return "", err
	}
	version := c.now().Format("20060102") + "-" + sum[:7]

	rel := filepath.Join(DirPackages, pkg.ID, version)
	dir := c.abs(rel)
	id := "package:" + pkg.ID + ":" + version

	if complete, err := hasFiles(dir, artifacts.MetaFile); err != nil {
		return "", err
	} else if complete {
		return dir, c.touch(id)
	}

	meta := artifacts.PackageMeta{
		ID:              pkg.ID,
		Name:            pkg.Name,
		EffectFiles:     pkg.EffectFiles,
		DenyEffectFiles: pkg.DenyEffectFiles,
		SourceURL:       pkg.DownloadURL,
	}
	if err := artifacts.NormalizePackage(zipPath, dir, meta); err != nil {
		return "", err
	}

	return dir, c.record(Entry{
		ID:        id,
		Kind:      KindPackage,
		Path:      rel,
		SHA256:    sum,
		SourceURL: pkg.DownloadURL,
		Version:   version,
		Name:      pkg.Name,
	})
}

// EnsureAddon returns the directory holding an add-on's binaries for a
// game architecture.
func (c *Cache) EnsureAddon(ctx context.Context, addon catalog.Addon, arch game.Arch, onProgress fetch.ProgressFunc) (string, error) {
	src, ok := addon.SourceFor(arch)
	if !ok {
		return "", fmt.Errorf("add-on %s has no build for %s", addon.ID, arch)
	}

	version := c.now().Format("20060102")
	rel := filepath.Join(DirAddons, addon.ID, version+"-"+string(arch))
	dir := c.abs(rel)
	id := "addon:" + addon.ID + ":" + version + ":" + string(arch)

	if nonEmpty(dir) {
		return dir, c.touch(id)
	}

	download := c.abs(filepath.Join(DirDownloads, addon.ID+urlExt(src.URL)))
	defer func() { _ = os.Remove(download) }()

	if err := c.Fetch.Download(ctx, src.URL, download, onProgress, ""); err != nil {
		return "", err
	}

	if src.Archive {
		if _, err := artifacts.ExtractAddonZip(download, dir, arch); err != nil {
			return "", err
		}
	} else if _, err := artifacts.InstallAddonFile(download, dir, arch); err != nil {
		return "", err
	}

	return dir, c.record(Entry{
		ID:        id,
		Kind:      KindAddon,
		Path:      rel,
		SourceURL: src.URL,
		Version:   version,
		Name:      addon.Name,
	})
}

// EnsureD3DCompiler returns the path to a verified d3dcompiler_47.dll for
// an architecture, downloading the pinned Firefox installer if needed.
//
// Only ever needed for Linux/Proton installs; on Windows the system
// already has this DLL.
func (c *Cache) EnsureD3DCompiler(ctx context.Context, arch game.Arch, onProgress fetch.ProgressFunc) (string, error) {
	src, err := artifacts.D3DSourceFor(arch)
	if err != nil {
		return "", err
	}

	rel := filepath.Join(DirD3DCompiler, string(arch))
	dir := c.abs(rel)
	dll := filepath.Join(dir, artifacts.D3DCompiler)
	id := "d3dcompiler:" + string(arch)

	if st, err := os.Stat(dll); err == nil && st.Size() == src.DLLSize {
		return dll, c.touch(id)
	}

	installer := c.abs(filepath.Join(DirDownloads, "firefox-"+string(arch)+".exe"))
	defer func() { _ = os.Remove(installer) }()

	if err := c.Fetch.Download(ctx, src.URL, installer, onProgress, src.InstallerSHA256); err != nil {
		return "", err
	}
	if _, err := artifacts.ExtractD3DCompiler(installer, dir, src); err != nil {
		return "", err
	}

	return dll, c.record(Entry{
		ID:        id,
		Kind:      KindD3DCompiler,
		Path:      rel,
		SHA256:    src.DLLSHA256,
		SourceURL: src.URL,
		Name:      artifacts.D3DCompiler + " (" + string(arch) + ")",
	})
}

// List returns the cache's entries with their sizes re-measured from disk,
// so an entry deleted outside YARM does not linger with a stale size.
// Entries whose directory has vanished are dropped from the index.
func (c *Cache) List(by SortBy, desc bool) ([]Entry, error) {
	idx, err := loadIndex(c.Root)
	if err != nil {
		return nil, err
	}

	out := make([]Entry, 0, len(idx.Entries))
	changed := false

	for id, e := range idx.Entries {
		size, err := fsutil.DirSize(c.abs(e.Path))
		if err != nil {
			delete(idx.Entries, id)
			changed = true
			continue
		}
		e.Size = size
		out = append(out, e)
	}

	if changed {
		if err := saveIndex(c.Root, idx); err != nil {
			return nil, err
		}
	}

	sortEntries(out, by, desc)
	return out, nil
}

// Delete removes an entry's directory and its index row.
func (c *Cache) Delete(id string) error {
	idx, err := loadIndex(c.Root)
	if err != nil {
		return err
	}
	e, ok := idx.Entries[id]
	if !ok {
		return fmt.Errorf("cache entry %q not found", id)
	}

	// Refuse to remove anything outside the cache root, however the index
	// came to hold such a path. filepath.Join cleans "..", so a recorded
	// path like "../../elsewhere" resolves to a real directory outside the
	// root; only comparing the result back to the root catches that.
	target := c.abs(e.Path)
	if !withinRoot(c.Root, target) {
		return fmt.Errorf("refusing to delete %q: outside the cache root", e.Path)
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}

	delete(idx.Entries, id)
	return saveIndex(c.Root, idx)
}

// Clean removes every downloadable entry, leaving custom content alone.
func (c *Cache) Clean() (removed int, err error) {
	entries, err := c.List(SortByName, false)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if err := c.Delete(e.ID); err != nil {
			return removed, err
		}
		removed++
	}
	// Partial downloads are not indexed, so clear them explicitly.
	_ = os.RemoveAll(c.abs(DirDownloads))
	return removed, nil
}

// Total returns the cache's total size on disk.
func (c *Cache) Total() (int64, error) { return fsutil.DirSize(c.Root) }

// record adds or replaces an index entry.
func (c *Cache) record(e Entry) error {
	idx, err := loadIndex(c.Root)
	if err != nil {
		return err
	}

	size, err := fsutil.DirSize(c.abs(e.Path))
	if err != nil {
		return err
	}
	e.Size = size

	now := c.now()
	e.DownloadedAt = now
	e.LastUsedAt = now
	idx.Entries[e.ID] = e

	return saveIndex(c.Root, idx)
}

// touch updates an entry's last-used time, so the cache screen can show
// what is actually being used. A missing entry is not an error: the files
// are there, only the bookkeeping is absent.
func (c *Cache) touch(id string) error {
	idx, err := loadIndex(c.Root)
	if err != nil {
		return err
	}
	e, ok := idx.Entries[id]
	if !ok {
		return nil
	}
	e.LastUsedAt = c.now()
	idx.Entries[id] = e
	return saveIndex(c.Root, idx)
}

// urlExt returns a download URL's file extension, taken from the URL path
// only.
//
// filepath.Ext over a raw URL picks up any query string too
// ("release.zip?token=abc"), which on Windows is an invalid filename and
// fails the write outright. The extension is also restricted to plain
// alphanumerics so a catalog entry cannot steer the cache filename with
// punctuation.
func urlExt(raw string) string {
	path := raw
	if u, err := url.Parse(raw); err == nil && u.Path != "" {
		path = u.Path
	}

	ext := filepath.Ext(path)
	if len(ext) < 2 || len(ext) > 16 {
		return ""
	}
	for _, r := range ext[1:] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return ""
		}
	}
	return ext
}

// withinRoot reports whether target is the cache root itself or a path
// strictly inside it.
func withinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// hasFiles reports whether dir contains every named file.
func hasFiles(dir string, names ...string) (bool, error) {
	for _, n := range names {
		_, err := os.Stat(filepath.Join(dir, n))
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// nonEmpty reports whether dir exists and holds at least one file.
func nonEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// hashFile returns a file's hex SHA-256.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
