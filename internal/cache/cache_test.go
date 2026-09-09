package cache

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/fetch"
	"github.com/secato/yarm/internal/game"
)

// fakeSetup builds a ReShade-setup-shaped payload: PE prefix + appended zip.
func fakeSetup(t *testing.T) []byte {
	t.Helper()
	prefix := make([]byte, 2048)
	_, _ = rand.Read(prefix)
	copy(prefix, []byte("MZ"))

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range []string{artifacts.ReShade32, artifacts.ReShade64} {
		w, _ := zw.Create(n)
		_, _ = io.WriteString(w, n+" body")
	}
	_ = zw.Close()
	return append(prefix, buf.Bytes()...)
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// newCache wires a Cache to a test server and a fixed clock.
func newCache(t *testing.T, handler http.HandlerFunc) (*Cache, *httptest.Server, *int64) {
	t.Helper()
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	f := &fetch.Client{HTTP: srv.Client(), UserAgent: "yarm/test", Retries: 1, Backoff: time.Millisecond}
	c := New(t.TempDir(), f)
	c.Now = func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }
	return c, srv, &hits
}

func TestEnsureReShade(t *testing.T) {
	payload := fakeSetup(t)
	c, srv, hits := newCache(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	c.SetupURL = func(version string, addon bool) string {
		return srv.URL + "/ReShade_Setup_" + version + ".exe"
	}

	dir, err := c.EnsureReShade(context.Background(), "6.8.0", true, nil)
	if err != nil {
		t.Fatalf("EnsureReShade() error = %v", err)
	}

	for _, n := range []string{artifacts.ReShade32, artifacts.ReShade64} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Errorf("%s missing from the cache entry: %v", n, err)
		}
	}
	if want := filepath.Join(c.Root, DirReShade, "6.8.0", FlavorAddon); dir != want {
		t.Errorf("cache dir = %q, want %q", dir, want)
	}

	// The setup executable is not kept.
	downloads, _ := os.ReadDir(filepath.Join(c.Root, DirDownloads))
	for _, e := range downloads {
		if strings.HasSuffix(e.Name(), ".exe") {
			t.Errorf("setup executable %s was left in the cache", e.Name())
		}
	}

	// A second call must be served from disk.
	before := *hits
	if _, err := c.EnsureReShade(context.Background(), "6.8.0", true, nil); err != nil {
		t.Fatalf("second EnsureReShade() error = %v", err)
	}
	if *hits != before {
		t.Errorf("second call made %d more requests, want 0", *hits-before)
	}

	entries, err := c.List(SortByName, false)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("index has %d entries, want 1", len(entries))
	}
	if entries[0].Kind != KindReShade || entries[0].Size == 0 {
		t.Errorf("entry = %+v, want a sized reshade entry", entries[0])
	}
}

func TestEnsurePackage(t *testing.T) {
	body := zipBytes(t, map[string]string{
		"repo-main/Shaders/A.fx":   "// a",
		"repo-main/Textures/t.png": "png",
		"repo-main/README.md":      "readme",
	})
	c, srv, hits := newCache(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})

	pkg := catalog.Package{
		ID:          "standard-effects",
		Name:        "Standard effects",
		DownloadURL: srv.URL + "/slim.zip",
		EffectFiles: []string{"A.fx"},
	}

	dir, err := c.EnsurePackage(context.Background(), pkg, nil)
	if err != nil {
		t.Fatalf("EnsurePackage() error = %v", err)
	}

	for _, rel := range []string{"Shaders/A.fx", "Textures/t.png", artifacts.MetaFile} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s missing: %v", rel, err)
		}
	}

	// The version key is the download date plus a short content hash, so
	// the same content on the same day reuses the directory.
	base := filepath.Base(dir)
	if !strings.HasPrefix(base, "20260905-") {
		t.Errorf("version key = %q, want a 20260905- prefix", base)
	}

	if !c.HasPackage(pkg.ID) {
		t.Error("HasPackage() = false right after EnsurePackage cached it")
	}
	if c.HasPackage("some-other-package") {
		t.Error("HasPackage() = true for a package never cached")
	}

	before := *hits
	dir2, err := c.EnsurePackage(context.Background(), pkg, nil)
	if err != nil {
		t.Fatalf("second EnsurePackage() error = %v", err)
	}
	if dir2 != dir {
		t.Errorf("second call gave %q, want the same dir %q", dir2, dir)
	}
	// The zip is re-downloaded to compute its hash, but not re-extracted.
	if *hits-before != 1 {
		t.Errorf("second call made %d requests, want 1", *hits-before)
	}
}

