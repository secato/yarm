package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/state"
)

// preflightReq returns a request targeting root/Game/game.exe.
func preflightReq(root string, target artifacts.TargetOS) Request {
	return Request{
		Game:     game.Game{Root: root},
		Exe:      game.Executable{Path: "Game/game.exe"},
		Version:  "6.8.0",
		Flavor:   FlavorNormal,
		DLLName:  "dxgi.dll",
		TargetOS: target,
	}
}

// writeFile creates root/rel with n bytes.
func writeFile(t *testing.T, root, rel string, n int) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightFindsNothingInACleanFolder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Game/game.exe", 1)

	if got := Preflight(preflightReq(root, artifacts.OSLinux), nil); len(got) != 0 {
		t.Errorf("Preflight() = %v, want nothing in an empty folder", got)
	}
}

// The case that prompted this: another injector already owns the proxy DLL
// name (OptiScaler installs itself as dxgi.dll), and the game ships its own
// d3dcompiler_47.dll. Without overwrite both are kept, and ReShade never
// loads — which the user can only find out afterwards unless the wizard
// says so first.
func TestPreflightReportsForeignFilesAsBlockingWithoutOverwrite(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Game/game.exe", 1)
	writeFile(t, root, "Game/dxgi.dll", 25872384)
	writeFile(t, root, "Game/d3dcompiler_47.dll", 4493352)

	got := Preflight(preflightReq(root, artifacts.OSLinux), nil)
	if len(got) != 2 {
		t.Fatalf("Preflight() returned %d conflicts, want 2: %v", len(got), got)
	}
	for _, c := range got {
		if c.Kind != ConflictForeign {
			t.Errorf("%s: kind = %q, want foreign", c.Path, c.Kind)
		}
		if c.ReShade {
			t.Errorf("%s: marked as a ReShade install, but there is no ReShade.ini beside it", c.Path)
		}
		if !c.Blocking(false) {
			t.Errorf("%s: should block an install that is not overwriting", c.Path)
		}
		if c.Blocking(true) {
			t.Errorf("%s: should not block once overwrite is on — it gets backed up", c.Path)
		}
	}
	if got[0].Size != 25872384 {
		t.Errorf("Size = %d, want the real file size — it is what identifies the file", got[0].Size)
	}
}

// On Windows there is no d3dcompiler_47.dll to install, so one sitting
// there is none of yarm's business.
func TestPreflightIgnoresD3DCompilerOnWindows(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Game/game.exe", 1)
	writeFile(t, root, "Game/d3dcompiler_47.dll", 10)

	if got := Preflight(preflightReq(root, artifacts.OSWindows), nil); len(got) != 0 {
		t.Errorf("Preflight() = %v, want nothing: d3dcompiler is not part of a Windows install", got)
	}
}

// A file a recorded install of ours owns is simply replaced and needs no
// permission — reporting it as a conflict would train the user to ignore
// the section.
func TestPreflightSeparatesOurOwnFilesFromForeignOnes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Game/game.exe", 1)
	writeFile(t, root, "Game/dxgi.dll", 5255448)
	prev := &state.Install{
		Exe:   "Game/game.exe",
		Files: []state.File{{Path: "Game/dxgi.dll", Origin: state.OriginReShade}},
	}

	got := Preflight(preflightReq(root, artifacts.OSLinux), prev)
	if len(got) != 1 || got[0].Kind != ConflictManaged {
		t.Fatalf("Preflight() = %v, want one managed conflict", got)
	}
	if got[0].Blocking(false) {
		t.Error("a file yarm installed itself must never block an install")
	}
}

// An unmanaged ReShade install (a DLL beside a ReShade.ini) is a different
// thing from an unrelated program using the same name, and the review page
// says so differently.
func TestPreflightRecognizesAnUnmanagedReShadeInstall(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Game/game.exe", 1)
	writeFile(t, root, "Game/dxgi.dll", 5255448)
	writeFile(t, root, "Game/"+ININame, 500)

	got := Preflight(preflightReq(root, artifacts.OSLinux), nil)
	if len(got) != 2 {
		t.Fatalf("Preflight() = %v, want the DLL and the ini", got)
	}
	if !got[0].ReShade {
		t.Error("a DLL beside a ReShade.ini should be recognized as a ReShade install")
	}
	if got[1].Kind != ConflictConfig {
		t.Errorf("ReShade.ini: kind = %q, want config — it is never overwritten either way", got[1].Kind)
	}
	if got[1].Blocking(false) || got[1].Blocking(true) {
		t.Error("ReShade.ini is kept in both modes, so it never blocks")
	}
}
