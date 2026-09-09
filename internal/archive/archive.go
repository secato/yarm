// Package archive reads zip files safely: every extracted path is checked
// to stay inside the destination directory, so a hostile or malformed
// archive cannot write outside it ("zip slip").
package archive

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnsafePath is returned when an archive entry would escape the
// destination directory.
var ErrUnsafePath = errors.New("unsafe path in archive")

// ErrTooLarge is returned when an archive exceeds the extraction limits.
var ErrTooLarge = errors.New("archive exceeds extraction limits")

// Limits bound how much one archive may expand to. Package zips come from
// arbitrary repositories listed in a third-party catalog, so a hostile or
// merely broken one must not be able to fill the disk.
type Limits struct {
	// MaxEntrySize caps a single extracted file.
	MaxEntrySize int64
	// MaxTotalSize caps everything extracted from one archive. Without
	// this, a per-entry cap alone still allows N x MaxEntrySize.
	MaxTotalSize int64
	// MaxEntryCount caps how many files one archive may yield.
	MaxEntryCount int
}

// DefaultLimits are generous next to real content — the largest effect
// package in the catalog expands to about 20 MB — while still bounding a
// bomb.
func DefaultLimits() Limits {
	return Limits{
		MaxEntrySize:  512 << 20,
		MaxTotalSize:  2 << 30,
		MaxEntryCount: 20000,
	}
}

// Budget enforces Limits across the entries of a single archive. Create
// one per archive and extract every entry through it, so the running total
// is actually tracked rather than each entry being checked in isolation.
type Budget struct {
	limits Limits
	used   int64
	count  int
}

// NewBudget returns a Budget enforcing l.
func NewBudget(l Limits) *Budget { return &Budget{limits: l} }

// Precheck rejects an archive up front using the sizes its headers
// declare, so an obvious bomb costs nothing to refuse. Declared sizes are
// attacker-controlled, so passing this is necessary but not sufficient;
// ExtractEntry enforces the same limits against bytes actually written.
func Precheck(entries []Entry, l Limits) error {
	if len(entries) > l.MaxEntryCount {
		return fmt.Errorf("%w: %d entries, limit %d", ErrTooLarge, len(entries), l.MaxEntryCount)
	}
	var declared int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Size() > l.MaxEntrySize {
			return fmt.Errorf("%w: entry %q declares %d bytes, limit %d",
				ErrTooLarge, e.Name(), e.Size(), l.MaxEntrySize)
		}
		declared += e.Size()
		if declared > l.MaxTotalSize {
			return fmt.Errorf("%w: entries declare over %d bytes total",
				ErrTooLarge, l.MaxTotalSize)
		}
	}
	return nil
}

// Reader is the subset of *zip.Reader this package needs, so callers can
// pass a reader built over a file, a byte slice, or an appended payload.
type Reader interface {
	Files() []Entry
}

// Entry is one file inside an archive.
type Entry interface {
	// Name is the archive-relative path, always slash-separated.
	Name() string
	// Size is the uncompressed size.
	Size() int64
	// IsDir reports whether the entry is a directory.
	IsDir() bool
	// Open returns the entry's contents.
	Open() (io.ReadCloser, error)
}

