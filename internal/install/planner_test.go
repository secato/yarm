package install

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/state"
)

func TestPlanFreshInstall(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("standard-effects",
			artifacts.PackageMeta{Name: "Standard", EffectFiles: []string{"Deband.fx"}},
			map[string]string{
				"Shaders/Deband.fx":       "// deband",
				"Shaders/DisplayDepth.fx": "// not in EffectFiles",
				"Shaders/ReShade.fxh":     "// header, always installed",
				"Textures/noise.png":      "png",
			}).
		WithAddon("swapchain", map[string]string{"swapchain.addon64": "bin"})

	req := f.Request()
	req.Packages = []string{"standard-effects"}
	req.Addons = []string{"swapchain"}

	plan, err := (Planner{}).Plan(req, f.Art)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	want := []string{
		"Game/dxgi.dll",
		"Game/reshade-shaders/Shaders/Deband.fx",
		"Game/reshade-shaders/Shaders/ReShade.fxh",
		"Game/reshade-shaders/Textures/noise.png",
		"Game/swapchain.addon64",
		"Game/ReShade.ini",
	}
	got := dests(plan)
	slices.Sort(got)
	slices.Sort(want)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("planned files mismatch (-want +got):\n%s", diff)
		t.Log("DisplayDepth.fx is absent from EffectFiles so must not be installed; .fxh headers always are")
	}

	for _, d := range got {
		if a := actionFor(t, plan, d); a != ActionCreate {
			t.Errorf("%s: action = %q, want %q on a fresh install", d, a, ActionCreate)
		}
	}
	if plan.Upgrade {
		t.Error("Upgrade = true on a fresh install")
	}
	if len(plan.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", plan.Warnings)
	}
}

// DenyEffectFiles wins even over an explicit EffectFiles entry.
func TestPlanAppliesDenyList(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("sweetfx",
			artifacts.PackageMeta{
				Name:            "SweetFX",
				EffectFiles:     []string{"LumaSharpen.fx", "Template.fx"},
				DenyEffectFiles: []string{"Template.fx"},
			},
			map[string]string{
				"Shaders/LumaSharpen.fx": "// luma",
				"Shaders/Template.fx":    "// denied",
			})

	req := f.Request()
	req.Packages = []string{"sweetfx"}

	plan, err := (Planner{}).Plan(req, f.Art)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	for _, d := range dests(plan) {
		if strings.HasSuffix(d, "Template.fx") {
			t.Error("a DenyEffectFiles entry was planned for installation")
		}
	}
}

// An empty EffectFiles list means install everything.
func TestPlanWithoutEffectFilterInstallsAll(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("everything", artifacts.PackageMeta{Name: "All"},
			map[string]string{
				"Shaders/A.fx": "a", "Shaders/B.fx": "b", "Shaders/H.fxh": "h",
			})

	req := f.Request()
	req.Packages = []string{"everything"}

	plan, err := (Planner{}).Plan(req, f.Art)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	var shaders int
	for _, d := range dests(plan) {
		if strings.Contains(d, "/Shaders/") {
			shaders++
		}
	}
	if shaders != 3 {
		t.Errorf("planned %d shader files, want 3", shaders)
	}
}

// The d3dcompiler branch is chosen by target OS, never by runtime.GOOS,
// so both cases are assertable on any machine.
func TestPlanD3DCompilerByTargetOS(t *testing.T) {
	tests := []struct {
		target artifacts.TargetOS
		want   bool
	}{
		{artifacts.OSLinux, true},
		{artifacts.OSWindows, false},
		{artifacts.OSDarwin, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.target), func(t *testing.T) {
			f := newFixture(t).WithReShade().WithD3DCompiler()
			req := f.Request()
			req.TargetOS = tt.target

			plan, err := (Planner{}).Plan(req, f.Art)
			if err != nil {
				t.Fatalf("Plan() error = %v", err)
			}

			got := slices.Contains(dests(plan), "Game/"+artifacts.D3DCompiler)
			if got != tt.want {
				t.Errorf("d3dcompiler planned = %v, want %v for target %s", got, tt.want, tt.target)
			}
		})
	}
}

