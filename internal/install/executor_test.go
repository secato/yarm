package install

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/state"
)

func newExec(f *fixture) *Executor {
	e := NewExecutor(f.StateDir)
	e.Now = func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }
	return e
}

// planAndRun is the common path: plan against the fixture, then execute.
func planAndRun(t *testing.T, f *fixture, req Request, reg state.Registry) (Result, error) {
	t.Helper()
	plan, err := (Planner{Registry: reg}).Plan(req, f.Art)
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}
	return newExec(f).Run(context.Background(), plan, nil)
}

func TestRunFreshInstall(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("standard-effects", artifacts.PackageMeta{Name: "Standard"},
			map[string]string{"Shaders/Deband.fx": "// deband", "Textures/n.png": "png"}).
		WithAddon("swapchain", map[string]string{"swapchain.addon64": "addon bin"})

	req := f.Request()
	req.Packages = []string{"standard-effects"}
	req.Addons = []string{"swapchain"}

	res, err := planAndRun(t, f, req, state.Registry{})
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}

	for _, rel := range []string{
		"Game/dxgi.dll",
		"Game/reshade-shaders/Shaders/Deband.fx",
		"Game/reshade-shaders/Textures/n.png",
		"Game/swapchain.addon64",
		"Game/" + ININame,
	} {
		if !f.exists(rel) {
			t.Errorf("%s was not created", rel)
		}
	}

	if got := f.read("Game/dxgi.dll"); got != "reshade 64-bit body" {
		t.Errorf("dxgi.dll content = %q", got)
	}

	// The manifest must record every file, with real hashes.
	reg, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	in, ok := reg.FindInstall("steam:1245620", "Game/eldenring.exe")
	if !ok {
		t.Fatal("no install recorded")
	}
	if len(in.Files) != 5 {
		t.Errorf("manifest lists %d files, want 5", len(in.Files))
	}
	for _, fl := range in.Files {
		if len(fl.SHA256) != 64 {
			t.Errorf("%s: sha256 = %q, want a full digest", fl.Path, fl.SHA256)
		}
		if fl.Size <= 0 {
			t.Errorf("%s: size = %d", fl.Path, fl.Size)
		}
	}
	if in.ReShade.Version != "6.8.0" || in.ReShade.DLL != "dxgi.dll" {
		t.Errorf("ReShade info = %+v", in.ReShade)
	}
	if len(res.Written) != 5 {
		t.Errorf("Result.Written has %d entries, want 5", len(res.Written))
	}
}

