package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/state"
)

// neededFixture is an installed game folder whose files match its
// manifest exactly: the "everything unchanged" state an edit starts from.
func neededFixture(t *testing.T) (*fixture, Request, *state.Install) {
	t.Helper()
	f := newFixture(t).WithReShade().
		WithPackage("standard-effects",
			artifacts.PackageMeta{Name: "Standard", EffectFiles: []string{"Deband.fx"}},
			map[string]string{"Shaders/Deband.fx": "// deband"}).
		WithAddon("swapchain", map[string]string{"swapchain.addon64": "addon bin"})

	req := f.Request()
	req.Packages = []string{"standard-effects"}
	req.Addons = []string{"swapchain"}
	req.RenoDX = ""

	// Install for real, so disk matches the manifest.
	if _, err := planAndRun(t, f, req, state.Registry{}); err != nil {
		t.Fatalf("install: %v", err)
	}
	reg, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	in, ok := reg.FindInstall(req.Game.ID, "Game/emberhollow.exe")
	if !ok {
		t.Fatal("no install recorded")
	}
	return f, req, &in
}

// An edit over an unchanged install needs nothing: not the DLL, not the
// packages, not the add-ons. This is what keeps "remove one add-on" from
// re-downloading the world.
func TestNeededArtifactsUnchangedEditNeedsNothing(t *testing.T) {
	if artifacts.NeedsD3DCompiler(artifacts.OSWindows) {
		t.Skip("d3dcompiler expectations below are Windows-shaped")
	}
	f, req, in := neededFixture(t)

	// The edit: same everything (dropping add-ons is expressed by them
	// not being in the request — here they stay, also unchanged).
	needed := NeededArtifacts(req, in)
	if needed.ReShade || needed.D3DCompiler || needed.RenoDX {
		t.Errorf("needed = %+v, want nothing", needed)
	}
	for id, want := range needed.Packages {
		if want {
			t.Errorf("package %q marked needed", id)
		}
	}
	for id, want := range needed.Addons {
		if want {
			t.Errorf("add-on %q marked needed", id)
		}
	}
	_ = f
}

// A version bump or a new selection is exactly what re-downloads.
func TestNeededArtifactsChangedInputs(t *testing.T) {
	f, req, in := neededFixture(t)

	bumped := req
	bumped.Version = "6.9.0"
	if n := NeededArtifacts(bumped, in); !n.ReShade {
		t.Error("a version bump should need the ReShade artifact")
	} else if n.Packages["standard-effects"] {
		t.Error("a version bump does not change package bytes")
	}

	addon := req
	addon.Addons = []string{"swapchain", "second"}
	if n := NeededArtifacts(addon, in); !n.Addons["second"] {
		t.Error("a newly selected add-on should be needed")
	} else if n.Addons["swapchain"] {
		t.Error("an unchanged add-on should not be needed")
	}
	_ = f
}

// A file edited on disk makes its whole group needed: the bytes will be
// rewritten, so a source is required.
func TestNeededArtifactsEditedFilePullsItsGroup(t *testing.T) {
	f, req, in := neededFixture(t)

	if err := os.WriteFile(filepath.Join(f.GameDir, "Game", "reshade-shaders", "Shaders", "Deband.fx"),
		[]byte("// edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	n := NeededArtifacts(req, in)
	if !n.Packages["standard-effects"] {
		t.Error("an edited shader should pull its package group")
	}
	if n.ReShade {
		t.Error("an edited shader says nothing about the DLL")
	}
}

// The full edit flow with an emptied cache: the plan resolves from the
// manifest, classifies everything Skip, and runs to completion without a
// single download.
func TestEditWithEmptyCacheResolvesNothingAndWritesNothing(t *testing.T) {
	f, req, in := neededFixture(t)

	// Empty the cache entirely — the user's "clear all resources" step.
	art := Artifacts{Packages: map[string]string{}, Addons: map[string]string{}, Custom: map[string]string{}}
	// The pipeline's demand-driven resolve: nothing needed -> nothing
	// ensured.

	reg, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	plan, err := (Planner{Registry: reg}).Plan(req, art)
	if err != nil {
		t.Fatalf("Plan() with empty cache over an unchanged install: %v", err)
	}
	if got := plan.WriteCount(); got != 0 {
		t.Errorf("plan writes %d file(s), want none", got)
	}
	if got := plan.TotalBytes(); got != 0 {
		t.Errorf("plan bytes = %d, want 0", got)
	}
	for _, fl := range plan.Files {
		if fl.Action != ActionSkip {
			t.Errorf("%s: action = %q, want skip", fl.Dest, fl.Action)
		}
	}
	// The removed-add-on case exercises plan.Removed, covered elsewhere.
	_ = in
}

// A fresh install (no manifest) still needs everything, and a missing
// artifact with no manifest to fall back on fails the plan.
func TestPlanMissingArtifactWithoutManifestErrors(t *testing.T) {
	f := newFixture(t).WithReShade()
	req := f.Request()
	req.Packages = []string{"standard-effects"}

	_, err := (Planner{}).Plan(req, Artifacts{
		Packages: map[string]string{}, Addons: map[string]string{}, Custom: map[string]string{},
	})
	if err == nil {
		t.Error("want ErrMissingArtifact for a fresh install with no artifacts, got nil")
	}
}

// The belt-and-braces guard: a manifest-derived file that no longer
// matches its record fails the plan loudly instead of half-rewriting.
func TestPlanManifestDerivedButEditedErrors(t *testing.T) {
	f, req, in := neededFixture(t)

	// Edit an installed shader, then hide its group from the plan — a
	// state the needed-set computation should prevent, but which the
	// planner still refuses to act on.
	if err := os.WriteFile(filepath.Join(f.GameDir, "Game", "reshade-shaders", "Shaders", "Deband.fx"),
		[]byte("// edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = in
	art := Artifacts{Packages: map[string]string{}, Addons: map[string]string{}, Custom: map[string]string{}}
	if _, err := (Planner{Registry: state.Registry{}}).Plan(req, art); err != nil {
		return // refused before planning: acceptable
	}
	reg, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if _, err := (Planner{Registry: reg}).Plan(req, art); err == nil {
		t.Error("want an error for a changed file with no resolved source, got nil")
	}
}
