package app

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/fetch"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/state"
)

// ProgressUpdate is one line of narrated progress during an install: a
// download starting or advancing, a file being copied, or a bookkeeping
// step. Total<=0 means the size is not known yet.
type ProgressUpdate struct {
	Label       string
	Done, Total int64
}

// folderOp is one folder's share of an Apply. Install and Uninstall are
// mutually exclusive; Dir is carried alongside so progress lines and the
// result screen can name which folder they are talking about without
// re-deriving it from a request.
//
// A single Apply can hold several of these because the folder is one of
// the wizard's answers: a game with a 32- and a 64-bit tree can be set up
// in one pass, and moving an install from one folder to another is an
// install and an uninstall submitted together.
type folderOp struct {
	Dir       string
	Install   *install.Request
	Uninstall *install.UninstallRequest
}

// verb names what an op does, for progress and the result screen.
func (o folderOp) verb() string {
	if o.Uninstall != nil {
		return "uninstalled"
	}
	return "installed"
}

// failVerb is verb's infinitive, for a title like "install failed".
func (o folderOp) failVerb() string {
	if o.Uninstall != nil {
		return "uninstall"
	}
	return "install"
}

// folderOutcome is what one folderOp actually did.
//
// Attempted separates "ran and failed" from "never started", which is the
// distinction that matters after a batch stops early: the folders behind
// the failure are untouched, and saying so is the difference between a
// useful report and a worrying one.
type folderOutcome struct {
	Op        folderOp
	Attempted bool
	Result    install.Result
	Removed   install.UninstallResult
	Err       error
}

// Installer resolves a Request's artifacts (downloading anything missing)
// and installs them, narrating its progress. An interface so the progress
// screen can be tested against a controllable fake instead of the real
// network and disk.
type Installer interface {
	Install(ctx context.Context, req install.Request, send func(ProgressUpdate)) (install.Result, error)
}

// RealInstaller wires the cache, the catalog and the install engine
// together. It exists in internal/app, not internal/install, because
// combining those packages is exactly the kind of glue the app layer is
// for: nothing imports app, so app is the one thing that sits above
// install, catalog and cache alike. cmd/yarm's `install` command builds the same pipeline for
// the CLI; the two are intentionally not shared, since each belongs to
// its own top-layer entry point.
type RealInstaller struct {
	Cache   *cache.Cache
	Catalog *catalog.Client
	// WizardData is the memoized catalog load the wizard showed its lists
	// from. Resolving from it rather than re-reading the catalogs keeps an
	// install identical to what was on screen when it was confirmed — and
	// skips a full re-parse per install. Nil (or an unloadable memo) falls
	// back to the catalog client.
	WizardData WizardDataLoader
	CustomDir  string
	StateDir   string
	// GamesCache is cleared whenever an install lands: the games list
	// reloads from it after every apply, and must see the new install.
	Games *GamesCache
}

// Install implements Installer.
func (r *RealInstaller) Install(ctx context.Context, req install.Request, send func(ProgressUpdate)) (install.Result, error) {
	reg, err := state.Load(r.StateDir)
	if err != nil {
		return install.Result{}, err
	}
	needed := install.NeededArtifacts(req, findPrevInstall(reg, req.Game.ID, filepath.ToSlash(req.Exe.Path)))

	art, err := r.resolve(ctx, &req, send, needed)
	if err != nil {
		return install.Result{}, err
	}

	send(ProgressUpdate{Label: "Planning install"})
	plan, err := install.Planner{Registry: reg}.Plan(req, art)
	if err != nil {
		return install.Result{}, err
	}

	result, err := install.NewExecutor(r.StateDir).Run(ctx, plan, func(ev install.Event) {
		send(ProgressUpdate{Label: describeEvent(ev), Done: int64(ev.Done), Total: int64(ev.Total)})
	})
	if err != nil {
		return install.Result{}, err
	}
	r.Games.Clear()
	return result, nil
}

