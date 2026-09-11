// Package state records what YARM installed into which game, so an
// uninstall can remove exactly the files it created and no others.
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/secato/yarm/internal/fsutil"
)

// FileName is the registry's name inside the data directory.
const FileName = "installs.json"

// SchemaVersion is bumped when the on-disk shape changes incompatibly.
const SchemaVersion = 1

// Origin says which part of an install produced a file, so the cache
// screen and uninstall reporting can group them.
type Origin string

// Origin values. Package and add-on origins carry their id, as
// "package:<id>" and "addon:<id>".
const (
	OriginReShade     Origin = "reshade"
	OriginD3DCompiler Origin = "d3dcompiler"
	OriginINI         Origin = "ini"
	// OriginAdopted marks a file yarm did not write itself but has taken
	// over tracking for — an install found already on disk, from a
	// manual install or another tool, whose exact catalog provenance
	// (which package or add-on it came from) is not known.
	OriginAdopted Origin = "adopted"
)

// PackageOrigin returns the Origin for a package id.
func PackageOrigin(id string) Origin { return Origin("package:" + id) }

// AddonOrigin returns the Origin for an add-on id.
func AddonOrigin(id string) Origin { return Origin("addon:" + id) }

// RenoDXOrigin returns the Origin for a RenoDX mod id.
func RenoDXOrigin(id string) Origin { return Origin("renodx:" + id) }

// CustomOrigin returns the Origin for a custom content id.
func CustomOrigin(id string) Origin { return Origin("custom:" + id) }

// File is one file YARM wrote into a game directory.
type File struct {
	// Path is relative to the game root, slash-separated so a manifest
	// written on one OS still reads on another.
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Origin Origin `json:"origin"`
}

// Backup records a pre-existing file YARM displaced, so uninstall can put
// it back.
type Backup struct {
	Path   string `json:"path"`
	Backup string `json:"backup"`
}

// ReShadeInfo describes the ReShade build an install used.
type ReShadeInfo struct {
	Version string `json:"version"`
	Flavor  string `json:"flavor"`
	Arch    string `json:"arch"`
	API     string `json:"api"`
	DLL     string `json:"dll"`
}

// Install is one ReShade installation, keyed by the executable it targets.
type Install struct {
	// Exe is the executable's path relative to the game root.
	Exe         string      `json:"exe"`
	InstalledAt time.Time   `json:"installed_at"`
	ReShade     ReShadeInfo `json:"reshade"`
	Packages    []string    `json:"packages,omitempty"`
	Addons      []string    `json:"addons,omitempty"`
	Custom      []string    `json:"custom,omitempty"`
	// RenoDX is the RenoDX mod id, or empty. Optional, so the schema
	// stays at 1: an older yarm reading a newer registry drops it on
	// rewrite, which costs only the wizard's preselection. The file
	// itself stays in Files with its renodx: origin, so uninstall keeps
	// removing it either way.
	RenoDX  string   `json:"renodx,omitempty"`
	Files   []File   `json:"files"`
	Backups []Backup `json:"backups,omitempty"`
}

// Game groups the installs belonging to one game.
type Game struct {
	Name     string    `json:"name"`
	Provider string    `json:"provider"`
	Root     string    `json:"root"`
	Installs []Install `json:"installs"`
}

// Registry is the whole installs.json document.
type Registry struct {
	Schema int             `json:"schema"`
	Games  map[string]Game `json:"games"`
}

// ErrSchemaTooNew is returned when the registry was written by a newer
// YARM.
var ErrSchemaTooNew = errors.New("installs.json schema is newer than this version of yarm")

// Path returns the registry's full path inside dir.
func Path(dir string) string { return filepath.Join(dir, FileName) }

// backupSuffix is the suffix of files yarm displaced and saved beside
// the original. Duplicated from install.BackupSuffix (which cannot be
// imported here without a cycle): the two must stay identical, and the
// uninstaller enforces it again at restore time.
const backupSuffix = ".yarm-bak"

// maxFilesPerInstall bounds one install's file list. Real installs number
// in the hundreds of files; anything past this is hand-crafted, and the
// uninstaller would otherwise walk it entry by entry.
const maxFilesPerInstall = 20000