// SafePath joins dst and an archive entry name, rejecting anything that
// could escape dst: absolute paths, Windows drive letters and UNC paths,
// and any ".." component at all.
//
// Note that it rejects rather than sanitizes. Cleaning "../evil.txt" would
// yield a path safely inside dst, but at a location the archive never
// named — the install manifest would then record a file the archive did
// not ask for, and a hostile archive would still get its payload written.
// An entry that cannot be placed where it says belongs nowhere.
//
// Backslashes are normalized first, because zips written on Windows
// sometimes use them as separators and would otherwise slip past a
// slash-only check.
func SafePath(dst, name string) (string, error) {
	reject := func() (string, error) {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}

	norm := strings.ReplaceAll(name, `\`, "/")
	if norm == "" || strings.HasPrefix(norm, "/") || hasVolumeName(norm) {
		return reject()
	}

	var parts []string
	for _, p := range strings.Split(norm, "/") {
		switch p {
		case "", ".":
			// Empty segments and "." carry no meaning; drop them.
		case "..":
			return reject()
		default:
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return reject()
	}

	return filepath.Join(dst, filepath.Join(parts...)), nil
}

// hasVolumeName reports whether a normalized archive name starts with a
// Windows drive letter ("C:") or a UNC share. filepath.VolumeName only
// recognizes these when running on Windows, so the check is spelled out
// to hold on every platform: an archive is not trusted more because it is
// being unpacked on Linux.
func hasVolumeName(name string) bool {
	if strings.HasPrefix(name, "//") {
		return true
	}
	return len(name) >= 2 && name[1] == ':' &&
		((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z'))
}

// StripPrefix removes a leading directory component from an archive
// entry's name, returning "" when the entry is that directory itself.
func StripPrefix(name, prefix string) string {
	if prefix == "" {
		return name
	}
	n := strings.TrimPrefix(strings.ReplaceAll(name, `\`, "/"), "./")
	n = strings.TrimPrefix(n, prefix)
	return strings.TrimPrefix(n, "/")
}

// ExtractEntry writes one archive entry to dst, creating parent
// directories, and charges what it writes against the budget.
//
// The size an archive declares for an entry is attacker-controlled, so it
// is used only as a cheap pre-filter. The copy is bounded independently
// and an entry that delivers more than it declared is an error, not a
// silently truncated file: quietly writing a partial DLL or shader would
// hand the install engine a corrupt artifact that looks complete.
//
// The entry is always written as a regular file. Archives can carry
// symlink entries, and honoring one would let an archive plant a link that
// a later entry writes through, escaping the destination despite SafePath.
func (b *Budget) ExtractEntry(e Entry, dst string) error {
	b.count++
	if b.count > b.limits.MaxEntryCount {
		return fmt.Errorf("%w: more than %d entries", ErrTooLarge, b.limits.MaxEntryCount)
	}

	declared := e.Size()
	if declared > b.limits.MaxEntrySize {
		return fmt.Errorf("%w: entry %q declares %d bytes, limit %d",
			ErrTooLarge, e.Name(), declared, b.limits.MaxEntrySize)
	}

	remaining := b.limits.MaxTotalSize - b.used
	if remaining < 0 {
		remaining = 0
	}
	// Allow one byte past what is permitted so that hitting the cap is
	// detectable rather than indistinguishable from a clean EOF.
	allowed := min(declared, remaining)

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	rc, err := e.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	f, err := os.Create(dst)
	if err != nil {
		return err
	}

	written, err := io.Copy(f, io.LimitReader(rc, allowed+1))
	if err == nil {
		err = f.Sync()
	}
	// The handle is closed here, before any os.Remove below, and not in a
	// defer. Windows refuses to unlink a file that still has an open
	// handle, so with a deferred close every rejected partial file would
	// survive on disk — precisely the truncated artifact this function
	// exists not to hand on. Unix hides the mistake, because unlinking an
	// open file works there; the Windows CI run is what caught it.
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
		return err
	}

	if written > declared {
		_ = os.Remove(dst)
		return fmt.Errorf("%w: entry %q declared %d bytes but delivered more",
			ErrTooLarge, e.Name(), declared)
	}
	if written > remaining {
		_ = os.Remove(dst)
		return fmt.Errorf("%w: archive expands past %d bytes total",
			ErrTooLarge, b.limits.MaxTotalSize)
	}

	b.used += written
	return nil
}

// FindDir returns the archive-relative path of the first directory named
// name (case-insensitively) at or above depth levels below root, or "".
// Used to locate "Shaders" and "Textures" inside a package zip whose
// layout varies by repository.
func FindDir(entries []Entry, name string, maxDepth int) string {
	best := ""
	bestDepth := maxDepth + 1

	for _, e := range entries {
		n := strings.Trim(strings.ReplaceAll(e.Name(), `\`, "/"), "/")
		if n == "" {
			continue
		}
		parts := strings.Split(n, "/")

		// A directory entry names itself; a file entry names its parents.
		limit := len(parts)
		if !e.IsDir() {
			limit--
		}
		for i := range limit {
			if !strings.EqualFold(parts[i], name) {
				continue
			}
			depth := i // 0 = directly at the archive root
			if depth <= maxDepth && depth < bestDepth {
				best = strings.Join(parts[:i+1], "/")
				bestDepth = depth
			}
			break
		}
	}
	return best
}