// findPrevInstall adapts Registry.FindInstall's (value, ok) result to the
// nil-or-pointer NeededArtifacts expects. Getting this wrong is not
// cosmetic: passing a non-nil pointer to a zero-value Install for a fresh
// install makes NeededArtifacts diff req against an empty manifest instead
// of taking its "nothing recorded yet, need everything" path — every group
// with no prior file to compare against (d3dcompiler_47.dll on Linux,
// wherever nothing was ever written for it) then reads as unchanged and is
// silently skipped, and the plan fails downstream with ErrMissingArtifact
// instead of ever downloading it.
func findPrevInstall(reg state.Registry, gameID, exe string) *state.Install {
	prev, ok := reg.FindInstall(gameID, exe)
	if !ok {
		return nil
	}
	return &prev
}

// resolve downloads and locates the artifact groups needed says this
// request can actually write through, mirroring cmd/yarm's
// resolveArtifacts. req.Packages/Addons are already canonical catalog ids
// here (selected straight from a list the wizard showed), so unlike the
// CLI path there is no alias resolution to do.
//
// Groups the needed set excludes are resolved from the previous
// manifest by the planner itself: their installed files are unchanged,
// so no source is read and nothing is downloaded — an edit that only
// removes an add-on fetches nothing at all, and runs offline.
func (r *RealInstaller) resolve(ctx context.Context, req *install.Request, send func(ProgressUpdate), needed install.Needed) (install.Artifacts, error) {
	art := install.Artifacts{
		Packages: map[string]string{},
		Addons:   map[string]string{},
		Custom:   map[string]string{},
	}

	progress := func(label string) fetch.ProgressFunc {
		return func(p fetch.Progress) {
			send(ProgressUpdate{Label: label, Done: p.Downloaded, Total: p.Total})
		}
	}

	if needed.ReShade {
		flavorWord := "normal"
		if req.Flavor.Addon() {
			flavorWord = "addon"
		}
		send(ProgressUpdate{Label: fmt.Sprintf("Downloading ReShade %s (%s)", req.Version, flavorWord)})
		reshadeDir, err := r.Cache.EnsureReShade(ctx, req.Version, req.Flavor.Addon(),
			progress(fmt.Sprintf("ReShade %s", req.Version)))
		if err != nil {
			return install.Artifacts{}, fmt.Errorf("reshade %s: %w", req.Version, err)
		}
		art.ReShadeDir = reshadeDir
	}

	if needed.D3DCompiler {
		send(ProgressUpdate{Label: "Downloading d3dcompiler_47.dll"})
		dll, err := r.Cache.EnsureD3DCompiler(ctx, req.Exe.Arch, progress("d3dcompiler_47.dll"))
		if err != nil {
			return install.Artifacts{}, fmt.Errorf("d3dcompiler: %w", err)
		}
		art.D3DCompiler = dll
	}

	if len(req.Packages) > 0 {
		byID, err := r.packageLookup(ctx, needed.Packages)
		if err != nil {
			return install.Artifacts{}, err
		}
		for _, id := range req.Packages {
			if !needed.Packages[id] {
				continue
			}
			p, ok := byID[id]
			if !ok {
				return install.Artifacts{}, fmt.Errorf("unknown package %q", id)
			}
			send(ProgressUpdate{Label: "Downloading package " + p.Name})
			dir, err := r.Cache.EnsurePackage(ctx, p, progress(p.Name))
			if err != nil {
				return install.Artifacts{}, fmt.Errorf("package %s: %w", id, err)
			}
			art.Packages[id] = dir
		}
	}

	if len(req.Addons) > 0 {
		var addonsByID map[string]catalog.Addon
		var renoMods []catalog.RenoMod
		if slices.ContainsFunc(req.Addons, func(id string) bool { return needed.Addons[id] }) {
			addons, err := r.addons(ctx)
			if err != nil {
				return install.Artifacts{}, err
			}
			addonsByID = make(map[string]catalog.Addon, len(addons))
			for _, a := range addons {
				addonsByID[a.ID] = a
			}
		}
		// RenoDX's utility mods (FPS Limiter, DLSS Fix) are not in
		// crosire's catalog at all — they are RenoDX's own, fetched the
		// same way req.RenoDX is below — so the RenoDX list is only
		// loaded lazily, the first time an id misses byID.
		for _, id := range req.Addons {
			if !needed.Addons[id] {
				continue
			}
			if a, ok := addonsByID[id]; ok {
				if !a.Installable() {
					return install.Artifacts{}, fmt.Errorf("add-on %q is manual only; see %s", id, a.RepositoryURL)
				}
				send(ProgressUpdate{Label: "Downloading add-on " + a.Name})
				dir, err := r.Cache.EnsureAddon(ctx, a, req.Exe.Arch, progress(a.Name))
				if err != nil {
					return install.Artifacts{}, fmt.Errorf("addon %s: %w", id, err)
				}
				art.Addons[id] = dir
				continue
			}

			if renoMods == nil {
				var err error
				renoMods, err = r.renoDX(ctx)
				if err != nil {
					return install.Artifacts{}, err
				}
			}
			i := slices.IndexFunc(renoMods, func(m catalog.RenoMod) bool { return m.Utility && m.ID == id })
			if i < 0 {
				return install.Artifacts{}, fmt.Errorf("unknown add-on %q", id)
			}
			mod := renoMods[i]
			if _, ok := mod.ArtifactFor(req.Exe.Arch); !ok {
				return install.Artifacts{}, fmt.Errorf("add-on %q has no %s build for this game", id, req.Exe.Arch)
			}
			send(ProgressUpdate{Label: "Downloading " + mod.Title})
			dir, err := r.Cache.EnsureRenoDX(ctx, mod, req.Exe.Arch, progress(mod.Title))
			if err != nil {
				return install.Artifacts{}, fmt.Errorf("addon %s: %w", id, err)
			}
			art.Addons[id] = dir
		}
	}

	if needed.RenoDX {
		mods, err := r.renoDX(ctx)
		if err != nil {
			return install.Artifacts{}, err
		}
		i := slices.IndexFunc(mods, func(m catalog.RenoMod) bool { return m.ID == req.RenoDX })
		if i < 0 {
			return install.Artifacts{}, fmt.Errorf("unknown RenoDX mod %q", req.RenoDX)
		}
		mod := mods[i]
		if _, ok := mod.ArtifactFor(req.Exe.Arch); !ok {
			return install.Artifacts{}, fmt.Errorf(
				"RenoDX %s has no %s build for this game", mod.Title, req.Exe.Arch)
		}
		send(ProgressUpdate{Label: "Downloading RenoDX " + mod.Title})
		dir, err := r.Cache.EnsureRenoDX(ctx, mod, req.Exe.Arch, progress("RenoDX "+mod.Title))
		if err != nil {
			return install.Artifacts{}, fmt.Errorf("renodx %s: %w", mod.ID, err)
		}
		art.RenoDX = dir
	}

	if len(req.Custom) > 0 {
		found, err := r.custom()
		if err != nil {
			return install.Artifacts{}, err
		}
		byID := make(map[string]string, len(found))
		for _, c := range found {
			byID[c.ID] = c.Path
		}
		for _, id := range req.Custom {
			path, ok := byID[id]
			if !ok {
				return install.Artifacts{}, fmt.Errorf("unknown custom content %q", id)
			}
			art.Custom[id] = path
		}
	}

	return art, nil
}

