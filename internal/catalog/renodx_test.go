package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/secato/yarm/internal/game"
)

// fixtureMods parses the trimmed copy of the real release metadata. Six
// mods, chosen to cover every shape the parser has to survive: stable,
// beta, no status at all, 32-bit only, both architectures, and the one
// mod that declares Vulkan.
func fixtureMods(t *testing.T) []RenoMod {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", "catalog", "renodx-metadata.json"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	mods, err := ParseRenoDX(f)
	if err != nil {
		t.Fatalf("ParseRenoDX: %v", err)
	}
	return mods
}

func findMod(t *testing.T, mods []RenoMod, id string) RenoMod {
	t.Helper()
	for _, m := range mods {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("no mod %q in %d parsed", id, len(mods))
	return RenoMod{}
}

func TestParseRenoDXReadsTheRelease(t *testing.T) {
	mods := fixtureMods(t)

	// Six from the file, plus the three hand-kept extras.
	if got, want := len(mods), 6+3; got != want {
		t.Fatalf("parsed %d mods, want %d", got, want)
	}

	m := findMod(t, mods, "1000xresist")
	if m.Title != "1000xResist" || m.Status != "stable" || m.SteamAppID != 1675830 {
		t.Errorf("1000xresist = %+v", m)
	}
	if len(m.Maintainers) != 1 || m.Maintainers[0] != "Ritsu" {
		t.Errorf("maintainers = %v", m.Maintainers)
	}
	if len(m.Artifacts) != 1 || m.Artifacts[0].Arch != game.ArchX64 {
		t.Errorf("artifacts = %+v", m.Artifacts)
	}
	if m.Artifacts[0].Size <= 0 {
		t.Errorf("size = %d, want the real byte count", m.Artifacts[0].Size)
	}
}

// The download URL is built by yarm from the asset name, never taken from
// the metadata's own "url" field.
func TestParseRenoDXBuildsTheDownloadURL(t *testing.T) {
	m := findMod(t, fixtureMods(t), "1000xresist")

	want := "https://github.com/clshortfuse/renodx/releases/download/snapshot/renodx-1000xresist.addon64"
	if got := m.Artifacts[0].URL; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}

// A name that carries a path, or points at anything but an add-on, is
// dropped. This is the whole defense against a downloaded file choosing
// what gets downloaded next — and against the 13 MB .pdb symbols that sit
// in the same release.
func TestParseRenoDXRefusesUnsafeArtifacts(t *testing.T) {
	tests := []struct {
		name     string
		artifact string
	}{
		{"parent directory", "../../../evil.addon64"},
		{"absolute path", "/etc/passwd.addon64"},
		// Escaped for the JSON document below; the parser sees sub\evil.addon64.
		{"windows separator", `sub\\evil.addon64`},
		{"debug symbols", "renodx-cp2077.pdb"},
		{"developer tool", "decomp.exe"},
		{"no extension", "renodx-cp2077"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := `{"schema_version":"1.0.0","mods":[{"id":"x","title":"X","artifacts":[
				{"name":"` + tt.artifact + `","arch":"x64","size":1}]}]}`

			mods, err := ParseRenoDX(strings.NewReader(doc))
			if err != nil {
				t.Fatalf("ParseRenoDX: %v", err)
			}
			// The mod loses its only artifact and with it its row: there
			// would be nothing to install.
			if m := modByID(mods, "x"); m != nil {
				t.Errorf("mod survived with artifacts %+v", m.Artifacts)
			}
		})
	}
}

func modByID(mods []RenoMod, id string) *RenoMod {
	for i := range mods {
		if mods[i].ID == id {
			return &mods[i]
		}
	}
	return nil
}

// Schema drift is reported rather than guessed at, so the caller can log
// it and carry on with the rest of the catalog.
func TestParseRenoDXRejectsAnUnknownSchema(t *testing.T) {
	_, err := ParseRenoDX(strings.NewReader(`{"schema_version":"2.0.0","mods":[]}`))

	var schemaErr UnsupportedSchemaError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("error = %v, want UnsupportedSchemaError", err)
	}
	if schemaErr.Version != "2.0.0" {
		t.Errorf("Version = %q", schemaErr.Version)
	}
}

// A newer minor of the same major is fine, and so is no version at all —
// the field is young enough that its absence means an older generator,
// not a different format.
func TestParseRenoDXAcceptsCompatibleSchemas(t *testing.T) {
	for _, v := range []string{`"1.4.0"`, `"1.0.0"`, `""`} {
		doc := `{"schema_version":` + v + `,"mods":[{"id":"x","title":"X","artifacts":[
			{"name":"renodx-x.addon64","arch":"x64","size":1}]}]}`
		if _, err := ParseRenoDX(strings.NewReader(doc)); err != nil {
			t.Errorf("schema_version %s: %v", v, err)
		}
	}
}

