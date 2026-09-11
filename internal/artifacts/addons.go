package artifacts

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/secato/yarm/internal/archive"
	"github.com/secato/yarm/internal/fsutil"
	"github.com/secato/yarm/internal/game"
)

// Add-on file extensions. ReShade loads *.addon32 / *.addon64 from the
// directory holding its DLL.
const (
	ExtAddon32 = ".addon32"
	ExtAddon64 = ".addon64"
	// ExtAddon is the unsuffixed form one catalog entry uses; it is
	// architecture-specific despite the name, so the catalog's own
	// per-arch URL decides which architecture it belongs to.
	ExtAddon = ".addon"
)

// InstallAddonFile places a directly-downloaded add-on binary into dstDir
// under a name ReShade will load.
//
// A URL ending in a bare ".addon" is renamed to the suffix matching the
// architecture it was published for, since ReShade only scans for
// .addon32/.addon64.
func InstallAddonFile(src, dstDir string, arch game.Arch) (string, error) {
	name := filepath.Base(src)
	if strings.EqualFold(filepath.Ext(name), ExtAddon) {
		name = strings.TrimSuffix(name, filepath.Ext(name)) + addonExt(arch)
	}

	dst, err := archive.SafePath(dstDir, name)
	if err != nil {
		return "", err
	}
	if _, _, err := fsutil.CopyHashed(dst, src); err != nil {
		return "", err
	}
	return dst, nil
}

// ExtractAddonZip pulls every .addon32/.addon64 matching arch out of a
// downloaded zip into dstDir, returning the files written.
//
// An archive with no add-on files at all is reported as an error: the
// catalog classifies those entries as manual ("see repository"), so
// reaching here with one means the catalog promised something the archive
// does not deliver.
func ExtractAddonZip(zipPath, dstDir string, arch game.Arch) ([]string, error) {
	f, err := os.Open(zipPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	zr, err := zipReader(f, info.Size())
	if err != nil {
		return nil, err
	}

	entries := archive.FromZip(zr)
	limits := archive.DefaultLimits()
	if err := archive.Precheck(entries, limits); err != nil {
		return nil, err
	}
	budget := archive.NewBudget(limits)

	want := addonExt(arch)
	var (
		written    []string
		sawAny     bool
		unsafeName int
	)

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ExtAddon32 && ext != ExtAddon64 {
			continue
		}
		sawAny = true
		if ext != want {
			continue
		}

		// Add-on binaries go directly beside the ReShade DLL: any
		// directory structure inside the zip is discarded.
		dst, err := archive.SafePath(dstDir, filepath.Base(strings.ReplaceAll(e.Name(), `\`, "/")))
		if err != nil {
			unsafeName++
			continue
		}
		if err := budget.ExtractEntry(e, dst); err != nil {
			return nil, fmt.Errorf("extract %s: %w", e.Name(), err)
		}
		written = append(written, dst)
	}

	// Reported once, not per entry: worth seeing, but a hostile archive
	// must not get to flood the log with it.
	if unsafeName > 0 {
		slog.Warn("skipped add-on entries whose name could not be placed",
			"archive", filepath.Base(zipPath), "count", unsafeName)
	}

	if !sawAny {
		return nil, fmt.Errorf("%s contains no .addon32/.addon64 files", filepath.Base(zipPath))
	}
	if len(written) == 0 {
		return nil, fmt.Errorf("%s has no %s build", filepath.Base(zipPath), want)
	}
	return written, nil
}

// addonExt returns the add-on extension ReShade loads for an
// architecture. Unknown resolves to 64-bit, matching catalog.SourceFor.
func addonExt(arch game.Arch) string {
	if arch == game.ArchX86 {
		return ExtAddon32
	}
	return ExtAddon64
}
