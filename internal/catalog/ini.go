// Package catalog lists the things YARM can install: ReShade versions,
// effect packages, add-ons, and user-managed custom content. Network
// results are cached on disk with a TTL so the app still works offline.
package catalog

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Section is one bracketed block of a catalog ini file.
type Section struct {
	// Name is the bracketed label, e.g. "00". Upstream numbers sections
	// but the numbers are neither contiguous nor meaningful once
	// commented-out blocks are dropped, so nothing should key off them.
	Name string
	Keys map[string]string
}

// Get returns the trimmed value for key, or "" when absent.
func (s Section) Get(key string) string { return s.Keys[key] }

// Has reports whether key is present and non-empty.
func (s Section) Has(key string) bool { return s.Keys[key] != "" }

// List splits a comma-separated value, dropping empty fields.
func (s Section) List(key string) []string {
	raw := s.Keys[key]
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Flag reports whether key is set to "1". Upstream uses Enabled=1 and
// Required=1 as opt-in markers; absent means false.
func (s Section) Flag(key string) bool { return s.Keys[key] == "1" }

// ParseINI reads the catalog ini dialect used by EffectPackages.ini and
// Addons.ini: `[name]` sections of `Key=Value` lines.
//
// Lines whose first non-space character is '#' are dropped before any
// other handling. That is not cosmetic: Addons.ini keeps six retired
// entries commented out block-by-block, and treating them as data would
// surface add-ons upstream has deliberately withdrawn.
//
// Values are taken after the first '=' so URLs with query strings survive.
// Keys appearing before any section header are ignored.
func ParseINI(r io.Reader) ([]Section, error) {
	var (
		sections []Section
		cur      *Section
	)

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			sections = append(sections, Section{
				Name: strings.TrimSpace(trimmed[1 : len(trimmed)-1]),
				Keys: make(map[string]string),
			})
			cur = &sections[len(sections)-1]
			continue
		}

		key, value, ok := strings.Cut(trimmed, "=")
		if !ok || cur == nil {
			continue
		}
		cur.Keys[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read catalog ini: %w", err)
	}

	return sections, nil
}