// catalogData is one resolve's view of the catalogs: the memoized wizard
// load when it has one (what the user confirmed against), the catalog
// client otherwise.
func (r *RealInstaller) catalogData(ctx context.Context) (WizardData, bool) {
	if r.WizardData == nil {
		return WizardData{}, false
	}
	data, err := r.WizardData.LoadWizardData(ctx)
	if err != nil || data.empty() {
		return WizardData{}, false
	}
	return data, true
}

func (r *RealInstaller) packages(ctx context.Context) ([]catalog.Package, error) {
	if data, ok := r.catalogData(ctx); ok {
		return data.Packages, nil
	}
	return r.Catalog.Packages(ctx)
}

// packageLookup loads the catalog only when at least one needed package
// actually has to be resolved; a lookup that skips the network entirely
// is what makes an unchanged edit work offline.
func (r *RealInstaller) packageLookup(ctx context.Context, needed map[string]bool) (map[string]catalog.Package, error) {
	any := false
	for _, want := range needed {
		if want {
			any = true
			break
		}
	}
	if !any {
		return map[string]catalog.Package{}, nil
	}
	packages, err := r.packages(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]catalog.Package, len(packages))
	for _, p := range packages {
		byID[p.ID] = p
	}
	return byID, nil
}

func (r *RealInstaller) addons(ctx context.Context) ([]catalog.Addon, error) {
	if data, ok := r.catalogData(ctx); ok {
		return data.Addons, nil
	}
	return r.Catalog.Addons(ctx)
}

