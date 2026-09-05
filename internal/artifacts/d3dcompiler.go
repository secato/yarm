package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"

	"github.com/secato/yarm/internal/archive"
	"github.com/secato/yarm/internal/game"
)

// D3DCompiler is the DLL name ReShade needs for D3D9/10/11 effects.
const D3DCompiler = "d3dcompiler_47.dll"

// archiveEntry is the path of the DLL inside the Firefox installer.
const archiveEntry = "core/" + D3DCompiler

// D3DSource pins one architecture's Firefox installer and the hashes of
// both the installer and the DLL extracted from it
// (docs/plan/04-external-sources.md §4.4).
//
// Firefox 62.0.3 is used because winetricks and reshade-steam-proton use
// it: it is a stable, permanently-hosted Mozilla CDN URL that happens to
// ship the Microsoft D3D compiler Wine's builtin cannot fully replace.
type D3DSource struct {
	URL string
	// InstallerSHA256 guards the ~40 MB download.
	InstallerSHA256 string
	// DLLSHA256 and DLLSize guard what comes out of it.
	DLLSHA256 string
	DLLSize   int64
}

// d3dSources are the pinned per-architecture installers.
var d3dSources = map[game.Arch]D3DSource{
	game.ArchX64: {
		URL:             "https://download-installer.cdn.mozilla.net/pub/firefox/releases/62.0.3/win64/ach/Firefox%20Setup%2062.0.3.exe",
		InstallerSHA256: "721977f36c008af2b637aedd3f1b529f3cfed6feb10f68ebe17469acb1934986",
		DLLSHA256:       "0427b16ffb7d2f6f2d611cc9f0520f42c70bcbfbdc2fc0d47e8735fd1c79a308",
		DLLSize:         4410168,
	},
	game.ArchX86: {
		URL:             "https://download-installer.cdn.mozilla.net/pub/firefox/releases/62.0.3/win32/ach/Firefox%20Setup%2062.0.3.exe",
		InstallerSHA256: "d6edb4ff0a713f417ebd19baedfe07527c6e45e84a6c73ed8c66a33377cc0aca",
		DLLSHA256:       "b35b96b1eb5539a3748b98643172681f9b74d0ec726b05f8c2c248c10888e9a5",
		DLLSize:         3681592,
	},
}

// ErrUnsupportedArch is returned for an architecture with no pinned
// d3dcompiler source.
var ErrUnsupportedArch = errors.New("no d3dcompiler_47.dll source for architecture")

// D3DSourceFor returns the pinned source for an architecture.
func D3DSourceFor(arch game.Arch) (D3DSource, error) {
	src, ok := d3dSources[arch]
	if !ok {
		return D3DSource{}, fmt.Errorf("%w: %s", ErrUnsupportedArch, arch)
	}
	return src, nil
}

// ExtractD3DCompiler pulls core/d3dcompiler_47.dll out of a downloaded
// Firefox installer (a 7-Zip self-extracting archive) into dstDir and
// verifies it against the pinned hash and size.
//
// A mismatch removes the extracted file and fails: this DLL is copied into
// the user's game directory, so an unverified one is never left behind for
// a later run to find and trust.
func ExtractD3DCompiler(installer, dstDir string, want D3DSource) (string, error) {
	r, err := sevenzip.OpenReader(installer)
	if err != nil {
		return "", fmt.Errorf("open %s as 7z: %w", filepath.Base(installer), err)
	}
	defer func() { _ = r.Close() }()

	dst, err := archive.SafePath(dstDir, D3DCompiler)
	if err != nil {
		return "", err
	}

	for _, f := range r.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		if !strings.EqualFold(name, archiveEntry) {
			continue
		}

		if err := extract7z(f, dst); err != nil {
			return "", err
		}
		if err := verifyD3D(dst, want); err != nil {
			_ = os.Remove(dst)
			return "", err
		}
		return dst, nil
	}

	return "", fmt.Errorf("%s not found in %s", archiveEntry, filepath.Base(installer))
}

// extract7z writes one 7z entry to dst.
func extract7z(f *sevenzip.File, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, rc); err != nil {
		return err
	}
	return out.Sync()
}

// verifyD3D checks an extracted DLL against its pinned size and hash.
func verifyD3D(path string, want D3DSource) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if want.DLLSize > 0 && info.Size() != want.DLLSize {
		return fmt.Errorf("%s: size %d, want %d", D3DCompiler, info.Size(), want.DLLSize)
	}

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want.DLLSHA256) {
		return fmt.Errorf("%s: sha256 %s, want %s", D3DCompiler, got, want.DLLSHA256)
	}
	return nil
}
