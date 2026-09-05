package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/secato/yarm/internal/fetch"
	"github.com/secato/yarm/internal/game"
)

// NetworkTestsEnv gates the tests that reach the real internet.
const NetworkTestsEnv = "YARM_NETWORK_TESTS"

// requireNetwork skips unless the opt-in env var is set. These tests
// download tens of megabytes from third-party CDNs, so they are not part
// of the default suite or CI.
func requireNetwork(t *testing.T) {
	t.Helper()
	if os.Getenv(NetworkTestsEnv) != "1" {
		t.Skipf("set %s=1 to run tests that download from the internet", NetworkTestsEnv)
	}
}

// TestExtractD3DCompilerLive checks the pinned Firefox installer still
// exists, still hashes as recorded, and still yields the exact DLL §4.4
// promises. If Mozilla ever re-publishes that release, this is what
// catches it.
func TestExtractD3DCompilerLive(t *testing.T) {
	requireNetwork(t)

	for _, arch := range []game.Arch{game.ArchX64, game.ArchX86} {
		t.Run(string(arch), func(t *testing.T) {
			src, err := D3DSourceFor(arch)
			if err != nil {
				t.Fatalf("D3DSourceFor: %v", err)
			}

			dir := t.TempDir()
			installer := filepath.Join(dir, "firefox.exe")

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			c := fetch.New("yarm/test (+https://github.com/secato/yarm)")
			// The installer hash is verified by Download itself; a
			// mismatch here means the pinned source changed.
			if err := c.Download(ctx, src.URL, installer, nil, src.InstallerSHA256); err != nil {
				t.Fatalf("download pinned installer: %v", err)
			}

			dll, err := ExtractD3DCompiler(installer, filepath.Join(dir, "out"), src)
			if err != nil {
				t.Fatalf("ExtractD3DCompiler: %v", err)
			}

			info, err := os.Stat(dll)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if info.Size() != src.DLLSize {
				t.Errorf("extracted size = %d, want %d", info.Size(), src.DLLSize)
			}
		})
	}
}

// TestExtractReShadeLive checks that reshade.me still publishes a setup
// executable Go's archive/zip can read directly, which the whole
// extraction path in §4.1 depends on.
func TestExtractReShadeLive(t *testing.T) {
	requireNetwork(t)

	const version = "6.8.0"
	dir := t.TempDir()
	setup := filepath.Join(dir, "setup.exe")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	c := fetch.New("yarm/test (+https://github.com/secato/yarm)")
	url := "https://reshade.me/downloads/ReShade_Setup_" + version + "_Addon.exe"
	if err := c.Download(ctx, url, setup, nil, ""); err != nil {
		t.Fatalf("download ReShade setup: %v", err)
	}

	out := filepath.Join(dir, "out")
	if err := ExtractReShade(setup, out); err != nil {
		t.Fatalf("ExtractReShade: %v", err)
	}

	for _, name := range []string{ReShade32, ReShade64} {
		info, err := os.Stat(filepath.Join(out, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if info.Size() < 1<<20 {
			t.Errorf("%s is only %d bytes; expected a real DLL", name, info.Size())
		}
	}
}
