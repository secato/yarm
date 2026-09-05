package game

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

// maxScanDepth limits how many directory levels below the game root are
// searched for executables (docs/plan/04-external-sources.md §4.6).
const maxScanDepth = 3

// skipExeName matches filenames that are almost never the game itself:
// uninstallers, redistributable/anti-cheat/setup installers, etc.
var skipExeName = regexp.MustCompile(`(?i)^(unins.*|.*crash.*|.*report.*|dxsetup|vc_?redist.*|.*easyanticheat.*|.*setup.*|.*installer.*|dotnet.*|ue4prereq.*|uninstall.*)\.exe$`)

// skipDirNames are directory names (matched case-insensitively, as a whole
// path component) that hold redistributables/tools rather than game code.
var skipDirNames = map[string]bool{
	"_commonredist": true,
	"redist":        true,
	"easyanticheat": true,
	"support":       true,
}

// skipDirSequences are multi-component path suffixes to skip, matched
// case-insensitively against adjacent path components.
var skipDirSequences = [][]string{
	{"engine", "extras"},
}

// Scan walks root up to maxScanDepth directories deep, collecting *.exe
// files. Directories matching skipDirNames/skipDirSequences are not
// descended into at all (redistributables, anti-cheat installers, etc. are
// never worth surfacing). Executables whose *filename* matches a common
// non-game pattern are still returned, flagged Skipped, so a caller can
// offer a "show all" toggle
// instead of silently hiding them.
func Scan(root string) ([]Executable, error) {
	var out []Executable

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		depth := strings.Count(filepath.ToSlash(rel), "/") + 1

		if d.IsDir() {
			if isSkippedDir(rel) || depth >= maxScanDepth {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.EqualFold(filepath.Ext(d.Name()), ".exe") {
			return nil
		}

		out = append(out, Executable{
			Path:    rel,
			Skipped: skipExeName.MatchString(d.Name()),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

// isSkippedDir reports whether rel (a slash- or OS-separated relative path)
// contains a directory component known to hold non-game files.
func isSkippedDir(rel string) bool {
	if rel == "." {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")

	for _, p := range parts {
		if skipDirNames[strings.ToLower(p)] {
			return true
		}
	}

	for _, seq := range skipDirSequences {
		for i := 0; i+len(seq) <= len(parts); i++ {
			match := true
			for j, want := range seq {
				if !strings.EqualFold(parts[i+j], want) {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}

	return false
}
