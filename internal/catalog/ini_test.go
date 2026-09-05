package catalog

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseINI(t *testing.T) {
	const input = "" +
		"[00]\r\n" + // CRLF tolerated
		"PackageName=First\r\n" +
		"EffectFiles=A.fx, B.fx ,,C.fx\r\n" +
		"Enabled=1\r\n" +
		"\r\n" +
		"# [01]\n" + // an entire commented-out block must vanish
		"# PackageName=Retired\n" +
		"# DownloadUrl32=https://example.invalid/x.addon32\n" +
		"\n" +
		"  # indented comment\n" +
		"[02]\n" +
		"PackageName=Second\n" +
		"DownloadUrl=https://example.invalid/a.zip?ref=main&x=1\n" + // '=' in value
		"Enabled=0\n" +
		"orphan-line-without-equals\n"

	got, err := ParseINI(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseINI() error = %v", err)
	}

	want := []Section{
		{Name: "00", Keys: map[string]string{
			"PackageName": "First",
			"EffectFiles": "A.fx, B.fx ,,C.fx",
			"Enabled":     "1",
		}},
		{Name: "02", Keys: map[string]string{
			"PackageName": "Second",
			"DownloadUrl": "https://example.invalid/a.zip?ref=main&x=1",
			"Enabled":     "0",
		}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ParseINI() mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff([]string{"A.fx", "B.fx", "C.fx"}, got[0].List("EffectFiles")); diff != "" {
		t.Errorf("List() mismatch (-want +got):\n%s", diff)
	}
	if !got[0].Flag("Enabled") {
		t.Error("Flag(Enabled) on \"1\" = false, want true")
	}
	if got[1].Flag("Enabled") {
		t.Error("Flag(Enabled) on \"0\" = true, want false")
	}
	if got[0].List("Missing") != nil {
		t.Error("List() on a missing key should be nil")
	}
}

// Keys before any section header have no owner and must not attach
// themselves to the first section that follows.
func TestParseINIIgnoresPreamble(t *testing.T) {
	got, err := ParseINI(strings.NewReader("Stray=value\n[00]\nPackageName=First\n"))
	if err != nil {
		t.Fatalf("ParseINI() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d sections, want 1", len(got))
	}
	if _, ok := got[0].Keys["Stray"]; ok {
		t.Error("preamble key leaked into the first section")
	}
}