func (r *RealInstaller) renoDX(ctx context.Context) ([]catalog.RenoMod, error) {
	// An empty memo list is not an answer: the wizard tolerates a failed
	// RenoDX load (it is a different project's host), so fall through to
	// the client, which may have recovered since.
	if data, ok := r.catalogData(ctx); ok && len(data.RenoDX) > 0 {
		return data.RenoDX, nil
	}
	return r.Catalog.RenoDX(ctx)
}

func (r *RealInstaller) custom() ([]catalog.Custom, error) {
	if r.WizardData != nil {
		if data, err := r.WizardData.LoadWizardData(context.Background()); err == nil && !data.empty() {
			out := make([]catalog.Custom, 0, len(data.CustomShaders)+len(data.CustomAddons))
			out = append(out, data.CustomShaders...)
			out = append(out, data.CustomAddons...)
			return out, nil
		}
	}
	return catalog.ScanCustom(r.CustomDir)
}

// describeEvent turns an install.Event into a progress line.
func describeEvent(ev install.Event) string {
	switch ev.Kind {
	case install.StepRemove:
		return "Removing files from the previous install"
	case install.StepBackup:
		return ev.Description
	case install.StepCopy:
		return ev.Description
	case install.StepINI:
		return ev.Description
	case install.StepManifest:
		return "Recording the install"
	default:
		return ev.Description
	}
}

// UninstallRunner removes a recorded install. An interface so the confirm
// flow can be tested against a fake instead of touching real files.
type UninstallRunner interface {
	Uninstall(req install.UninstallRequest) (install.UninstallResult, error)
}

// RealUninstaller wraps install.Uninstaller.
type RealUninstaller struct {
	StateDir string
	// GamesCache is cleared whenever an uninstall lands, for the same
	// reason as RealInstaller.Games.
	Games *GamesCache
}

// Uninstall implements UninstallRunner.
func (r RealUninstaller) Uninstall(req install.UninstallRequest) (install.UninstallResult, error) {
	res, err := install.NewUninstaller(r.StateDir).Run(req)
	if err != nil {
		return install.UninstallResult{}, err
	}
	r.Games.Clear()
	return res, nil
}

// AdoptRunner records an unmanaged install as yarm-tracked. An interface so
// the confirm flow can be tested against a fake instead of touching real
// files.
type AdoptRunner interface {
	Adopt(g game.Game, exe game.Executable, candidate install.AdoptCandidate) (state.Install, error)
}

// RealAdopter wraps install.Adopt.
type RealAdopter struct {
	StateDir string
	// GamesCache is cleared whenever an adopt lands, for the same reason
	// as RealInstaller.Games.
	Games *GamesCache
}

// Adopt implements AdoptRunner.
func (r RealAdopter) Adopt(g game.Game, exe game.Executable, candidate install.AdoptCandidate) (state.Install, error) {
	in, err := install.Adopt(r.StateDir, g, exe, candidate)
	if err != nil {
		return state.Install{}, err
	}
	r.Games.Clear()
	return in, nil
}