// Validate rejects registries that could not have come from yarm. Every
// path the uninstaller may delete has to be a clean relative slash-path
// with a known origin, every recorded hash a real SHA-256, every size
// sane, every game root an absolute path — so a hand-edited or tampered
// file fails here, before anything acts on it, rather than relying on
// every consumer to re-check.
func (r Registry) Validate() error {
	for id, g := range r.Games {
		if g.Root == "" || !filepath.IsAbs(g.Root) {
			return fmt.Errorf("game %q: root %q is not an absolute path", id, g.Root)
		}
		for _, in := range g.Installs {
			if err := validRelPath(in.Exe); err != nil {
				return fmt.Errorf("game %q exe %q: %w", id, in.Exe, err)
			}
			if len(in.Files) > maxFilesPerInstall {
				return fmt.Errorf("game %q: %d files, more than the %d yarm ever writes",
					id, len(in.Files), maxFilesPerInstall)
			}
			seen := make(map[string]bool, len(in.Files))
			for _, f := range in.Files {
				if err := validRelPath(f.Path); err != nil {
					return fmt.Errorf("game %q file %q: %w", id, f.Path, err)
				}
				if seen[f.Path] {
					return fmt.Errorf("game %q file %q: listed twice", id, f.Path)
				}
				seen[f.Path] = true
				if err := validOrigin(f.Origin); err != nil {
					return fmt.Errorf("game %q file %q: %w", id, f.Path, err)
				}
				if err := validSHA256(f.SHA256); err != nil {
					return fmt.Errorf("game %q file %q: %w", id, f.Path, err)
				}
				if f.Size < 0 {
					return fmt.Errorf("game %q file %q: negative size %d", id, f.Path, f.Size)
				}
			}
			for _, b := range in.Backups {
				if err := validRelPath(b.Path); err != nil {
					return fmt.Errorf("game %q backup %q: %w", id, b.Path, err)
				}
				if err := validRelPath(b.Backup); err != nil {
					return fmt.Errorf("game %q backup %q: %w", id, b.Backup, err)
				}
				if !strings.HasSuffix(b.Backup, backupSuffix) {
					return fmt.Errorf("game %q backup %q: must end in %q", id, b.Backup, backupSuffix)
				}
				if b.Backup == b.Path {
					return fmt.Errorf("game %q backup %q: backup and original are the same file", id, b.Backup)
				}
				if path.Dir(b.Backup) != path.Dir(b.Path) {
					return fmt.Errorf("game %q backup %q: must sit beside the file it displaced", id, b.Backup)
				}
			}
		}
	}
	return nil
}

// validOrigin reports whether o is an origin yarm records: a fixed
// keyword, or a prefixed id with something after the colon. Anything else
// was not written by yarm.
func validOrigin(o Origin) error {
	switch o {
	case OriginReShade, OriginD3DCompiler, OriginINI, OriginAdopted:
		return nil
	}
	for _, prefix := range []string{"package:", "addon:", "renodx:", "custom:"} {
		if rest, ok := strings.CutPrefix(string(o), prefix); ok && rest != "" {
			return nil
		}
	}
	return fmt.Errorf("unknown origin %q", o)
}