func TestRunWritesDefaultINI(t *testing.T) {
	f := newFixture(t).WithReShade()

	if _, err := planAndRun(t, f, f.Request(), state.Registry{}); err != nil {
		t.Fatalf("Run(): %v", err)
	}

	got := f.read("Game/" + ININame)
	for _, want := range []string{
		"[GENERAL]",
		`EffectSearchPaths=.\reshade-shaders\Shaders\**`,
		`TextureSearchPaths=.\reshade-shaders\Textures\**`,
		`PresetPath=.\` + PresetName,
		"[SCREENSHOT]",
		`SavePath=.\screenshots`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ReShade.ini missing %q\ngot:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "\r\n") {
		t.Error("ReShade.ini should use CRLF line endings, as ReShade's own installer does")
	}
}

// An existing ini holds the user's settings and must survive untouched.
func TestRunKeepsExistingINI(t *testing.T) {
	f := newFixture(t).WithReShade()
	const userINI = "[GENERAL]\nEffectSearchPaths=.\\my-shaders\n"
	f.GameFile("Game/"+ININame, userINI)

	if _, err := planAndRun(t, f, f.Request(), state.Registry{}); err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if got := f.read("Game/" + ININame); got != userINI {
		t.Errorf("ReShade.ini was modified:\ngot:  %q\nwant: %q", got, userINI)
	}
}

func TestRunBacksUpForeignFile(t *testing.T) {
	f := newFixture(t).WithReShade()
	const original = "a pre-existing dxgi.dll from something else"
	f.GameFile("Game/dxgi.dll", original)

	req := f.Request()
	req.Overwrite = true

	res, err := planAndRun(t, f, req, state.Registry{})
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}

	if got := f.read("Game/dxgi.dll"); got != "reshade 64-bit body" {
		t.Errorf("dxgi.dll = %q, want the ReShade body", got)
	}
	if got := f.read("Game/dxgi.dll" + BackupSuffix); got != original {
		t.Errorf("backup = %q, want the original", got)
	}
	if len(res.Install.Backups) != 1 {
		t.Fatalf("manifest records %d backups, want 1", len(res.Install.Backups))
	}
	if res.Install.Backups[0].Path != "Game/dxgi.dll" {
		t.Errorf("backup entry = %+v", res.Install.Backups[0])
	}
}

// Without Overwrite, a foreign file is left exactly as it was.
func TestRunKeepsForeignFileWithoutOverwrite(t *testing.T) {
	f := newFixture(t).WithReShade()
	const original = "not ours"
	f.GameFile("Game/dxgi.dll", original)

	res, err := planAndRun(t, f, f.Request(), state.Registry{})
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if got := f.read("Game/dxgi.dll"); got != original {
		t.Errorf("dxgi.dll = %q, want it untouched", got)
	}
	if !slices.Contains(res.Skipped, "Game/dxgi.dll") {
		t.Errorf("Skipped = %v, want it to include the kept file", res.Skipped)
	}
	if f.exists("Game/dxgi.dll" + BackupSuffix) {
		t.Error("no backup should be made when nothing is overwritten")
	}
}

// A failure part-way through must leave the directory as it was found.
func TestRunRollsBackOnFailure(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("pkg", artifacts.PackageMeta{Name: "P"},
			map[string]string{"Shaders/A.fx": "a", "Shaders/B.fx": "b"})

	const foreign = "a pre-existing dxgi.dll"
	f.GameFile("Game/dxgi.dll", foreign)

	req := f.Request()
	req.Packages = []string{"pkg"}
	req.Overwrite = true

	plan, err := (Planner{}).Plan(req, f.Art)
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}

	// Break one source after planning, so the copy fails mid-run.
	var broken string
	for _, pf := range plan.Files {
		if strings.HasSuffix(pf.Dest, "B.fx") {
			broken = pf.Source
		}
	}
	if broken == "" {
		t.Fatal("could not find the B.fx source to break")
	}
	if err := os.Remove(broken); err != nil {
		t.Fatalf("remove source: %v", err)
	}

	if _, err := newExec(f).Run(context.Background(), plan, nil); err == nil {
		t.Fatal("Run() succeeded despite a missing source")
	}

	// Everything must be back as it was.
	if got := f.read("Game/dxgi.dll"); got != foreign {
		t.Errorf("dxgi.dll = %q, want the original restored", got)
	}
	if f.exists("Game/dxgi.dll" + BackupSuffix) {
		t.Error("backup file left behind after rollback")
	}
	if f.exists("Game/reshade-shaders/Shaders/A.fx") {
		t.Error("a copied file was not removed by rollback")
	}
	if f.exists("Game/" + ININame) {
		t.Error("ReShade.ini was left behind after rollback")
	}

	// And nothing may claim an install happened.
	reg, err := state.Load(f.StateDir)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if _, ok := reg.FindInstall("steam:1245620", "Game/eldenring.exe"); ok {
		t.Error("a manifest was written despite the failure")
	}
}

// A failed upgrade must not destroy the install it was replacing.
func TestRunFailedUpgradeRestoresRemovedFiles(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("pkg", artifacts.PackageMeta{Name: "P"},
			map[string]string{"Shaders/A.fx": "a"})

	// A previous install left a shader the new selection drops.
	const stale = "shader from the previous install"
	f.GameFile("Game/reshade-shaders/Shaders/Dropped.fx", stale)
	f.GameFile("Game/dxgi.dll", "old reshade")

	var reg state.Registry
	reg.Record("steam:1245620", state.Game{Root: f.GameDir}, state.Install{
		Exe: "Game/eldenring.exe",
		Files: []state.File{
			{Path: "Game/dxgi.dll", SHA256: sha256Of("old reshade"), Origin: state.OriginReShade},
			{Path: "Game/reshade-shaders/Shaders/Dropped.fx", SHA256: sha256Of(stale), Origin: state.PackageOrigin("gone")},
		},
	})

	req := f.Request()
	req.Packages = []string{"pkg"}

	plan, err := (Planner{Registry: reg}).Plan(req, f.Art)
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}
	if !slices.Contains(plan.Removed, "Game/reshade-shaders/Shaders/Dropped.fx") {
		t.Fatalf("Removed = %v, want the dropped shader", plan.Removed)
	}

	// Break the install after the removal step has already run.
	for _, pf := range plan.Files {
		if strings.HasSuffix(pf.Dest, "A.fx") {
			if err := os.Remove(pf.Source); err != nil {
				t.Fatalf("remove source: %v", err)
			}
		}
	}

	if _, err := newExec(f).Run(context.Background(), plan, nil); err == nil {
		t.Fatal("Run() succeeded despite a missing source")
	}

	if !f.exists("Game/reshade-shaders/Shaders/Dropped.fx") {
		t.Fatal("a failed upgrade destroyed a file from the previous install")
	}
	if got := f.read("Game/reshade-shaders/Shaders/Dropped.fx"); got != stale {
		t.Errorf("restored content = %q, want %q", got, stale)
	}
	// The staging file must not survive either.
	if f.exists("Game/reshade-shaders/Shaders/Dropped.fx" + removedSuffix) {
		t.Error("a staging file was left behind")
	}
}

// A successful upgrade really does remove what it superseded.
func TestRunUpgradeRemovesStaleFiles(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("pkg", artifacts.PackageMeta{Name: "P"},
			map[string]string{"Shaders/A.fx": "a"})

	const stale = "shader from the previous install"
	f.GameFile("Game/reshade-shaders/Shaders/Dropped.fx", stale)

	var reg state.Registry
	reg.Record("steam:1245620", state.Game{Root: f.GameDir}, state.Install{
		Exe: "Game/eldenring.exe",
		Files: []state.File{
			{Path: "Game/reshade-shaders/Shaders/Dropped.fx", SHA256: sha256Of(stale), Origin: state.PackageOrigin("gone")},
		},
	})

	req := f.Request()
	req.Packages = []string{"pkg"}

	res, err := planAndRun(t, f, req, reg)
	if err != nil {
		t.Fatalf("Run(): %v", err)
	}
	if f.exists("Game/reshade-shaders/Shaders/Dropped.fx") {
		t.Error("a superseded file survived the upgrade")
	}
	if f.exists("Game/reshade-shaders/Shaders/Dropped.fx" + removedSuffix) {
		t.Error("a staging file was left behind after a successful upgrade")
	}
	if !slices.Contains(res.Removed, "Game/reshade-shaders/Shaders/Dropped.fx") {
		t.Errorf("Removed = %v", res.Removed)
	}
	if !f.exists("Game/reshade-shaders/Shaders/A.fx") {
		t.Error("the new shader was not installed")
	}
}

func TestRunEmitsProgress(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("pkg", artifacts.PackageMeta{Name: "P"},
			map[string]string{"Shaders/A.fx": "a", "Shaders/B.fx": "b"})

	req := f.Request()
	req.Packages = []string{"pkg"}

	plan, err := (Planner{}).Plan(req, f.Art)
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}

	var events []Event
	if _, err := newExec(f).Run(context.Background(), plan, func(ev Event) {
		events = append(events, ev)
	}); err != nil {
		t.Fatalf("Run(): %v", err)
	}

	var copies int
	kinds := map[StepKind]bool{}
	for _, ev := range events {
		kinds[ev.Kind] = true
		if ev.Kind == StepCopy {
			copies++
			if ev.Total != plan.WriteCount()-1 && ev.Total != plan.WriteCount() {
				t.Errorf("copy event Total = %d, plan writes %d", ev.Total, plan.WriteCount())
			}
		}
	}
	if copies != 3 { // dxgi.dll + two shaders; the ini is its own step
		t.Errorf("got %d copy events, want 3", copies)
	}
	for _, want := range []StepKind{StepCopy, StepINI, StepManifest} {
		if !kinds[want] {
			t.Errorf("no %q event emitted", want)
		}
	}
}

func TestRunCancellation(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("pkg", artifacts.PackageMeta{Name: "P"},
			map[string]string{"Shaders/A.fx": "a"})

	req := f.Request()
	req.Packages = []string{"pkg"}
	plan, err := (Planner{}).Plan(req, f.Art)
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := newExec(f).Run(ctx, plan, nil); err == nil {
		t.Fatal("Run() succeeded with a canceled context")
	}
	if f.exists("Game/dxgi.dll") {
		t.Error("a canceled run left files behind")
	}
}

// Destinations are built from catalog content, so escaping the game root
// is refused rather than assumed impossible.
func TestSafeDest(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "games", "mygame")

	for _, rel := range []string{"../evil.dll", "a/../../evil.dll", "/etc/passwd"} {
		if _, err := safeDest(root, rel); err == nil {
			t.Errorf("safeDest(%q) = nil error, want a rejection", rel)
		}
	}
	got, err := safeDest(root, "Game/dxgi.dll")
	if err != nil {
		t.Fatalf("safeDest on a normal path: %v", err)
	}
	if want := filepath.Join(root, "Game", "dxgi.dll"); got != want {
		t.Errorf("safeDest() = %q, want %q", got, want)
	}
}

func TestPlanCountersMatchFiles(t *testing.T) {
	f := newFixture(t).WithReShade().
		WithPackage("pkg", artifacts.PackageMeta{Name: "P"},
			map[string]string{"Shaders/A.fx": "aaa"})
	req := f.Request()
	req.Packages = []string{"pkg"}

	plan, err := (Planner{}).Plan(req, f.Art)
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}

	var wantCount int
	var wantBytes int64
	for _, pf := range plan.Files {
		if pf.Action.Writes() {
			wantCount++
			wantBytes += pf.Size
		}
	}
	if diff := cmp.Diff(wantCount, plan.WriteCount()); diff != "" {
		t.Errorf("WriteCount mismatch: %s", diff)
	}
	if plan.TotalBytes() != wantBytes {
		t.Errorf("TotalBytes = %d, want %d", plan.TotalBytes(), wantBytes)
	}
}