func TestEnsureAddon(t *testing.T) {
	tests := []struct {
		name     string
		addon    catalog.Addon
		body     func(t *testing.T) []byte
		arch     game.Arch
		wantFile string
	}{
		{
			name:     "direct addon file",
			addon:    catalog.Addon{ID: "swapchain", Name: "Swap chain"},
			body:     func(t *testing.T) []byte { return []byte("addon binary") },
			arch:     game.ArchX64,
			wantFile: "swapchain.addon64",
		},
		{
			name:  "zip archive",
			addon: catalog.Addon{ID: "rest", Name: "REST"},
			body: func(t *testing.T) []byte {
				return zipBytes(t, map[string]string{"release/rest.addon64": "bin"})
			},
			arch:     game.ArchX64,
			wantFile: "rest.addon64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := tt.body(t)
			c, srv, _ := newCache(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write(payload)
			})

			addon := tt.addon
			if strings.Contains(tt.name, "zip") {
				addon.URL64 = srv.URL + "/release.zip"
			} else {
				addon.URL64 = srv.URL + "/" + addon.ID + ".addon64"
			}

			dir, err := c.EnsureAddon(context.Background(), addon, tt.arch, nil)
			if err != nil {
				t.Fatalf("EnsureAddon() error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, tt.wantFile)); err != nil {
				t.Errorf("%s missing: %v", tt.wantFile, err)
			}
			if !c.HasAddon(addon.ID) {
				t.Error("HasAddon() = false right after EnsureAddon cached it")
			}
		})
	}
}

// An add-on with no build for the game's architecture must say so rather
// than download something wrong.
func TestEnsureAddonMissingArch(t *testing.T) {
	c, srv, _ := newCache(t, func(w http.ResponseWriter, r *http.Request) {})
	addon := catalog.Addon{ID: "igcs", Name: "IGCS", URL64: srv.URL + "/x.zip"}

	if _, err := c.EnsureAddon(context.Background(), addon, game.ArchX86, nil); err == nil {
		t.Error("want an error for an addon with no 32-bit build, got nil")
	}
}

