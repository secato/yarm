package catalog

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// minReShadeMajor is the oldest major version YARM offers. Setup
// executables before 5.0.0 are not reliably zip-appended, so the
// extraction path in internal/artifacts cannot be trusted for them.
const minReShadeMajor = 5

// downloadURLRe matches the setup links on reshade.me's front page, e.g.
// "/downloads/ReShade_Setup_6.8.0_Addon.exe".
var downloadURLRe = regexp.MustCompile(`/downloads/ReShade_Setup_(\d+\.\d+\.\d+)(_Addon)?\.exe`)

// Version is a ReShade release offered to the user.
type Version struct {
	// Version is the bare semver string, e.g. "6.8.0".
	Version string
	// Latest marks the release currently advertised on reshade.me.
	Latest bool
}

// SetupURL returns the download URL for this version in the given flavor.
func SetupURL(version string, addon bool) string {
	suffix := ""
	if addon {
		suffix = "_Addon"
	}
	return fmt.Sprintf("https://reshade.me/downloads/ReShade_Setup_%s%s.exe", version, suffix)
}

// parseLatestVersion extracts the newest version advertised on the
// reshade.me front page. Both the normal and Addon links point at the same
// release, so the first match is enough.
func parseLatestVersion(html []byte) (string, error) {
	m := downloadURLRe.FindSubmatch(html)
	if m == nil {
		return "", fmt.Errorf("no ReShade_Setup download link found on reshade.me")
	}
	return string(m[1]), nil
}

// githubTag is the subset of the GitHub tags API response we use.
type githubTag struct {
	Name string `json:"name"`
}

// parseTags reads a GitHub tags response and returns the versions at or
// above minReShadeMajor, newest first.
//
// A single per_page=100 request is enough: the repository's 100 most
// recent tags currently reach back to v0.18.2, far below the 5.0.0 floor,
// so no pagination is needed to see every offered version.
func parseTags(r io.Reader) ([]string, error) {
	var tags []githubTag
	if err := json.NewDecoder(r).Decode(&tags); err != nil {
		return nil, fmt.Errorf("decode github tags: %w", err)
	}

	versions := make([]string, 0, len(tags))
	for _, t := range tags {
		v := strings.TrimPrefix(t.Name, "v")
		major, _, _, ok := parseSemver(v)
		if !ok || major < minReShadeMajor {
			continue
		}
		versions = append(versions, v)
	}

	slices.SortFunc(versions, func(a, b string) int { return compareSemver(b, a) })
	return slices.Compact(versions), nil
}

// parseSemver splits "6.8.0" into its numeric parts.
func parseSemver(v string) (major, minor, patch int, ok bool) {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, 0, 0, false
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], true
}

// compareSemver orders two dotted versions numerically, so 6.10.0 sorts
// above 6.9.0 where a string comparison would not.
func compareSemver(a, b string) int {
	aMaj, aMin, aPat, aOK := parseSemver(a)
	bMaj, bMin, bPat, bOK := parseSemver(b)
	if !aOK || !bOK {
		return strings.Compare(a, b)
	}
	if c := cmp.Compare(aMaj, bMaj); c != 0 {
		return c
	}
	if c := cmp.Compare(aMin, bMin); c != 0 {
		return c
	}
	return cmp.Compare(aPat, bPat)
}
