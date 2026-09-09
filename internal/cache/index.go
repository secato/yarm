// Package cache stores downloaded and normalized artifacts under a single
// root, tracked by an index file so entries can be listed, sized and
// deleted without re-deriving what they are.
package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/secato/yarm/internal/fsutil"
)

// IndexFile is the index's name inside the cache root.
const IndexFile = "index.json"

// SchemaVersion is bumped when the on-disk index shape changes
// incompatibly.
const SchemaVersion = 1

// Kind classifies a cache entry.
type Kind string

// Kind values.
const (
	KindReShade     Kind = "reshade"
	KindPackage     Kind = "package"
	KindAddon       Kind = "addon"
	KindRenoDX      Kind = "renodx"
	KindD3DCompiler Kind = "d3dcompiler"
	KindCatalog     Kind = "catalog"
)

// Entry is one cached artifact.
type Entry struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	// Path is relative to the cache root, so the whole cache can be moved.
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	SHA256       string    `json:"sha256,omitempty"`
	SourceURL    string    `json:"source_url,omitempty"`
	Version      string    `json:"version,omitempty"`
	DownloadedAt time.Time `json:"downloaded_at"`
	LastUsedAt   time.Time `json:"last_used_at"`
	// Name is a human label for the cache screen.
	Name string `json:"name,omitempty"`
}

// Index is the on-disk catalog of cache entries.
type Index struct {
	Schema  int              `json:"schema"`
	Entries map[string]Entry `json:"entries"`
}

// ErrSchemaTooNew is returned when the index was written by a newer YARM.
var ErrSchemaTooNew = errors.New("cache index schema is newer than this version of yarm")

// loadIndex reads the index from root, returning an empty one when it does
// not exist yet.
func loadIndex(root string) (Index, error) {
	raw, err := os.ReadFile(filepath.Join(root, IndexFile))
	if os.IsNotExist(err) {
		return Index{Schema: SchemaVersion, Entries: map[string]Entry{}}, nil
	}
	if err != nil {
		return Index{}, err
	}

	var idx Index
	if err := json.Unmarshal(raw, &idx); err != nil {
		// A corrupt index must not brick the cache: the artifacts on disk
		// are still valid and will simply be re-registered as they are
		// used again.
		return Index{Schema: SchemaVersion, Entries: map[string]Entry{}}, nil
	}
	if idx.Schema > SchemaVersion {
		return Index{}, fmt.Errorf("%w: found %d, support %d", ErrSchemaTooNew, idx.Schema, SchemaVersion)
	}
	if idx.Entries == nil {
		idx.Entries = map[string]Entry{}
	}
	idx.Schema = SchemaVersion
	return idx, nil
}

// saveIndex writes the index atomically.
func saveIndex(root string, idx Index) error {
	idx.Schema = SchemaVersion
	raw, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(filepath.Join(root, IndexFile), raw, 0o644)
}

// SortBy names a cache listing order.
type SortBy string

// SortBy values.
const (
	SortByName SortBy = "name"
	SortBySize SortBy = "size"
	SortByDate SortBy = "date"
	SortByUsed SortBy = "used"
)

// sortEntries orders entries in place.
func sortEntries(entries []Entry, by SortBy, desc bool) {
	less := func(i, j int) bool {
		switch by {
		case SortBySize:
			return entries[i].Size < entries[j].Size
		case SortByDate:
			return entries[i].DownloadedAt.Before(entries[j].DownloadedAt)
		case SortByUsed:
			return entries[i].LastUsedAt.Before(entries[j].LastUsedAt)
		default:
			if entries[i].Kind != entries[j].Kind {
				return entries[i].Kind < entries[j].Kind
			}
			return entries[i].ID < entries[j].ID
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if desc {
			return less(j, i)
		}
		return less(i, j)
	})
}
