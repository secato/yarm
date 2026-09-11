package install

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/secato/yarm/internal/fsutil"
	"github.com/secato/yarm/internal/state"
)

// Event reports progress while an install runs.
type Event struct {
	Kind        StepKind
	Description string
	// Done and Total count files copied out of files to copy.
	Done  int
	Total int
}

// ProgressFunc receives Events. It may be nil.
type ProgressFunc func(Event)

// Executor applies a Plan to a game directory.
//
// Everything it writes is recorded as it goes, so a failure part-way
// through can be undone precisely: created files are deleted, displaced
// originals are restored, and no manifest is written. A half-installed
// ReShade is worse than none, because the game may then fail to start
// with no record of why.
type Executor struct {
	// StateDir holds installs.json.
	StateDir string
	// Now is overridable for tests.
	Now func() time.Time
}

// NewExecutor returns an Executor writing its manifest into stateDir.
func NewExecutor(stateDir string) *Executor {
	return &Executor{StateDir: stateDir, Now: time.Now}
}

func (e *Executor) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// Result summarizes a completed install.
type Result struct {
	Install state.Install
	// Written lists the game-relative paths actually created or replaced.
	Written []string
	// Skipped lists files left alone because they were already correct or
	// because they were foreign and Overwrite was not given.
	Skipped []string
	// Removed lists files deleted from a previous install.
	Removed []string
	// Bytes is how much was written, taken from the plan: on a run that
	// returns without error, everything the plan meant to write was
	// written. A failed run returns the zero Result, so this is never a
	// partial figure.
	Bytes    int64
	Warnings []string
}

// stagedRemoval is a file an upgrade no longer needs, moved aside rather
// than deleted so a rollback can put it back.
type stagedRemoval struct {
	original string // absolute
	staged   string // absolute
}

// journal tracks what has been changed so it can be undone.
type journal struct {
	created []string // absolute paths written by this run
	backups []state.Backup
	staged  []stagedRemoval
	root    string
}

// Run applies the plan. On any failure after the first change, everything
// this run did is rolled back before the error is returned.
func (e *Executor) Run(ctx context.Context, plan Plan, onProgress ProgressFunc) (Result, error) {
	req := plan.Request
	if err := req.Validate(); err != nil {
		return Result{}, err
	}

	root := req.Game.Root
	jr := &journal{root: root}

	result, err := e.run(ctx, plan, jr, onProgress)
	if err != nil {
		if rbErr := jr.rollback(); rbErr != nil {
			// Report both: the user needs to know the directory may not
			// be clean.
			return Result{}, fmt.Errorf("%w (rollback also failed: %v)", err, rbErr)
		}
		return Result{}, err
	}
	return result, nil
}

