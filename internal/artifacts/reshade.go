// Package artifacts turns downloaded files into normalized cache entries:
// ReShade DLLs out of a setup executable, effect packages out of
// repository zips, add-on binaries, and d3dcompiler_47.dll out of a
// Firefox installer.
package artifacts

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/secato/yarm/internal/archive"
)

// ReShade DLL names inside the setup executable.
const (
	ReShade32 = "ReShade32.dll"
	ReShade64 = "ReShade64.dll"
)

// ExtractReShade pulls ReShade32.dll and ReShade64.dll out of a
// ReShade_Setup executable into dstDir.
//
// The setup exe is a .NET PE with a zip appended. Go's
// archive/zip.NewReader reads it directly: the central directory records
// offsets relative to the start of the archive, and the reader derives the
// base offset itself, so the PE prefix needs no special handling.
//
// Both DLLs must be present. A setup exe missing either one is not
// something to install half of.
func ExtractReShade(setupExe, dstDir string) error {
	f, err := os.Open(setupExe)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return fmt.Errorf("read appended zip in %s: %w", filepath.Base(setupExe), err)
	}

	entries := archive.FromZip(zr)
	limits := archive.DefaultLimits()
	if err := archive.Precheck(entries, limits); err != nil {
		return err
	}
	budget := archive.NewBudget(limits)

	found := make(map[string]archive.Entry, 2)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := filepath.Base(strings.ReplaceAll(e.Name(), `\`, "/"))
		switch {
		case strings.EqualFold(base, ReShade32):
			found[ReShade32] = e
		case strings.EqualFold(base, ReShade64):
			found[ReShade64] = e
		}
	}

	for _, name := range []string{ReShade32, ReShade64} {
		if _, ok := found[name]; !ok {
			return fmt.Errorf("%s does not contain %s", filepath.Base(setupExe), name)
		}
	}

	for name, e := range found {
		dst, err := archive.SafePath(dstDir, name)
		if err != nil {
			return err
		}
		if err := budget.ExtractEntry(e, dst); err != nil {
			return fmt.Errorf("extract %s: %w", name, err)
		}
	}

	return nil
}

// DLLFor returns the ReShade DLL name matching a game's architecture.
func DLLFor(x64 bool) string {
	if x64 {
		return ReShade64
	}
	return ReShade32
}
