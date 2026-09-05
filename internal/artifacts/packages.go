package artifacts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/secato/yarm/internal/archive"
	"github.com/secato/yarm/internal/fsutil"
)

// MetaFile is written beside a normalized package's files, recording the
// install rules that came from the catalog so the install engine does not
// need the catalog again.
const MetaFile = "package.json"

// Normalized subdirectories inside a cached package entry.
const (
	ShadersDir  = "Shaders"
	TexturesDir = "Textures"
)

// maxPackageDepth is how deep inside a package zip the Shaders/Textures
// directories are looked for (§4.2: depth <= 2, allowing for the GitHub
// wrapper directory).
const maxPackageDepth = 2

// PackageMeta is the contents of package.json.
type PackageMeta struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// EffectFiles, when non-empty, restricts installation to these .fx
	// files plus every .fxh header.
	EffectFiles []string `json:"effect_files,omitempty"`
	// DenyEffectFiles are never installed.
	DenyEffectFiles []string `json:"deny_effect_files,omitempty"`
	SourceURL       string   `json:"source_url"`
}

// NormalizePackage extracts a downloaded effect-package zip into dstDir as
// Shaders/ and Textures/, writing MetaFile alongside.
//
// Repository layouts vary: most wrap everything in a single
// "<repo>-<branch>/" directory and carry Shaders/ and Textures/ inside it,
// but some publish .fx files at the top level with no Shaders/ directory
// at all. Both are normalized to the same shape so the install engine only
// ever sees one layout.
func NormalizePackage(zipPath, dstDir string, meta PackageMeta) error {
	f, err := os.Open(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	zr, err := zipReader(f, info.Size())
	if err != nil {
		return err
	}
	entries := archive.FromZip(zr)

	limits := archive.DefaultLimits()
	if err := archive.Precheck(entries, limits); err != nil {
		return err
	}
	budget := archive.NewBudget(limits)

	shadersRoot := archive.FindDir(entries, ShadersDir, maxPackageDepth)
	texturesRoot := archive.FindDir(entries, TexturesDir, maxPackageDepth)

	var copied int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ReplaceAll(e.Name(), `\`, "/")

		rel, ok := packageDest(name, shadersRoot, texturesRoot)
		if !ok {
			continue
		}

		dst, err := archive.SafePath(dstDir, rel)
		if err != nil {
			// A traversing entry is skipped, not fatal: the rest of the
			// package is still usable and the entry is logged upstream.
			continue
		}
		if err := budget.ExtractEntry(e, dst); err != nil {
			return fmt.Errorf("extract %s: %w", name, err)
		}
		copied++
	}

	if copied == 0 {
		return fmt.Errorf("no shader files found in %s", filepath.Base(zipPath))
	}

	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(filepath.Join(dstDir, MetaFile), raw, 0o644)
}

// packageDest maps an archive entry to its normalized destination, or
// reports that the entry is not part of the package's shader content.
func packageDest(name, shadersRoot, texturesRoot string) (string, bool) {
	if shadersRoot != "" && isUnder(name, shadersRoot) {
		return ShadersDir + "/" + archive.StripPrefix(name, shadersRoot), true
	}
	if texturesRoot != "" && isUnder(name, texturesRoot) {
		return TexturesDir + "/" + archive.StripPrefix(name, texturesRoot), true
	}

	// No Shaders/ directory in the archive: treat loose .fx/.fxh files as
	// the shader set, flattened.
	if shadersRoot == "" && isShaderFile(name) {
		return ShadersDir + "/" + filepath.Base(name), true
	}
	return "", false
}

// isUnder reports whether an archive entry lives inside dir.
func isUnder(name, dir string) bool {
	return strings.HasPrefix(name, dir+"/")
}

// isShaderFile reports whether a name is a ReShade effect or header.
func isShaderFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".fx", ".fxh":
		return true
	default:
		return false
	}
}