func (e *Executor) run(ctx context.Context, plan Plan, jr *journal, onProgress ProgressFunc) (Result, error) {
	req := plan.Request
	root := req.Game.Root
	result := Result{Warnings: plan.Warnings, Bytes: plan.TotalBytes()}

	emit := func(ev Event) {
		if onProgress != nil {
			onProgress(ev)
		}
	}

	// 1. Set aside files a previous install left that this one does not
	//    want. They are moved rather than deleted: if a later step fails,
	//    rollback puts them back, so a failed upgrade does not destroy the
	//    working install it was replacing. They are deleted for real only
	//    once the manifest has been written.
	for _, rel := range plan.Removed {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		abs, err := safeDest(root, rel)
		if err != nil {
			return Result{}, err
		}
		// Regular files only, as at uninstall: the manifest claims yarm
		// wrote this path, and yarm only ever writes regular files. A
		// directory or a symlink standing there now belongs to someone
		// else, and moving it aside would take whatever it holds with it.
		if st, err := os.Lstat(abs); err == nil && !st.Mode().IsRegular() {
			slog.Warn("leaving a previous install's path alone: not a regular file", "path", rel)
			continue
		}
		staged := abs + removedSuffix
		if err := os.Rename(abs, staged); err != nil {
			if os.IsNotExist(err) {
				// Already gone; nothing to restore either.
				result.Removed = append(result.Removed, rel)
				continue
			}
			return Result{}, fmt.Errorf("remove %s: %w", rel, err)
		}
		jr.staged = append(jr.staged, stagedRemoval{original: abs, staged: staged})
		result.Removed = append(result.Removed, rel)
	}
	if len(plan.Removed) > 0 {
		emit(Event{Kind: StepRemove, Description: "removed files from the previous install"})
	}

	// 2. Get foreign files out of the way: saved alongside, or deleted
	//    outright when the request said not to keep a copy.
	for _, f := range plan.Files {
		if f.Action != ActionBackup && f.Action != ActionDiscard {
			continue
		}
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		abs, err := safeDest(root, f.Dest)
		if err != nil {
			return Result{}, err
		}
		if f.Action == ActionDiscard {
			if err := os.Remove(abs); err != nil {
				return Result{}, fmt.Errorf("replace %s: %w", f.Dest, err)
			}
			emit(Event{Kind: StepBackup, Description: "discarded " + f.Dest})
			continue
		}

		backupRel := f.Dest + BackupSuffix
		backupAbs := abs + BackupSuffix
		// An existing backup is an older original — from an install that
		// was interrupted before uninstall could put it back. Renaming
		// over it would destroy the only copy of the file the user
		// actually started with, so the older one wins and the current
		// file is simply replaced.
		if _, err := os.Stat(backupAbs); err == nil {
			if err := os.Remove(abs); err != nil {
				return Result{}, fmt.Errorf("replace %s: %w", f.Dest, err)
			}
		} else if err := os.Rename(abs, backupAbs); err != nil {
			return Result{}, fmt.Errorf("back up %s: %w", f.Dest, err)
		}
		jr.backups = append(jr.backups, state.Backup{Path: f.Dest, Backup: backupRel})
		emit(Event{Kind: StepBackup, Description: "backed up " + f.Dest})
	}

	// 3. Copy everything, hashing as we go so the manifest records what
	//    actually landed rather than what we meant to write.
	total := plan.WriteCount()
	done := 0
	var files []state.File

	for _, f := range plan.Files {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}

		abs, err := safeDest(root, f.Dest)
		if err != nil {
			return Result{}, err
		}

		switch {
		case f.Origin == state.OriginINI:
			// Handled after the copies, so a failed copy does not leave a
			// stray ini behind.
			continue

		case !f.Action.Writes():
			result.Skipped = append(result.Skipped, f.Dest)
			// A skipped file that a previous install owns is still ours:
			// keep it in the manifest so uninstall removes it.
			if f.Action == ActionSkip {
				sum, size, err := statHash(abs)
				if err == nil {
					files = append(files, state.File{
						Path: f.Dest, SHA256: sum, Size: size, Origin: f.Origin,
					})
				}
			}
			continue
		}

		sum, size, err := fsutil.CopyHashed(abs, f.Source)
		if err != nil {
			return Result{}, fmt.Errorf("copy %s: %w", f.Dest, err)
		}
		jr.created = append(jr.created, abs)
		files = append(files, state.File{Path: f.Dest, SHA256: sum, Size: size, Origin: f.Origin})
		result.Written = append(result.Written, f.Dest)

		done++
		emit(Event{Kind: StepCopy, Description: f.Dest, Done: done, Total: total})
	}

	// 4. ReShade.ini, only when the game has none.
	for _, f := range plan.Files {
		if f.Origin != state.OriginINI {
			continue
		}
		abs, err := safeDest(root, f.Dest)
		if err != nil {
			return Result{}, err
		}
		wrote, err := writeINIIfAbsent(abs)
		if err != nil {
			return Result{}, fmt.Errorf("write %s: %w", f.Dest, err)
		}
		switch {
		case wrote:
			jr.created = append(jr.created, abs)
			sum, size, err := statHash(abs)
			if err != nil {
				return Result{}, err
			}
			files = append(files, state.File{Path: f.Dest, SHA256: sum, Size: size, Origin: state.OriginINI})
			result.Written = append(result.Written, f.Dest)
			emit(Event{Kind: StepINI, Description: "wrote " + f.Dest})

		case f.Owned:
			// An earlier install of ours created this ini and we left it
			// alone. Carry its original manifest entry forward, or an
			// upgrade would quietly disown the file and uninstall would
			// leave it behind for good. The recorded hash is the one from
			// when we wrote it, so an ini the user has since edited still
			// reads as edited and survives uninstall.
			files = append(files, f.OwnedRecord)
			result.Skipped = append(result.Skipped, f.Dest)

		default:
			// A pre-existing ini we never created stays the user's.
			result.Skipped = append(result.Skipped, f.Dest)
		}
		break
	}

	// 5. Record the manifest last: until it is written, nothing claims
	//    these files, and a failure above leaves the directory as found.
	in := state.Install{
		Exe:         filepath.ToSlash(req.Exe.Path),
		InstalledAt: e.now().UTC(),
		ReShade: state.ReShadeInfo{
			Version: req.Version,
			Flavor:  string(req.Flavor),
			Arch:    string(req.Exe.Arch),
			API:     string(req.Exe.API),
			DLL:     req.DLLName,
		},
		Packages: req.Packages,
		Addons:   req.Addons,
		Custom:   req.Custom,
		RenoDX:   req.RenoDX,
		Files:    files,
		Backups:  jr.backups,
	}

	reg, err := state.Load(e.StateDir)
	if err != nil {
		return Result{}, err
	}
	reg.Record(req.Game.ID, state.Game{
		Name:     req.Game.Name,
		Provider: req.Game.Provider,
		Root:     root,
	}, in)
	if err := state.Save(e.StateDir, reg); err != nil {
		return Result{}, fmt.Errorf("write manifest: %w", err)
	}
	emit(Event{Kind: StepManifest, Description: "recorded install"})

	// 6. The upgrade is now committed, so the files it superseded can go
	//    for good. A failure here is not worth undoing the install over;
	//    it leaves recoverable clutter, which is logged.
	jr.discardStaged()

	// Prune directories the removals emptied, so a dropped package does
	// not leave an empty reshade-shaders tree behind.
	pruneEmptyDirs(root, result.Removed)

	result.Install = in
	return result, nil
}