func TestListSortAndDelete(t *testing.T) {
	c, _, _ := newCache(t, func(w http.ResponseWriter, r *http.Request) {})

	// Register three entries with distinct sizes.
	for i, spec := range []struct {
		id   string
		kind Kind
		size int
	}{
		{"a", KindPackage, 300},
		{"b", KindAddon, 100},
		{"c", KindReShade, 200},
	} {
		rel := filepath.Join("test", spec.id)
		if err := os.MkdirAll(c.abs(rel), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(c.abs(rel), "f.bin"), make([]byte, spec.size), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := c.record(Entry{ID: spec.id, Kind: spec.kind, Path: rel, Name: spec.id}); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	bySize, err := c.List(SortBySize, false)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(bySize) != 3 {
		t.Fatalf("got %d entries, want 3", len(bySize))
	}
	if bySize[0].ID != "b" || bySize[2].ID != "a" {
		t.Errorf("size order = %s,%s,%s; want b,c,a", bySize[0].ID, bySize[1].ID, bySize[2].ID)
	}

	desc, _ := c.List(SortBySize, true)
	if desc[0].ID != "a" {
		t.Errorf("descending order starts with %s, want a", desc[0].ID)
	}

	if err := c.Delete("b"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(c.abs(filepath.Join("test", "b"))); err == nil {
		t.Error("deleted entry's directory still exists")
	}
	after, _ := c.List(SortByName, false)
	if len(after) != 2 {
		t.Errorf("after delete: %d entries, want 2", len(after))
	}
	if err := c.Delete("nonexistent"); err == nil {
		t.Error("deleting an unknown id should error")
	}
}

// An entry removed outside YARM must not linger in the index.
func TestListDropsVanishedEntries(t *testing.T) {
	c, _, _ := newCache(t, func(w http.ResponseWriter, r *http.Request) {})

	rel := filepath.Join("test", "gone")
	if err := os.MkdirAll(c.abs(rel), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := c.record(Entry{ID: "gone", Kind: KindPackage, Path: rel}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := os.RemoveAll(c.abs(rel)); err != nil {
		t.Fatalf("remove: %v", err)
	}

	entries, err := c.List(SortByName, false)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0 after the directory vanished", len(entries))
	}
}

// A corrupt index must not brick the cache.
func TestLoadIndexSurvivesCorruption(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, IndexFile), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	idx, err := loadIndex(root)
	if err != nil {
		t.Fatalf("loadIndex() error = %v", err)
	}
	if idx.Entries == nil || len(idx.Entries) != 0 {
		t.Errorf("want an empty usable index, got %+v", idx)
	}
}

// An index from a newer YARM must be refused rather than misread.
func TestLoadIndexRejectsNewerSchema(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, IndexFile),
		[]byte(`{"schema":99,"entries":{}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadIndex(root); err == nil {
		t.Error("want an error for a newer schema, got nil")
	}
}

// Delete must never touch anything outside the cache root, however the
// index came to hold such a path.
func TestDeleteRefusesEscape(t *testing.T) {
	c, _, _ := newCache(t, func(w http.ResponseWriter, r *http.Request) {})

	victim := filepath.Join(t.TempDir(), "important")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	idx, _ := loadIndex(c.Root)
	idx.Entries["evil"] = Entry{ID: "evil", Kind: KindPackage, Path: filepath.Join("..", "..", "important")}
	if err := saveIndex(c.Root, idx); err != nil {
		t.Fatalf("saveIndex: %v", err)
	}

	if err := c.Delete("evil"); err == nil {
		t.Error("Delete() should refuse a path outside the cache root")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("a directory outside the cache root was deleted: %v", err)
	}
}

func TestClean(t *testing.T) {
	c, _, _ := newCache(t, func(w http.ResponseWriter, r *http.Request) {})

	for _, id := range []string{"a", "b"} {
		rel := filepath.Join("test", id)
		if err := os.MkdirAll(c.abs(rel), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(c.abs(rel), "f"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := c.record(Entry{ID: id, Kind: KindPackage, Path: rel}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	removed, err := c.Clean()
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if removed != 2 {
		t.Errorf("removed %d entries, want 2", removed)
	}
	entries, _ := c.List(SortByName, false)
	if len(entries) != 0 {
		t.Errorf("%d entries remain after Clean()", len(entries))
	}
}

// A download filename is built from a catalog-supplied URL, so the
// extension it contributes must be plain and bounded. A query string in
// particular is an invalid filename on Windows and would fail the write.
func TestURLExt(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.invalid/release.zip", ".zip"},
		{"https://example.invalid/x.addon64", ".addon64"},
		{"https://example.invalid/release.zip?token=abc", ".zip"},
		{"https://example.invalid/release.zip#frag", ".zip"},
		{"https://example.invalid/download/latest", ""},
		{"https://example.invalid/x.", ""},
		{"https://example.invalid/x.a-b", ""},               // punctuation refused
		{"https://example.invalid/x.veryverylongext11", ""}, // absurd length refused
		{"", ""},
	}
	for _, tt := range tests {
		if got := urlExt(tt.url); got != tt.want {
			t.Errorf("urlExt(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

// Whatever a catalog entry's URL looks like, the download must land inside
// the cache root.
func TestEnsureAddonKeepsDownloadInsideRoot(t *testing.T) {
	payload := zipBytes(t, map[string]string{"thing.addon64": "bin"})
	c, srv, _ := newCache(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})

	addon := catalog.Addon{
		ID:    "hostile",
		Name:  "Hostile",
		URL64: srv.URL + "/release.zip?x=/../../../../../../tmp/pwned",
	}

	dir, err := c.EnsureAddon(context.Background(), addon, game.ArchX64, nil)
	if err != nil {
		t.Fatalf("EnsureAddon() error = %v", err)
	}
	if !withinRoot(c.Root, dir) {
		t.Errorf("addon extracted to %q, outside the cache root %q", dir, c.Root)
	}
	if _, err := os.Stat(filepath.Join(dir, "thing.addon64")); err != nil {
		t.Errorf("expected file missing: %v", err)
	}
}

func TestWithinRoot(t *testing.T) {
	root := filepath.Join("/tmp", "cacheroot")
	tests := []struct {
		target string
		want   bool
	}{
		{filepath.Join(root, "packages", "x"), true},
		{filepath.Join(root, "a"), true},
		{root, false},                                   // the root itself
		{filepath.Join(root, "..", "sibling"), false},   // one level out
		{filepath.Join(root, "..", "..", "far"), false}, // the case that slipped through
		{"/etc", false},
	}
	for _, tt := range tests {
		if got := withinRoot(root, tt.target); got != tt.want {
			t.Errorf("withinRoot(%q, %q) = %v, want %v", root, tt.target, got, tt.want)
		}
	}
}

// HasReShade/HasPackage/HasAddon let a UI show a "cached" badge without
// triggering a download to find out.
func TestHasReShadeHasPackageHasAddon(t *testing.T) {
	payload := fakeSetup(t)
	c, srv, _ := newCache(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})

	if c.HasReShade("6.8.0", true) {
		t.Error("HasReShade() = true before anything was cached")
	}
	c.SetupURL = func(version string, addon bool) string {
		return srv.URL + "/ReShade_Setup_" + version + ".exe"
	}
	if _, err := c.EnsureReShade(context.Background(), "6.8.0", true, nil); err != nil {
		t.Fatalf("EnsureReShade(): %v", err)
	}
	if !c.HasReShade("6.8.0", true) {
		t.Error("HasReShade() = false after caching the addon build")
	}
	if c.HasReShade("6.8.0", false) {
		t.Error("HasReShade() = true for the normal flavor, which was never fetched")
	}
	if c.HasReShade("6.9.0", true) {
		t.Error("HasReShade() = true for a version never fetched")
	}

	if c.HasPackage("standard-effects") {
		t.Error("HasPackage() = true before anything was cached")
	}
	if c.HasAddon("swapchain") {
		t.Error("HasAddon() = true before anything was cached")
	}
}

func TestHasD3DCompiler(t *testing.T) {
	c, _, _ := newCache(t, func(w http.ResponseWriter, r *http.Request) {})

	if c.HasD3DCompiler(game.ArchX64) {
		t.Error("HasD3DCompiler() = true before anything was cached")
	}

	src, err := artifacts.D3DSourceFor(game.ArchX64)
	if err != nil {
		t.Fatalf("D3DSourceFor: %v", err)
	}
	dll := c.abs(filepath.Join(DirD3DCompiler, string(game.ArchX64), artifacts.D3DCompiler))
	if err := os.MkdirAll(filepath.Dir(dll), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dll, make([]byte, src.DLLSize), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !c.HasD3DCompiler(game.ArchX64) {
		t.Error("HasD3DCompiler() = false for a file matching the pinned size")
	}

	// A file that exists but is the wrong size (a partial or corrupt
	// download) must not read as cached.
	if err := os.WriteFile(dll, []byte("wrong size"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if c.HasD3DCompiler(game.ArchX64) {
		t.Error("HasD3DCompiler() = true for a file with the wrong size")
	}

	if c.HasD3DCompiler(game.ArchX86) {
		t.Error("HasD3DCompiler() = true for an architecture never cached")
	}
}

// A RenoDX mod is one add-on binary, cached per architecture and per day
// — the day matters because `snapshot` is a rolling tag, so the same URL
// serves different bytes over time.
func TestEnsureRenoDX(t *testing.T) {
	c, srv, hits := newCache(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("renodx binary"))
	})
	mod := catalog.RenoMod{
		ID: "cp2077", Title: "Cyberpunk 2077",
		Artifacts: []catalog.RenoArtifact{{
			Name: "renodx-cp2077.addon64", Arch: game.ArchX64,
			URL: srv.URL + "/renodx-cp2077.addon64",
		}},
	}

	dir, err := c.EnsureRenoDX(context.Background(), mod, game.ArchX64, nil)
	if err != nil {
		t.Fatalf("EnsureRenoDX() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "renodx-cp2077.addon64")); err != nil {
		t.Errorf("the add-on file is missing: %v", err)
	}
	if !c.HasRenoDX(mod.ID) {
		t.Error("HasRenoDX() = false right after EnsureRenoDX cached it")
	}
	if !strings.Contains(dir, "20260905-x64") {
		t.Errorf("dir = %q, want it stamped with the date and architecture", dir)
	}

	// Recorded in the index so the resources browser can account for it.
	entries, err := c.List(SortByName, false)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Kind == KindRenoDX {
			found = true
			if e.SourceURL != mod.Artifacts[0].URL {
				t.Errorf("SourceURL = %q", e.SourceURL)
			}
		}
	}
	if !found {
		t.Errorf("no %s entry in the index", KindRenoDX)
	}

	// A second call the same day is a cache hit, not a second download.
	before := *hits
	if _, err := c.EnsureRenoDX(context.Background(), mod, game.ArchX64, nil); err != nil {
		t.Fatalf("second EnsureRenoDX(): %v", err)
	}
	if *hits != before {
		t.Errorf("downloaded again: %d hits, want %d", *hits, before)
	}
}

// A mod with no build for this architecture must say so rather than
// download the wrong one — a mismatched add-on is inert, not degraded.
func TestEnsureRenoDXMissingArch(t *testing.T) {
	c, srv, _ := newCache(t, func(http.ResponseWriter, *http.Request) {})
	mod := catalog.RenoMod{
		ID: "x64only", Title: "64-bit only",
		Artifacts: []catalog.RenoArtifact{{
			Name: "renodx-x64only.addon64", Arch: game.ArchX64, URL: srv.URL + "/x.addon64",
		}},
	}

	if _, err := c.EnsureRenoDX(context.Background(), mod, game.ArchX86, nil); err == nil {
		t.Fatal("EnsureRenoDX() = nil error for an architecture the mod does not build")
	}
}
