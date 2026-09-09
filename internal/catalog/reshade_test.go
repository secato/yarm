package catalog

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseLatestVersion(t *testing.T) {
	const page = `<a href="/downloads/ReShade_Setup_6.8.0.exe">Download</a>
	              <a href="/downloads/ReShade_Setup_6.8.0_Addon.exe">Download with add-on support</a>`

	got, err := parseLatestVersion([]byte(page))
	if err != nil {
		t.Fatalf("parseLatestVersion() error = %v", err)
	}
	if got != "6.8.0" {
		t.Errorf("parseLatestVersion() = %q, want %q", got, "6.8.0")
	}

	if _, err := parseLatestVersion([]byte("<html>no downloads here</html>")); err == nil {
		t.Error("parseLatestVersion() on a page with no link: want error, got nil")
	}
}

func TestParseTags(t *testing.T) {
	const body = `[
		{"name":"v6.10.0"},
		{"name":"v6.9.1"},
		{"name":"v6.8.0"},
		{"name":"v5.0.0"},
		{"name":"v4.9.1"},
		{"name":"v0.18.2"},
		{"name":"not-a-version"},
		{"name":"v6.8.0"}
	]`

	got, err := parseTags(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parseTags() error = %v", err)
	}

	// Newest first, below 5.0.0 dropped, malformed dropped, deduplicated.
	// 6.10.0 must outrank 6.9.1: a string sort would get this backwards.
	want := []string{"6.10.0", "6.9.1", "6.8.0", "5.0.0"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("parseTags() mismatch (-want +got):\n%s", diff)
	}
}

func TestSetupURL(t *testing.T) {
	tests := []struct {
		version string
		addon   bool
		want    string
	}{
		{"6.8.0", false, "https://reshade.me/downloads/ReShade_Setup_6.8.0.exe"},
		{"6.8.0", true, "https://reshade.me/downloads/ReShade_Setup_6.8.0_Addon.exe"},
	}
	for _, tt := range tests {
		if got := SetupURL(tt.version, tt.addon); got != tt.want {
			t.Errorf("SetupURL(%q, %v) = %q, want %q", tt.version, tt.addon, got, tt.want)
		}
	}
}

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"6.10.0", "6.9.0", 1},
		{"6.9.0", "6.10.0", -1},
		{"6.8.0", "6.8.0", 0},
		{"7.0.0", "6.99.99", 1},
		{"6.8.1", "6.8.0", 1},
	}
	for _, tt := range tests {
		if got := CompareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
