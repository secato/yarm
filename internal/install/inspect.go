package install

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// RuntimeInfo is what ReShade itself recorded about an install, read from
// its own files (ReShade.ini, its active preset, its log) rather than
// anything yarm wrote. Every field is best-effort and left at its zero
// value when the underlying file is missing — a log only exists once the
// game has been launched at least once with the DLL in place, and a
// preset only exists once one has been saved from the in-game overlay.
type RuntimeInfo struct {
	// Version is what ReShade's own log reported at its last startup, or
	// "" if no log was found. ReShade truncates the log on every launch
	// (verified against its source: the log file is opened with
	// CREATE_ALWAYS), so this always reflects the most recent run, never
	// a stale one from further back — but it can still be out of date if
	// the DLL was swapped without the game being relaunched since.
	Version string
	// ActiveTechniques lists the technique names the current preset has
	// enabled (deduplicated, in the order the preset lists them), or nil
	// if no preset is active.
	ActiveTechniques []string
	// AvailableEffects lists effect file names present under
	// reshade-shaders/Shaders (deduplicated, sorted), or nil if none
	// exist.
	AvailableEffects []string
}

// reshadeVersionLine matches ReShade's own startup log line, e.g.
// `Initializing crosire's ReShade version '6.8.0' (64-bit) loaded from
// 'dxgi.dll' into 'game.exe' ...` (verified against
// github.com/crosire/reshade's dll_main.cpp).
var reshadeVersionLine = regexp.MustCompile(`Initializing crosire's ReShade version '([^']+)'`)

// InspectRuntime gathers RuntimeInfo for root/exePath's directory. It
// never errors — a missing or unreadable file just leaves that field
// empty, since these are files ReShade itself writes, not yarm, and may
// simply not exist yet.
func InspectRuntime(root, exePath string) RuntimeInfo {
	dir := filepath.Join(root, filepath.FromSlash(ExeDir(exePath)))

	return RuntimeInfo{
		Version:          readReshadeVersion(filepath.Join(dir, LogName)),
		ActiveTechniques: readActiveTechniques(dir),
		AvailableEffects: listEffectFiles(filepath.Join(dir, filepath.FromSlash(ShadersDir))),
	}
}

func readReshadeVersion(logPath string) string {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	m := reshadeVersionLine.FindSubmatch(data)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// readActiveTechniques follows ReShade.ini's own GENERAL/PresetPath to the
// active preset and reads its Techniques list — the effects ReShade's
// overlay actually has switched on, not just what happens to be present
// on disk.
func readActiveTechniques(dir string) []string {
	ini, err := loadReshadeINI(filepath.Join(dir, ININame))
	if err != nil {
		return nil
	}
	presetRel, ok := ini.get("GENERAL", "PresetPath")
	if !ok || presetRel == "" || isAbsoluteish(presetRel) {
		return nil
	}
	presetPath := filepath.Join(dir, filepath.FromSlash(strings.ReplaceAll(presetRel, `\`, "/")))

	preset, err := loadReshadeINI(presetPath)
	if err != nil {
		return nil
	}
	// Techniques lives in the preset's unnamed section — the keys ReShade
	// writes before the first "[EffectFile.fx]" header.
	raw, ok := preset.get("", "Techniques")
	if !ok {
		return nil
	}

	seen := map[string]bool{}
	var out []string
	for _, entry := range splitReshadeList(raw) {
		// Each entry is "TechniqueName@EffectFile.fx"; the name alone is
		// what ReShade's own UI shows as the toggleable effect.
		name, _, _ := strings.Cut(entry, "@")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func listEffectFiles(shadersDir string) []string {
	seen := map[string]bool{}
	var out []string
	_ = walkFiles(shadersDir, func(rel string, _ int64) error {
		if strings.EqualFold(filepath.Ext(rel), ".fx") {
			name := filepath.Base(rel)
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// isAbsoluteish reports whether p looks like an absolute path on either
// Windows (a drive letter) or a rooted one — ReShade always writes
// PresetPath relative to its own directory (confirmed against its
// source's make_relative_path() call at save time), but a manually edited
// config could hold an absolute Windows path yarm has no way to resolve
// from a Linux or macOS host.
func isAbsoluteish(p string) bool {
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return true
	}
	return len(p) >= 2 && p[1] == ':'
}

// reshadeINI is a minimal reader for ReShade's own ini dialect: an unnamed
// section (empty-string key) for anything written before the first
// "[Section]" header — where ReShade keeps a preset's "Techniques" list —
// plus named sections otherwise, and comma-separated list values where a
// literal comma is escaped as two commas in a row. This is a different
// dialect from the one internal/catalog parses for upstream sources (which
// drops unnamed-section keys entirely and does not escape commas), so it
// is not reused here.
type reshadeINI struct {
	sections map[string]map[string]string
}

func loadReshadeINI(path string) (reshadeINI, error) {
	f, err := os.Open(path)
	if err != nil {
		return reshadeINI{}, err
	}
	defer func() { _ = f.Close() }()

	ini := reshadeINI{sections: map[string]map[string]string{"": {}}}
	cur := ""

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			cur = line[1 : len(line)-1]
			if _, ok := ini.sections[cur]; !ok {
				ini.sections[cur] = map[string]string{}
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		ini.sections[cur][strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return ini, sc.Err()
}

func (ini reshadeINI) get(section, key string) (string, bool) {
	v, ok := ini.sections[section][key]
	return v, ok
}

// splitReshadeList splits a comma-separated ini value using ReShade's own
// escaping rule (verified against its ini_file.cpp): a literal comma is
// written as two commas in a row, so a naive split on every comma would
// cut an entry in half.
func splitReshadeList(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	var cur strings.Builder
	r := []rune(raw)
	for i := 0; i < len(r); i++ {
		if r[i] == ',' {
			if i+1 < len(r) && r[i+1] == ',' {
				cur.WriteRune(',')
				i++
				continue
			}
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r[i])
	}
	out = append(out, cur.String())
	return out
}