// A Linux install without a resolved d3dcompiler is an error, not a
// silently incomplete install.
func TestPlanLinuxRequiresD3DCompiler(t *testing.T) {
	f := newFixture(t).WithReShade()
	req := f.Request()
	req.TargetOS = artifacts.OSLinux

	_, err := (Planner{}).Plan(req, f.Art)
	if !errors.Is(err, ErrMissingArtifact) {
		t.Errorf("error = %v, want ErrMissingArtifact", err)
	}
}

func TestPlanArchSelectsDLL(t *testing.T) {
	tests := []struct {
		arch game.Arch
		want string
	}{
		{game.ArchX64, artifacts.ReShade64},
		{game.ArchX86, artifacts.ReShade32},
		{game.ArchUnknown, artifacts.ReShade64}, // 64-bit is the modern default
	}

	for _, tt := range tests {
		t.Run(string(tt.arch), func(t *testing.T) {
			f := newFixture(t).WithReShade()
			req := f.Request()
			req.Exe.Arch = tt.arch

			plan, err := (Planner{}).Plan(req, f.Art)
			if err != nil {
				t.Fatalf("Plan() error = %v", err)
			}
			for _, pf := range plan.Files {
				if pf.Origin == state.OriginReShade {
					if !strings.HasSuffix(pf.Source, tt.want) {
						t.Errorf("source = %q, want it to end in %q", pf.Source, tt.want)
					}
					return
				}
			}
			t.Fatal("no reshade file planned")
		})
	}
}

// The conflict decision table from §5.2.
func TestPlanConflictDetection(t *testing.T) {
	const dest = "Game/dxgi.dll"

	t.Run("foreign file kept without Overwrite", func(t *testing.T) {
		f := newFixture(t).WithReShade()
		f.GameFile("Game/dxgi.dll", "someone else's dll")

		plan, err := (Planner{}).Plan(f.Request(), f.Art)
		if err != nil {
			t.Fatalf("Plan(): %v", err)
		}
		if a := actionFor(t, plan, dest); a != ActionKeep {
			t.Errorf("action = %q, want %q", a, ActionKeep)
		}
		if len(plan.Warnings) == 0 {
			t.Error("want a warning about the file being kept")
		}
	})

	t.Run("foreign file backed up with Overwrite", func(t *testing.T) {
		f := newFixture(t).WithReShade()
		f.GameFile("Game/dxgi.dll", "someone else's dll")

		req := f.Request()
		req.Overwrite = true
		plan, err := (Planner{}).Plan(req, f.Art)
		if err != nil {
			t.Fatalf("Plan(): %v", err)
		}
		if a := actionFor(t, plan, dest); a != ActionBackup {
			t.Errorf("action = %q, want %q", a, ActionBackup)
		}
	})

	t.Run("our own identical file is skipped", func(t *testing.T) {
		f := newFixture(t).WithReShade()
		f.GameFile("Game/dxgi.dll", "reshade 64-bit body") // same as the cached source

		var reg state.Registry
		reg.Record("steam:700110", state.Game{Root: f.GameDir}, state.Install{
			Exe: "Game/emberhollow.exe",
			Files: []state.File{
				{Path: dest, SHA256: sha256Of("reshade 64-bit body"), Origin: state.OriginReShade},
			},
		})

		plan, err := (Planner{Registry: reg}).Plan(f.Request(), f.Art)
		if err != nil {
			t.Fatalf("Plan(): %v", err)
		}
		if a := actionFor(t, plan, dest); a != ActionSkip {
			t.Errorf("action = %q, want %q", a, ActionSkip)
		}
		if !plan.Upgrade {
			t.Error("Upgrade = false despite an existing install record")
		}
	})

	t.Run("our own file, user edited, is replaced with a warning", func(t *testing.T) {
		f := newFixture(t).WithReShade()
		f.GameFile("Game/dxgi.dll", "edited by the user")

		var reg state.Registry
		reg.Record("steam:700110", state.Game{Root: f.GameDir}, state.Install{
			Exe: "Game/emberhollow.exe",
			Files: []state.File{
				{Path: dest, SHA256: sha256Of("the original we installed"), Origin: state.OriginReShade},
			},
		})

		plan, err := (Planner{Registry: reg}).Plan(f.Request(), f.Art)
		if err != nil {
			t.Fatalf("Plan(): %v", err)
		}
		if a := actionFor(t, plan, dest); a != ActionReplace {
			t.Errorf("action = %q, want %q", a, ActionReplace)
		}
		if len(plan.Warnings) == 0 {
			t.Error("want a warning that the file was modified")
		}
	})

	t.Run("existing ReShade.ini is never overwritten", func(t *testing.T) {
		f := newFixture(t).WithReShade()
		f.GameFile("Game/"+ININame, "[GENERAL]\nUserSettings=1\n")

		req := f.Request()
		req.Overwrite = true // even then
		plan, err := (Planner{}).Plan(req, f.Art)
		if err != nil {
			t.Fatalf("Plan(): %v", err)
		}
		if a := actionFor(t, plan, "Game/"+ININame); a != ActionSkip {
			t.Errorf("action = %q, want %q: the ini is the user's configuration", a, ActionSkip)
		}
	})
}

