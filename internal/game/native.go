package game

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// nativeExts are the extensions a native game binary plausibly carries:
// none at all (Source, id Tech, and the executable inside a macOS .app
// bundle) or Unity's architecture-suffixed names.
var nativeExts = map[string]bool{
	"":        true,
	".x86_64": true,
	".x86":    true,
}

// maxNativeSniffs caps how many candidate files HasNativeBuild opens, so a
// game root full of extensionless data files cannot turn the check into a
// long series of reads.
const maxNativeSniffs = 512

// nativeMagics are the leading bytes of executable formats that are not
// Windows PE: ELF, then Mach-O (32- and 64-bit, both byte orders) and the
// Mach-O "fat" container.
var nativeMagics = [][]byte{
	{0x7f, 'E', 'L', 'F'},
	{0xcf, 0xfa, 0xed, 0xfe},
	{0xce, 0xfa, 0xed, 0xfe},
	{0xfe, 0xed, 0xfa, 0xcf},
	{0xfe, 0xed, 0xfa, 0xce},
	{0xca, 0xfe, 0xba, 0xbe},
	{0xbe, 0xba, 0xfe, 0xca},
}

// HasNativeBuild reports whether root holds a native ELF (Linux) or Mach-O
// (macOS) executable.
//
// Call it only when Scan found no .exe. ReShade works by having the Windows
// loader — the real one, or Wine's under Proton — load a proxy DLL sitting
// next to a PE executable, so a game shipping only a native binary cannot
// take ReShade at all. Telling that apart from an empty scan lets the UI
// say "native build, not applicable" rather than looking like the scan
// failed.
func HasNativeBuild(root string) bool {
	var (
		found  bool
		sniffs int
	)

	_ = walkGameDir(root, func(rel string, d fs.DirEntry) error {
		if !nativeExts[strings.ToLower(filepath.Ext(d.Name()))] {
			return nil
		}
		if sniffs >= maxNativeSniffs {
			return fs.SkipAll
		}
		sniffs++

		if isNativeExecutable(filepath.Join(root, rel)) {
			found = true
			return fs.SkipAll
		}
		return nil
	})

	return found
}

// isNativeExecutable reports whether path begins with the magic bytes of a
// non-PE executable format. Unreadable files are simply not native.
func isNativeExecutable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return false
	}

	for _, m := range nativeMagics {
		if bytes.Equal(magic[:], m) {
			return true
		}
	}
	return false
}