// validRelPath reports whether p is shaped like a path yarm records: a
// relative slash-separated path with no empty, ".", or ".." segments and
// no drive letters or backslashes (which would mean different directories
// on Windows than on Linux).
func validRelPath(p string) error {
	switch {
	case p == "":
		return errors.New("empty path")
	case filepath.IsAbs(p):
		return errors.New("absolute path")
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".", "..":
			return fmt.Errorf("bad segment %q", seg)
		}
		if strings.ContainsAny(seg, `:\`) {
			return fmt.Errorf("bad segment %q", seg)
		}
	}
	return nil
}

// validSHA256 reports whether s looks like a recorded SHA-256: 64 hex
// characters. Everything yarm records comes from a real hash, so
// anything else is hand-written or tampered with.
func validSHA256(s string) error {
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != sha256.Size {
		return fmt.Errorf("not a SHA-256: %q", s)
	}
	return nil
}

// Load reads the registry from dir, returning an empty one when the file
// does not exist yet.
//
// Unlike the cache index, a corrupt registry is an error rather than
// something to start fresh from: it is the only record of which files
// YARM put into the user's game directories, and silently discarding it
// would strand those files with nothing able to remove them. The same
// goes for a registry that fails validation.
func Load(dir string) (Registry, error) {
	raw, err := os.ReadFile(Path(dir))
	if os.IsNotExist(err) {
		return Registry{Schema: SchemaVersion, Games: map[string]Game{}}, nil
	}
	if err != nil {
		return Registry{}, err
	}

	var reg Registry
	if err := json.Unmarshal(raw, &reg); err != nil {
		return Registry{}, fmt.Errorf("parse %s: %w", Path(dir), err)
	}
	if reg.Schema > SchemaVersion {
		return Registry{}, fmt.Errorf("%w: found %d, support %d", ErrSchemaTooNew, reg.Schema, SchemaVersion)
	}
	if reg.Games == nil {
		reg.Games = map[string]Game{}
	}
	reg.Schema = SchemaVersion
	if err := reg.Validate(); err != nil {
		return Registry{}, fmt.Errorf("%s: %w", Path(dir), err)
	}
	return reg, nil
}

// Save writes the registry to dir atomically. It refuses a registry that
// fails validation, so bad in-memory state can never reach the disk.
func Save(dir string, reg Registry) error {
	if err := reg.Validate(); err != nil {
		return err
	}
	reg.Schema = SchemaVersion
	if reg.Games == nil {
		reg.Games = map[string]Game{}
	}
	raw, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(Path(dir), raw, 0o644)
}

// FindInstall returns the install recorded for a game's executable.
func (r Registry) FindInstall(gameID, exe string) (Install, bool) {
	g, ok := r.Games[gameID]
	if !ok {
		return Install{}, false
	}
	for _, in := range g.Installs {
		if in.Exe == exe {
			return in, true
		}
	}
	return Install{}, false
}

// Installs returns every recorded install across all games, with the game
// id each belongs to.
func (r Registry) Installs() []GameInstall {
	var out []GameInstall
	for id, g := range r.Games {
		for _, in := range g.Installs {
			out = append(out, GameInstall{GameID: id, Game: g, Install: in})
		}
	}
	slices.SortFunc(out, func(a, b GameInstall) int {
		if a.GameID != b.GameID {
			if a.GameID < b.GameID {
				return -1
			}
			return 1
		}
		switch {
		case a.Install.Exe < b.Install.Exe:
			return -1
		case a.Install.Exe > b.Install.Exe:
			return 1
		default:
			return 0
		}
	})
	return out
}

// GameInstall pairs an install with the game it belongs to.
type GameInstall struct {
	GameID  string
	Game    Game
	Install Install
}

// Record adds or replaces the install for a game's executable. Passing the
// game's identity alongside keeps the registry self-describing even if the
// game later disappears from its provider.
func (r *Registry) Record(gameID string, g Game, in Install) {
	if r.Games == nil {
		r.Games = map[string]Game{}
	}

	existing, ok := r.Games[gameID]
	if !ok {
		existing = Game{Name: g.Name, Provider: g.Provider, Root: g.Root}
	} else {
		// Refresh identity: a game can move between libraries.
		existing.Name, existing.Provider, existing.Root = g.Name, g.Provider, g.Root
	}

	replaced := false
	for i := range existing.Installs {
		if existing.Installs[i].Exe == in.Exe {
			existing.Installs[i] = in
			replaced = true
			break
		}
	}
	if !replaced {
		existing.Installs = append(existing.Installs, in)
	}

	slices.SortFunc(existing.Installs, func(a, b Install) int {
		switch {
		case a.Exe < b.Exe:
			return -1
		case a.Exe > b.Exe:
			return 1
		default:
			return 0
		}
	})

	r.Games[gameID] = existing
}

// Remove drops the install for a game's executable, and the game itself
// once it has no installs left.
func (r *Registry) Remove(gameID, exe string) bool {
	g, ok := r.Games[gameID]
	if !ok {
		return false
	}

	idx := slices.IndexFunc(g.Installs, func(in Install) bool { return in.Exe == exe })
	if idx < 0 {
		return false
	}

	g.Installs = slices.Delete(g.Installs, idx, idx+1)
	if len(g.Installs) == 0 {
		delete(r.Games, gameID)
		return true
	}
	r.Games[gameID] = g
	return true
}

// OwnedFile reports whether an install claims a game-relative path, and
// with which recorded hash.
func (in Install) OwnedFile(path string) (File, bool) {
	for _, f := range in.Files {
		if f.Path == path {
			return f, true
		}
	}
	return File{}, false
}
