package artifacts

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/secato/yarm/internal/game"
)

// The pinned values are what makes this DLL safe to copy into a user's
// game directory, so they are asserted rather than merely used.
func TestD3DSourceFor(t *testing.T) {
	tests := []struct {
		arch     game.Arch
		wantURL  string
		wantSize int64
		wantSHA  string
	}{
		{
			arch:     game.ArchX64,
			wantURL:  "https://download-installer.cdn.mozilla.net/pub/firefox/releases/62.0.3/win64/ach/Firefox%20Setup%2062.0.3.exe",
			wantSize: 4410168,
			wantSHA:  "0427b16ffb7d2f6f2d611cc9f0520f42c70bcbfbdc2fc0d47e8735fd1c79a308",
		},
		{
			arch:     game.ArchX86,
			wantURL:  "https://download-installer.cdn.mozilla.net/pub/firefox/releases/62.0.3/win32/ach/Firefox%20Setup%2062.0.3.exe",
			wantSize: 3681592,
			wantSHA:  "b35b96b1eb5539a3748b98643172681f9b74d0ec726b05f8c2c248c10888e9a5",
		},
	}

	for _, tt := range tests {
		got, err := D3DSourceFor(tt.arch)
		if err != nil {
			t.Errorf("D3DSourceFor(%s) error = %v", tt.arch, err)
			continue
		}
		if got.URL != tt.wantURL {
			t.Errorf("%s URL = %q", tt.arch, got.URL)
		}
		if got.DLLSize != tt.wantSize {
			t.Errorf("%s DLLSize = %d, want %d", tt.arch, got.DLLSize, tt.wantSize)
		}
		if got.DLLSHA256 != tt.wantSHA {
			t.Errorf("%s DLLSHA256 = %q", tt.arch, got.DLLSHA256)
		}
		if len(got.InstallerSHA256) != 64 {
			t.Errorf("%s InstallerSHA256 is not a sha256 digest: %q", tt.arch, got.InstallerSHA256)
		}
	}

	if _, err := D3DSourceFor(game.ArchUnknown); !errors.Is(err, ErrUnsupportedArch) {
		t.Errorf("D3DSourceFor(unknown) error = %v, want ErrUnsupportedArch", err)
	}
}

func TestExtractD3DCompilerRejectsNon7z(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-7z.exe")
	if err := os.WriteFile(path, []byte("MZ not a 7z archive"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	src, err := D3DSourceFor(game.ArchX64)
	if err != nil {
		t.Fatalf("D3DSourceFor: %v", err)
	}
	if _, err := ExtractD3DCompiler(path, filepath.Join(dir, "out"), src); err == nil {
		t.Error("want an error for a non-7z file, got nil")
	}
}

// A DLL that does not match the pinned hash must not survive on disk: it
// is copied into the user's game directory, so a bad one is never left for
// a later run to trust.
func TestVerifyD3DRejectsMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, D3DCompiler)
	if err := os.WriteFile(path, []byte("wrong content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	src, _ := D3DSourceFor(game.ArchX64)
	if err := verifyD3D(path, src); err == nil {
		t.Fatal("want an error for a size/hash mismatch, got nil")
	}

	// Size matches but content does not.
	content := make([]byte, src.DLLSize)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := verifyD3D(path, src); err == nil {
		t.Error("want a hash error when only the size matches, got nil")
	}
}

// Fixture values for testdata/7z/sfx-sample.7z, whose core/ entry is a
// dummy standing in for the real DLL. The archive is committed so the
// sevenzip extraction path is exercised without a 38 MB download; the real
// installer is covered by TestExtractD3DCompilerLive.
const (
	sampleArchive = "../../testdata/7z/sfx-sample.7z"
	sampleSHA     = "2e0a7e1cd4cd7124d0c63a5819fa7312b761f98101c882a76893bb5d71f8d7a6"
	sampleSize    = 4600
)

func TestExtractD3DCompilerFromFixture(t *testing.T) {
	dst := t.TempDir()
	want := D3DSource{DLLSHA256: sampleSHA, DLLSize: sampleSize}

	got, err := ExtractD3DCompiler(sampleArchive, dst, want)
	if err != nil {
		t.Fatalf("ExtractD3DCompiler() error = %v", err)
	}
	if filepath.Base(got) != D3DCompiler {
		t.Errorf("extracted as %q, want %q", filepath.Base(got), D3DCompiler)
	}

	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat extracted file: %v", err)
	}
	if info.Size() != sampleSize {
		t.Errorf("size = %d, want %d", info.Size(), sampleSize)
	}

	// The sibling entry must not be extracted: only core/d3dcompiler_47.dll
	// is wanted.
	if _, err := os.Stat(filepath.Join(dst, "unrelated.txt")); err == nil {
		t.Error("an unrelated archive entry was extracted")
	}
}

// A DLL failing verification must be removed, not left for a later run to
// find and trust.
func TestExtractD3DCompilerRemovesUnverified(t *testing.T) {
	dst := t.TempDir()
	want := D3DSource{DLLSHA256: sampleSHA, DLLSize: sampleSize + 1} // wrong size

	if _, err := ExtractD3DCompiler(sampleArchive, dst, want); err == nil {
		t.Fatal("want a verification error, got nil")
	}
	if _, err := os.Stat(filepath.Join(dst, D3DCompiler)); err == nil {
		t.Error("an unverified DLL was left on disk")
	}
}
