package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/secato/yarm/internal/safetext"
)

// Subdirectories of the custom-content folder.
const (
	CustomShadersDir = "shaders"
	CustomAddonsDir  = "addons"
	// customMetaFile optionally overrides a folder's display name and
	// description.
	customMetaFile = "yarm.json"
)

// CustomKind distinguishes the two kinds of user-managed content.
type CustomKind string

// CustomKind values.
const (
	CustomShaders CustomKind = "shaders"
	CustomAddons  CustomKind = "addons"
)

// Custom is one user-managed folder of shaders or add-ons.
type Custom struct {
	// ID is "custom:<kind>:<slug>", distinct from catalog package ids so
	// the two can share a selection list without colliding.
	ID          string
	Name        string
	Description string
	Kind        CustomKind
	// Path is the absolute path to the content folder.
	Path string
}

// customMeta is the optional yarm.json override.
type customMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ScanCustom lists user-managed content under root. A
// missing root is not an error: most users never create one.
//
// A shaders folder counts only if it actually holds shaders, and an addons
// folder only if it holds .addon32/.addon64 files, so an empty or
// half-populated directory does not appear as installable content.
func ScanCustom(root string) ([]Custom, error) {
	var out []Custom

	for _, kind := range []CustomKind{CustomShaders, CustomAddons} {
		dir := filepath.Join(root, string(kind))
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if !hasCustomContent(path, kind) {
				continue
			}

			c := Custom{
				ID:   "custom:" + string(kind) + ":" + Slugify(e.Name()),
				Name: safetext.Clean(e.Name()),
				Kind: kind,
				Path: path,
			}
			if m, ok := readCustomMeta(path); ok {
				if m.Name != "" {
					c.Name = safetext.Clean(m.Name)
				}
				c.Description = safetext.Clean(m.Description)
			}
			out = append(out, c)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// hasCustomContent reports whether a folder holds the files its kind
// implies.
func hasCustomContent(path string, kind CustomKind) bool {
	if kind == CustomShaders {
		if st, err := os.Stat(filepath.Join(path, "Shaders")); err == nil && st.IsDir() {
			return true
		}
		return anyFileMatches(path, func(name string) bool {
			ext := strings.ToLower(filepath.Ext(name))
			return ext == ".fx" || ext == ".fxh"
		})
	}

	return anyFileMatches(path, func(name string) bool {
		ext := strings.ToLower(filepath.Ext(name))
		return ext == ".addon32" || ext == ".addon64"
	})
}

// anyFileMatches reports whether any regular file directly inside dir
// satisfies pred.
func anyFileMatches(dir string, pred func(name string) bool) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && pred(e.Name()) {
			return true
		}
	}
	return false
}

// readCustomMeta loads an optional yarm.json from a content folder.
func readCustomMeta(dir string) (customMeta, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, customMetaFile))
	if err != nil {
		return customMeta{}, false
	}
	var m customMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return customMeta{}, false
	}
	return m, true
}
