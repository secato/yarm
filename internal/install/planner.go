package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/state"
)

// Artifacts are the cached directories an install draws from. The planner
// receives them already resolved so it does no network I/O of its own.
type Artifacts struct {
	// ReShadeDir holds ReShade32.dll and ReShade64.dll.
	ReShadeDir string
	// D3DCompiler is the path to a verified d3dcompiler_47.dll, or "".
	D3DCompiler string
	// Packages maps a package id to its normalized cache directory.
	Packages map[string]string
	// Addons maps an add-on id to a directory of .addon32/.addon64 files.
	Addons map[string]string
	// Custom maps a custom content id to its folder under cache/custom.
	Custom map[string]string
}

// Planner turns a Request plus resolved Artifacts into a Plan.
//
// Planning performs no downloads and no writes: it only stats the game
// directory to decide what each file's action would be. That keeps the
// review screen honest and lets the whole decision table be tested against
// temporary directories.
type Planner struct {
	// Registry is the current install state, used to tell files YARM owns
	// from files it does not.
	Registry state.Registry
}

// ErrMissingArtifact reports a request naming content that was not
// resolved into Artifacts.
var ErrMissingArtifact = errors.New("required artifact is missing")

// Plan computes the full file set and the action for each file.
func (p Planner) Plan(req Request, art Artifacts) (Plan, error) {
	if err := req.Validate(); err != nil {
		return Plan{}, err
	}

	plan := Plan{Request: req}
	exeDir := req.ExeDir()

	files, err := p.collect(req, art, exeDir)
	if err != nil {
		return Plan{}, err
	}

	prev, hasPrev := p.Registry.FindInstall(req.Game.ID, filepath.ToSlash(req.Exe.Path))
	plan.Upgrade = hasPrev

	// Decide each file's action against what is on disk and what a
	// previous install claims.
	for i := range files {
		if hasPrev {
			files[i].OwnedRecord, files[i].Owned = prev.OwnedFile(files[i].Dest)
		}
		action, warn, err := p.classify(req, prev, hasPrev, files[i])
		if err != nil {
			return Plan{}, err
		}
		files[i].Action = action
		if warn != "" {
			plan.Warnings = append(plan.Warnings, warn)
		}
	}
	plan.Files = files

	// An upgrade removes files the previous install created that this one
	// no longer produces, so a dropped package does not leave shaders
	// behind.
	if hasPrev {
		wanted := make(map[string]bool, len(files))
		for _, f := range files {
			wanted[f.Dest] = true
		}
		for _, old := range prev.Files {
			if !wanted[old.Path] {
				plan.Removed = append(plan.Removed, old.Path)
			}
		}
		slices.Sort(plan.Removed)
	}

	plan.Steps = buildSteps(plan)
	return plan, nil
}

// collect builds the full list of files the install would place, before
// any conflict analysis.
func (p Planner) collect(req Request, art Artifacts, exeDir string) ([]PlannedFile, error) {
	var files []PlannedFile

	// The ReShade proxy DLL, named for the API the game uses.
	if art.ReShadeDir == "" {
		return nil, fmt.Errorf("%w: reshade %s", ErrMissingArtifact, req.Version)
	}
	dll := artifacts.DLLFor(req.Exe.Arch != game.ArchX86)
	src := filepath.Join(art.ReShadeDir, dll)
	size, err := fileSize(src)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrMissingArtifact, src)
	}
	files = append(files, PlannedFile{
		Source: src,
		Dest:   join(exeDir, req.DLLName),
		Origin: state.OriginReShade,
		Size:   size,
	})

	// d3dcompiler_47.dll, on Linux only.
	if artifacts.NeedsD3DCompiler(req.TargetOS) {
		if art.D3DCompiler == "" {
			return nil, fmt.Errorf("%w: %s", ErrMissingArtifact, artifacts.D3DCompiler)
		}
		size, err := fileSize(art.D3DCompiler)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrMissingArtifact, art.D3DCompiler)
		}
		files = append(files, PlannedFile{
			Source: art.D3DCompiler,
			Dest:   join(exeDir, artifacts.D3DCompiler),
			Origin: state.OriginD3DCompiler,
			Size:   size,
		})
	}

	// Effect packages, filtered by the rules recorded at extraction time.
	for _, id := range req.Packages {
		dir, ok := art.Packages[id]
		if !ok {
			return nil, fmt.Errorf("%w: package %s", ErrMissingArtifact, id)
		}
		pkgFiles, err := packageFiles(dir, exeDir, state.PackageOrigin(id))
		if err != nil {
			return nil, fmt.Errorf("package %s: %w", id, err)
		}
		files = append(files, pkgFiles...)
	}

	// Custom shader folders follow the same layout as a package but carry
	// no filter: the user put them there deliberately.
	for _, id := range req.Custom {
		dir, ok := art.Custom[id]
		if !ok {
			return nil, fmt.Errorf("%w: custom %s", ErrMissingArtifact, id)
		}
		customFiles, err := customContentFiles(dir, exeDir, state.CustomOrigin(id))
		if err != nil {
			return nil, fmt.Errorf("custom %s: %w", id, err)
		}
		files = append(files, customFiles...)
	}

	// Add-ons sit beside the ReShade DLL; ReShade scans the exe directory.
	for _, id := range req.Addons {
		dir, ok := art.Addons[id]
		if !ok {
			return nil, fmt.Errorf("%w: addon %s", ErrMissingArtifact, id)
		}
		addonFiles, err := addonFiles(dir, exeDir, state.AddonOrigin(id))
		if err != nil {
			return nil, fmt.Errorf("addon %s: %w", id, err)
		}
		files = append(files, addonFiles...)
	}

	// ReShade.ini is generated rather than copied.
	files = append(files, PlannedFile{
		Dest:   join(exeDir, ININame),
		Origin: state.OriginINI,
		Size:   int64(len(DefaultINI())),
	})

	if err := checkDuplicates(files); err != nil {
		return nil, err
	}
	return files, nil
}