// rollback undoes everything the run changed, best effort, collecting any
// failures rather than stopping at the first.
func (j *journal) rollback() error {
	var errs []error

	for _, abs := range j.created {
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove %s: %w", abs, err))
		}
	}

	for _, b := range j.backups {
		dst := filepath.Join(j.root, filepath.FromSlash(b.Path))
		src := filepath.Join(j.root, filepath.FromSlash(b.Backup))
		if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("restore %s: %w", b.Path, err))
		}
	}

	// Put back anything an upgrade had set aside, so a failed upgrade
	// leaves the previous install intact.
	for _, sr := range j.staged {
		if err := os.Rename(sr.staged, sr.original); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("restore %s: %w", sr.original, err))
		}
	}

	if len(errs) > 0 {
		slog.Error("rollback incomplete", "errors", errors.Join(errs...))
	}
	return errors.Join(errs...)
}

// discardStaged deletes the files an upgrade superseded, once the new
// install is committed.
func (j *journal) discardStaged() {
	for _, sr := range j.staged {
		if err := os.Remove(sr.staged); err != nil && !os.IsNotExist(err) {
			slog.Warn("could not delete superseded file", "path", sr.staged, "error", err)
		}
	}
}

// pruneEmptyDirs removes directories left empty by the given
// game-relative paths, deepest first, stopping at the game root. Removal
// itself is the guard: os.Remove only removes empty directories, so a
// folder holding the game's own files is never touched. Callers only pass
// paths that passed the uninstaller's shape checks, so nothing outside
// yarm's own layout can nominate a directory here.
func pruneEmptyDirs(root string, rels []string) {
	seen := make(map[string]bool, len(rels))
	var dirs []string
	for _, rel := range rels {
		d := path.Dir(rel)
		for d != "." && d != "/" && d != "" {
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
			d = path.Dir(d)
		}
	}

	// Deepest first, so a parent is only tried after its children.
	sort.Slice(dirs, func(i, j int) bool {
		return strings.Count(dirs[i], "/") > strings.Count(dirs[j], "/")
	})

	for _, d := range dirs {
		abs, err := safeDest(root, d)
		if err != nil {
			continue
		}
		// Remove fails harmlessly when the directory is not empty.
		_ = os.Remove(abs)
	}
}

// safeDest resolves a game-relative destination, refusing anything that
// would escape the game root. Destinations are built from catalog
// content, so this is enforced rather than assumed.
func safeDest(root, rel string) (string, error) {
	clean := filepath.FromSlash(rel)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("destination %q must be relative to the game root", rel)
	}

	abs := filepath.Join(root, clean)
	back, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	if back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("destination %q escapes the game root", rel)
	}
	return abs, nil
}

// statHash returns a file's hash and size.
func statHash(path string) (string, int64, error) {
	sum, err := hashFile(path)
	if err != nil {
		return "", 0, err
	}
	size, err := fileSize(path)
	if err != nil {
		return "", 0, err
	}
	return sum, size, nil
}