// Unknown fields must not break parsing: upstream generates this file
// from a script in its own repo and adds to it.
func TestParseRenoDXIgnoresUnknownFields(t *testing.T) {
	doc := `{"schema_version":"1.0.0","build":{"host":"x"},"stats":{"mods_count":1},
		"mods":[{"id":"x","title":"X","tomorrows_field":42,
		"deploy":{"steam_appid":1,"nexus_mods_game_id":9,"platform":"steam"},
		"artifacts":[{"name":"renodx-x.addon64","arch":"x64","size":1,"path":"./renodx-x.addon64"}]}]}`

	mods, err := ParseRenoDX(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("ParseRenoDX: %v", err)
	}
	if m := modByID(mods, "x"); m == nil || m.SteamAppID != 1 {
		t.Errorf("mod = %+v", m)
	}
}

// The architecture match is exact. An add-on of the wrong architecture is
// not degraded, it is inert — the loader rejects it and nothing surfaces —
// so offering one would be worse than offering nothing.
func TestArtifactForDoesNotFallBackAcrossArchitectures(t *testing.T) {
	mods := fixtureMods(t)

	x64Only := findMod(t, mods, "1000xresist")
	if _, ok := x64Only.ArtifactFor(game.ArchX86); ok {
		t.Error("a 64-bit-only mod offered something for a 32-bit game")
	}
	if _, ok := x64Only.ArtifactFor(game.ArchX64); !ok {
		t.Error("a 64-bit-only mod offered nothing for a 64-bit game")
	}

	x86Only := findMod(t, mods, "alienisolation")
	if _, ok := x86Only.ArtifactFor(game.ArchX64); ok {
		t.Error("a 32-bit-only mod offered something for a 64-bit game")
	}

	both := findMod(t, mods, "dishonored")
	for _, arch := range []game.Arch{game.ArchX86, game.ArchX64} {
		a, ok := both.ArtifactFor(arch)
		if !ok {
			t.Fatalf("dual-arch mod offered nothing for %s", arch)
		}
		if a.Arch != arch {
			t.Errorf("ArtifactFor(%s) returned the %s build", arch, a.Arch)
		}
	}
}

// An unknown architecture is the one case that resolves to 64-bit, which
// is the right guess for anything modern and matches Addon.SourceFor.
func TestArtifactForTreatsUnknownAs64Bit(t *testing.T) {
	m := findMod(t, fixtureMods(t), "1000xresist")

	a, ok := m.ArtifactFor(game.ArchUnknown)
	if !ok || a.Arch != game.ArchX64 {
		t.Errorf("ArtifactFor(unknown) = %+v, %v", a, ok)
	}
}

func TestRenoModStatusHelpers(t *testing.T) {
	mods := fixtureMods(t)

	if m := findMod(t, mods, "animalwell"); !m.Beta() {
		t.Errorf("animalwell status = %q, want beta", m.Status)
	}
	if m := findMod(t, mods, "acecombat7"); !m.Unverified() {
		t.Errorf("acecombat7 status = %q, want empty", m.Status)
	}
	if m := findMod(t, mods, "1000xresist"); m.Beta() || m.Unverified() {
		t.Errorf("a stable mod reported as beta/unverified: %+v", m)
	}
}

// yarm cannot install into a Vulkan game at all, so the one mod that
// declares it has to be refusable before anything is downloaded.
func TestVulkanModIsMarkedUnsupported(t *testing.T) {
	mods := fixtureMods(t)

	if got := findMod(t, mods, "rdr2vk").Unsupported(); got != "Vulkan" {
		t.Errorf("rdr2vk Unsupported() = %q, want %q", got, "Vulkan")
	}
	if got := findMod(t, mods, "1000xresist").Unsupported(); got != "" {
		t.Errorf("a normal mod reported unsupported: %q", got)
	}
}

// The extras are the mods upstream builds but does not index, because
// they apply to any game rather than to one.
func TestExtrasAreOfferedForBothArchitectures(t *testing.T) {
	mods := fixtureMods(t)

	for _, id := range []string{"generic", "dlssfix", "fpslimiter"} {
		m := findMod(t, mods, id)
		if m.Description == "" {
			t.Errorf("%s has no description; it is the only thing explaining it", id)
		}
		for _, arch := range []game.Arch{game.ArchX86, game.ArchX64} {
			a, ok := m.ArtifactFor(arch)
			if !ok {
				t.Fatalf("%s offers nothing for %s", id, arch)
			}
			if !strings.HasPrefix(a.URL, RenoDXAssetBase) {
				t.Errorf("%s URL = %q", id, a.URL)
			}
		}
	}
	// The development kit is deliberately not among them.
	if m := modByID(mods, "devkit"); m != nil {
		t.Error("renodx-devkit is a development tool and must not be offered")
	}
}