// An upgrade drops files the new selection no longer includes.
func TestPlanUpgradeRemovesStaleFiles(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("standard-effects", artifacts.PackageMeta{Name: "Standard"},
			map[string]string{"Shaders/Deband.fx": "// deband"})

	var reg state.Registry
	reg.Record("steam:700110", state.Game{Root: f.GameDir}, state.Install{
		Exe: "Game/emberhollow.exe",
		Files: []state.File{
			{Path: "Game/dxgi.dll", SHA256: "old", Origin: state.OriginReShade},
			{Path: "Game/reshade-shaders/Shaders/Deband.fx", SHA256: "old", Origin: state.PackageOrigin("standard-effects")},
			{Path: "Game/reshade-shaders/Shaders/Dropped.fx", SHA256: "old", Origin: state.PackageOrigin("gone")},
			{Path: "Game/old.addon64", SHA256: "old", Origin: state.AddonOrigin("gone")},
		},
	})

	req := f.Request()
	req.Packages = []string{"standard-effects"}

	plan, err := (Planner{Registry: reg}).Plan(req, f.Art)
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}
	if !plan.Upgrade {
		t.Error("Upgrade = false, want true")
	}

	want := []string{"Game/old.addon64", "Game/reshade-shaders/Shaders/Dropped.fx"}
	if diff := cmp.Diff(want, plan.Removed); diff != "" {
		t.Errorf("Removed mismatch (-want +got):\n%s", diff)
	}
}

func TestPlanRejectsInvalidRequests(t *testing.T) {
	f := newFixture(t).WithReShade()

	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{"no version", func(r *Request) { r.Version = "" }},
		{"no dll name", func(r *Request) { r.DLLName = "" }},
		{"dll name with a path", func(r *Request) { r.DLLName = "../evil.dll" }},
		{"dll name not a dll", func(r *Request) { r.DLLName = "dxgi.txt" }},
		{"no exe", func(r *Request) { r.Exe.Path = "" }},
		{"no game root", func(r *Request) { r.Game.Root = "" }},
		{"bad flavor", func(r *Request) { r.Flavor = "weird" }},
		{"addons without the addon flavor", func(r *Request) {
			r.Flavor = FlavorNormal
			r.Addons = []string{"swapchain"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := f.Request()
			tt.mutate(&req)
			if _, err := (Planner{}).Plan(req, f.Art); err == nil {
				t.Error("want an error, got nil")
			}
		})
	}
}