// classify decides what to do about one planned file, given what is on
// disk and what a previous install claims.
func (p Planner) classify(req Request, prev state.Install, hasPrev bool, f PlannedFile) (Action, string, error) {
	dst := filepath.Join(req.Game.Root, filepath.FromSlash(f.Dest))

	info, err := os.Stat(dst)
	if os.IsNotExist(err) {
		return ActionCreate, "", nil
	}
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return "", "", fmt.Errorf("%s is a directory, cannot write a file there", f.Dest)
	}

	// ReShade.ini is the user's configuration once written: it is never
	// overwritten, only created when absent (§5.2).
	if f.Origin == state.OriginINI {
		return ActionSkip, "", nil
	}

	// Does a previous install of ours claim this file, unmodified?
	if hasPrev {
		if owned, ok := prev.OwnedFile(f.Dest); ok {
			sum, err := hashFile(dst)
			if err != nil {
				return "", "", err
			}
			if sum == owned.SHA256 {
				// Identical content already installed: nothing to do.
				if same, err := sameContent(f.Source, sum); err == nil && same {
					return ActionSkip, "", nil
				}
				return ActionReplace, "", nil
			}
			// Ours by the manifest, but the user has since edited it.
			return ActionReplace, fmt.Sprintf(
				"%s was modified after installation and will be replaced", f.Dest), nil
		}
	}

	// A file we do not own. Only touch it with explicit permission.
	if !req.Overwrite {
		return ActionKeep, fmt.Sprintf(
			"%s already exists and is not managed by yarm; it will be kept", f.Dest), nil
	}
	if req.NoBackup {
		return ActionDiscard, fmt.Sprintf(
			"%s will be replaced and the original discarded; uninstall cannot put it back",
			f.Dest), nil
	}
	return ActionBackup, fmt.Sprintf(
		"%s will be replaced; the original is saved as %s",
		f.Dest, f.Dest+BackupSuffix), nil
}

