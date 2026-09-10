package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/secato/yarm/internal/catalog"
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