// Two packages shipping the same shader would race, and the manifest
// could record a hash that does not match what landed.
func TestPlanRejectsDuplicateDestinations(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("a", artifacts.PackageMeta{Name: "A"}, map[string]string{"Shaders/Same.fx": "from a"}).
		WithPackage("b", artifacts.PackageMeta{Name: "B"}, map[string]string{"Shaders/Same.fx": "from b"})

	req := f.Request()
	req.Packages = []string{"a", "b"}

	_, err := (Planner{}).Plan(req, f.Art)
	if err == nil {
		t.Fatal("want an error for two packages writing the same file, got nil")
	}
	if !strings.Contains(err.Error(), "Same.fx") {
		t.Errorf("error should name the conflicting file, got: %v", err)
	}
}

func TestPlanMissingArtifacts(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*fixture) Request
	}{
		{"no reshade", func(f *fixture) Request { return f.Request() }},
		{"unknown package", func(f *fixture) Request {
			f.WithReShade()
			r := f.Request()
			r.Packages = []string{"nope"}
			return r
		}},
		{"unknown addon", func(f *fixture) Request {
			f.WithReShade()
			r := f.Request()
			r.Addons = []string{"nope"}
			return r
		}},
		{"unknown custom", func(f *fixture) Request {
			f.WithReShade()
			r := f.Request()
			r.Custom = []string{"nope"}
			return r
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			req := tt.setup(f)
			if _, err := (Planner{}).Plan(req, f.Art); !errors.Is(err, ErrMissingArtifact) {
				t.Errorf("error = %v, want ErrMissingArtifact", err)
			}
		})
	}
}

// Custom content accepts both the documented layout and loose .fx files.
func TestPlanCustomContent(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithCustom("mine", map[string]string{
			"Shaders/Cool.fx": "// cool",
			"Textures/t.png":  "png",
			"Loose.fx":        "// loose, no Shaders dir",
			"notes.txt":       "ignored",
		})

	req := f.Request()
	req.Custom = []string{"mine"}

	plan, err := (Planner{}).Plan(req, f.Art)
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}

	got := dests(plan)
	for _, want := range []string{
		"Game/reshade-shaders/Shaders/Cool.fx",
		"Game/reshade-shaders/Textures/t.png",
		"Game/reshade-shaders/Shaders/Loose.fx",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %q from %v", want, got)
		}
	}
	for _, d := range got {
		if strings.HasSuffix(d, "notes.txt") {
			t.Error("a non-shader file was planned for installation")
		}
	}
}

// A game whose executable sits at the root has no exe subdirectory.
func TestPlanExeAtGameRoot(t *testing.T) {
	f := newFixture(t).WithReShade()
	req := f.Request()
	req.Exe.Path = "Ravenswatch.exe"

	plan, err := (Planner{}).Plan(req, f.Art)
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}
	if !slices.Contains(dests(plan), "dxgi.dll") {
		t.Errorf("want dxgi.dll at the root, got %v", dests(plan))
	}
}

// Overwrite with NoBackup is the only irreversible thing the installer
// does, so it is a distinct action rather than a quieter kind of backup:
// the plan, its warnings and the progress log all have to say so.
func TestPlanDiscardsForeignFileWhenBackupIsOff(t *testing.T) {
	f := newFixture(t).WithReShade()
	f.GameFile("Game/dxgi.dll", "something else's dxgi.dll")

	req := f.Request()
	req.Overwrite = true
	req.NoBackup = true

	plan, err := (Planner{}).Plan(req, f.Art)
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}

	var got Action
	for _, pf := range plan.Files {
		if pf.Dest == "Game/dxgi.dll" {
			got = pf.Action
		}
	}
	if got != ActionDiscard {
		t.Errorf("dxgi.dll action = %q, want %q", got, ActionDiscard)
	}
	if !hasWarningContaining(plan.Warnings, "uninstall cannot put it back") {
		t.Errorf("warnings = %v, want one saying the original is gone for good", plan.Warnings)
	}
}

// hasWarningContaining reports whether any warning contains sub.
func hasWarningContaining(warnings []string, sub string) bool {
	for _, w := range warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}