// packageFiles enumerates the installable files of a normalized package
// cache entry, applying the EffectFiles / DenyEffectFiles rules recorded
// in package.json.
func packageFiles(dir, exeDir string, origin state.Origin) ([]PlannedFile, error) {
	meta, err := artifacts.ReadPackageMeta(dir)
	if err != nil {
		return nil, err
	}

	allow := make(map[string]bool, len(meta.EffectFiles))
	for _, n := range meta.EffectFiles {
		allow[strings.ToLower(n)] = true
	}
	deny := make(map[string]bool, len(meta.DenyEffectFiles))
	for _, n := range meta.DenyEffectFiles {
		deny[strings.ToLower(n)] = true
	}

	var out []PlannedFile
	err = walkFiles(dir, func(rel string, size int64) error {
		top, rest, ok := strings.Cut(rel, "/")
		if !ok {
			return nil // package.json and anything else at the root
		}

		switch top {
		case artifacts.ShadersDir:
			base := strings.ToLower(path.Base(rest))
			if deny[base] {
				return nil
			}
			// A filter names .fx effects; .fxh headers are always needed,
			// since the listed effects include them.
			if len(allow) > 0 && strings.HasSuffix(base, ".fx") && !allow[base] {
				return nil
			}
			out = append(out, PlannedFile{
				Source: filepath.Join(dir, filepath.FromSlash(rel)),
				Dest:   join(exeDir, ShadersDir+"/"+rest),
				Origin: origin,
				Size:   size,
			})
		case artifacts.TexturesDir:
			// Textures are installed whole (§4.2).
			out = append(out, PlannedFile{
				Source: filepath.Join(dir, filepath.FromSlash(rel)),
				Dest:   join(exeDir, TexturesDir+"/"+rest),
				Origin: origin,
				Size:   size,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// customContentFiles enumerates a user-managed shader folder. It accepts
// both the documented Shaders/ + Textures/ layout and loose .fx files.
func customContentFiles(dir, exeDir string, origin state.Origin) ([]PlannedFile, error) {
	var out []PlannedFile

	err := walkFiles(dir, func(rel string, size int64) error {
		src := filepath.Join(dir, filepath.FromSlash(rel))

		top, rest, nested := strings.Cut(rel, "/")
		switch {
		case nested && top == artifacts.ShadersDir:
			out = append(out, PlannedFile{Source: src, Dest: join(exeDir, ShadersDir+"/"+rest), Origin: origin, Size: size})
		case nested && top == artifacts.TexturesDir:
			out = append(out, PlannedFile{Source: src, Dest: join(exeDir, TexturesDir+"/"+rest), Origin: origin, Size: size})
		case !nested && isShaderName(rel):
			out = append(out, PlannedFile{Source: src, Dest: join(exeDir, ShadersDir+"/"+rel), Origin: origin, Size: size})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// addonFiles enumerates the .addon32/.addon64 binaries in a cached add-on
// directory. They go directly beside the ReShade DLL.
func addonFiles(dir, exeDir string, origin state.Origin) ([]PlannedFile, error) {
	var out []PlannedFile

	err := walkFiles(dir, func(rel string, size int64) error {
		switch strings.ToLower(filepath.Ext(rel)) {
		case artifacts.ExtAddon32, artifacts.ExtAddon64:
			out = append(out, PlannedFile{
				Source: filepath.Join(dir, filepath.FromSlash(rel)),
				Dest:   join(exeDir, path.Base(rel)),
				Origin: origin,
				Size:   size,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// buildSteps summarizes a plan as the ordered work the executor performs.
func buildSteps(p Plan) []Step {
	var steps []Step

	if n := len(p.Removed); n > 0 {
		steps = append(steps, Step{
			Kind:        StepRemove,
			Description: fmt.Sprintf("remove %d file(s) from the previous install", n),
		})
	}

	backups, discards := 0, 0
	for _, f := range p.Files {
		switch f.Action {
		case ActionBackup:
			backups++
		case ActionDiscard:
			discards++
		}
	}
	if backups > 0 {
		steps = append(steps, Step{
			Kind:        StepBackup,
			Description: fmt.Sprintf("back up %d existing file(s)", backups),
		})
	}
	if discards > 0 {
		steps = append(steps, Step{
			Kind:        StepBackup,
			Description: fmt.Sprintf("replace %d existing file(s), keeping no copy", discards),
		})
	}

	if n := p.WriteCount(); n > 0 {
		steps = append(steps, Step{
			Kind:        StepCopy,
			Description: fmt.Sprintf("copy %d file(s)", n),
		})
	}

	for _, f := range p.Files {
		if f.Origin == state.OriginINI && f.Action == ActionCreate {
			steps = append(steps, Step{Kind: StepINI, Description: "write " + ININame})
			break
		}
	}

	steps = append(steps, Step{Kind: StepManifest, Description: "record what was installed"})
	return steps
}

// checkDuplicates rejects a plan in which two sources target the same
// file. Two packages shipping the same shader would otherwise race, and
// the manifest could record a hash that does not match what landed.
func checkDuplicates(files []PlannedFile) error {
	seen := make(map[string]state.Origin, len(files))
	for _, f := range files {
		if prev, ok := seen[f.Dest]; ok {
			return fmt.Errorf("%s would be written by both %s and %s", f.Dest, prev, f.Origin)
		}
		seen[f.Dest] = f.Origin
	}
	return nil
}

// walkFiles calls fn for every regular file under root, with a
// slash-separated path relative to root.
func walkFiles(root string, fn func(rel string, size int64) error) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return fn(filepath.ToSlash(rel), info.Size())
	})
}

// join composes a game-relative destination path.
func join(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// isShaderName reports whether a file is a ReShade effect or header.
func isShaderName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".fx", ".fxh":
		return true
	default:
		return false
	}
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

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

// sameContent reports whether a source file hashes to sum. Generated
// files (no source) never match, so they are rewritten.
func sameContent(source, sum string) (bool, error) {
	if source == "" {
		return false, nil
	}
	got, err := hashFile(source)
	if err != nil {
		return false, err
	}
	return got == sum, nil
}
