package app

import (
	archivezip "archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/fetch"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
)

// resolveUsesMemoData checks the install pipeline resolves what the wizard
// showed rather than re-reading the catalogs: the same data, no network.
func TestResolveUsesMemoData(t *testing.T) {
	r := &RealInstaller{WizardData: fakeWizardData{data: sampleWizardData()}}

	packages, err := r.packages(context.Background())
	if err != nil {
		t.Fatalf("packages error = %v", err)
	}
	if len(packages) != 2 || packages[0].ID != "standard-effects" {
		t.Errorf("packages = %v, want the memo's two", packages)
	}

	addons, err := r.addons(context.Background())
	if err != nil {
		t.Fatalf("addons error = %v", err)
	}
	if len(addons) != 2 {
		t.Errorf("addons = %d, want the memo's two", len(addons))
	}

	mods, err := r.renoDX(context.Background())
	if err != nil {
		t.Fatalf("renodx error = %v", err)
	}
	if len(mods) != 4 {
		t.Errorf("renodx = %d mods, want the memo's four", len(mods))
	}
}

// catalogTestServer serves the real catalog fixtures for resolve-fallback
// tests.
func catalogTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		if name == "tags" {
			_, _ = w.Write([]byte(`[{"name":"v6.8.0"}]`))
			return
		}
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "catalog", name))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	}))
}

func catalogTestClient(t *testing.T, srv *httptest.Server) *catalog.Client {
	t.Helper()
	c := catalog.New(srv.Client(), t.TempDir(), time.Hour, "yarm/test")
	c.PackagesURL = srv.URL + "/EffectPackages.ini"
	c.AddonsURL = srv.URL + "/Addons.ini"
	c.TagsURL = srv.URL + "/tags"
	c.RenoDXURL = srv.URL + "/renodx-metadata.json"
	return c
}

// A memo that cannot load must not take the install down with it: resolve
// falls back to the catalog client.
func TestResolveFallsBackToCatalog(t *testing.T) {
	srv := catalogTestServer(t)
	t.Cleanup(srv.Close)
	r := &RealInstaller{
		WizardData: fakeWizardData{err: errors.New("memo empty")},
		Catalog:    catalogTestClient(t, srv),
	}

	packages, err := r.packages(context.Background())
	if err != nil {
		t.Fatalf("packages error = %v", err)
	}
	if len(packages) != 3 {
		t.Errorf("packages = %d, want the 3 from the fixture catalog", len(packages))
	}
}

// RenoDX tolerates a failed load at wizard time, so an empty memo list is
// not an answer — resolve asks the client, which may have recovered since.
func TestResolveRenoDXEmptyMemoFallsBack(t *testing.T) {
	srv := catalogTestServer(t)
	t.Cleanup(srv.Close)
	data := sampleWizardData()
	data.RenoDX = nil
	r := &RealInstaller{
		WizardData: fakeWizardData{data: data},
		Catalog:    catalogTestClient(t, srv),
	}

	// Packages still come from the memo untouched.
	if packages, err := r.packages(context.Background()); err != nil || len(packages) != 2 {
		t.Fatalf("packages = %d, %v; want the memo's two", len(packages), err)
	}

	mods, err := r.renoDX(context.Background())
	if err != nil {
		t.Fatalf("renodx error = %v", err)
	}
	if len(mods) == 0 {
		t.Error("renodx should fall back to the catalog when the memo has none")
	}
}

// editFlowFixture installs ReShade for real through the installer (one
// HTTP round of downloads), then empties the cache, leaving an installed
// game with nothing cached — the user's exact scenario.
func editFlowFixture(t *testing.T) (installer *RealInstaller, req install.Request, hits *atomic.Int64) {
	t.Helper()
	var hitsN atomic.Int64

	// A ReShade-setup-shaped payload: PE prefix with the two DLLs zipped
	// on (the shape ExtractReShade reads), served per requested version.
	setup := func(version string) []byte {
		var buf bytes.Buffer
		buf.WriteString("MZ\x00\x00")
		zw := archivezip.NewWriter(&buf)
		for _, n := range []string{"ReShade32.dll", "ReShade64.dll"} {
			w, _ := zw.Create(n)
			_, _ = w.Write([]byte(n + " body for " + version))
		}
		_ = zw.Close()
		return buf.Bytes()
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsN.Add(1)
		name := filepath.Base(r.URL.Path)
		if name == "tags" {
			_, _ = w.Write([]byte(`[{"name":"v6.8.0"}]`))
			return
		}
		if strings.HasPrefix(name, "ReShade_Setup_") {
			version := strings.TrimSuffix(strings.TrimPrefix(name, "ReShade_Setup_"), "_Addon.exe")
			version = strings.TrimSuffix(version, ".exe")
			_, _ = w.Write(setup(version))
			return
		}
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "catalog", name))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	c := cache.New(t.TempDir(), fetch.New("yarm/test"))
	c.SetupURL = func(version string, addon bool) string {
		flavor := ""
		if addon {
			flavor = "_Addon"
		}
		return srv.URL + "/ReShade_Setup_" + version + flavor + ".exe"
	}

	gameRoot := t.TempDir()
	exeDir := filepath.Join(gameRoot, "Game")
	if err := os.MkdirAll(exeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(exeDir, "ember.exe"), []byte("game"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &RealInstaller{
		Cache:     c,
		Catalog:   catalogTestClient(t, srv),
		CustomDir: t.TempDir(),
		StateDir:  t.TempDir(),
		Games:     NewGamesCache(),
	}

	req = install.Request{
		Game:     game.Game{ID: "steam:700110", Name: "Ember Hollow", Root: gameRoot},
		Exe:      game.Executable{Path: "Game/ember.exe", Arch: game.ArchX64, API: game.APID3D12},
		Version:  "6.8.0",
		Flavor:   install.FlavorNormal,
		DLLName:  "dxgi.dll",
		TargetOS: artifacts.OSWindows,
	}
	if _, err := inst.Install(context.Background(), req, func(ProgressUpdate) {}); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// Clear the cache wholesale.
	if _, err := c.Clean(); err != nil {
		t.Fatalf("Clean(): %v", err)
	}
	return inst, req, &hitsN
}

// The user's scenario: cache emptied, then an edit over an installed
// game. Nothing the edit leaves unchanged may trigger a download.
func TestEditOverEmptyCacheDownloadsNothing(t *testing.T) {
	inst, req, hits := editFlowFixture(t)
	before := hits.Load() // the first install's own round of downloads

	if _, err := inst.Install(context.Background(), req, func(ProgressUpdate) {}); err != nil {
		t.Fatalf("edit install: %v", err)
	}
	if n := hits.Load() - before; n != 0 {
		t.Errorf("an unchanged edit made %d HTTP request(s), want 0", n)
	}
}
