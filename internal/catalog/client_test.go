package catalog

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// testServer serves the catalog fixtures and counts requests per path.
type testServer struct {
	*httptest.Server
	hits atomic.Int64
	fail atomic.Bool
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	ts := &testServer{}

	mux := http.NewServeMux()
	serve := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ts.hits.Add(1)
			if ts.fail.Load() {
				http.Error(w, "down", http.StatusInternalServerError)
				return
			}
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "catalog", name))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(data)
		}
	}
	mux.HandleFunc("/EffectPackages.ini", serve("EffectPackages.ini"))
	mux.HandleFunc("/Addons.ini", serve("Addons.ini"))
	mux.HandleFunc("/renodx.json", serve("renodx-metadata.json"))
	mux.HandleFunc("/tags", func(w http.ResponseWriter, r *http.Request) {
		ts.hits.Add(1)
		if ts.fail.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`[{"name":"v6.8.0"},{"name":"v6.7.3"},{"name":"v4.9.1"}]`))
	})
	mux.HandleFunc("/reshade", func(w http.ResponseWriter, r *http.Request) {
		if ts.fail.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`<a href="/downloads/ReShade_Setup_6.8.0.exe">x</a>`))
	})

	ts.Server = httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// newTestClient points a Client at ts, with a controllable clock.
func newTestClient(t *testing.T, ts *testServer, now *time.Time) *Client {
	t.Helper()
	c := New(ts.Client(), t.TempDir(), time.Hour, "yarm/test")
	c.PackagesURL = ts.URL + "/EffectPackages.ini"
	c.AddonsURL = ts.URL + "/Addons.ini"
	c.RenoDXURL = ts.URL + "/renodx.json"
	c.TagsURL = ts.URL + "/tags"
	c.ReShadeURL = ts.URL + "/reshade"
	c.Now = func() time.Time { return *now }
	return c
}

func TestClientCachesWithinTTL(t *testing.T) {
	ts := newTestServer(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	c := newTestClient(t, ts, &now)

	first, err := c.load(context.Background(), ts.URL+"/EffectPackages.ini", packagesFile)
	if err != nil {
		t.Fatalf("first load error = %v", err)
	}
	if got := ts.hits.Load(); got != 1 {
		t.Fatalf("after first load: %d requests, want 1", got)
	}

	// Within the TTL: served from disk, no second request.
	now = now.Add(30 * time.Minute)
	second, err := c.load(context.Background(), ts.URL+"/EffectPackages.ini", packagesFile)
	if err != nil {
		t.Fatalf("second load error = %v", err)
	}
	if got := ts.hits.Load(); got != 1 {
		t.Errorf("within TTL: %d requests, want 1 (cache should have served it)", got)
	}
	if string(first) != string(second) {
		t.Error("cached copy differs from the fetched one")
	}

	// Past the TTL: refetched.
	now = now.Add(time.Hour)
	if _, err := c.load(context.Background(), ts.URL+"/EffectPackages.ini", packagesFile); err != nil {
		t.Fatalf("third load error = %v", err)
	}
	if got := ts.hits.Load(); got != 2 {
		t.Errorf("past TTL: %d requests, want 2", got)
	}
}

// A dead network must not break the app when a cached copy exists, even an
// expired one.
func TestClientFallsBackToStaleCacheOffline(t *testing.T) {
	ts := newTestServer(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	c := newTestClient(t, ts, &now)

	packages, err := c.Packages(context.Background())
	if err != nil {
		t.Fatalf("warm the cache: %v", err)
	}
	if len(packages) != 3 {
		t.Fatalf("got %d packages, want 3", len(packages))
	}

	ts.fail.Store(true)
	now = now.Add(48 * time.Hour) // long past the TTL

	stale, err := c.load(context.Background(), ts.URL+"/EffectPackages.ini", packagesFile)
	if err != nil {
		t.Fatalf("offline load should fall back to cache, got error = %v", err)
	}
	if len(stale) == 0 {
		t.Error("offline fallback returned no data")
	}
}

// With no cache at all, a failure has to surface.
func TestClientErrorsWhenOfflineWithNoCache(t *testing.T) {
	ts := newTestServer(t)
	ts.fail.Store(true)
	now := time.Now()
	c := newTestClient(t, ts, &now)

	if _, err := c.load(context.Background(), ts.URL+"/EffectPackages.ini", packagesFile); err == nil {
		t.Error("want an error with no cached copy and a failing server, got nil")
	}
}

func TestClientVersions(t *testing.T) {
	ts := newTestServer(t)
	now := time.Now()
	c := newTestClient(t, ts, &now)

	versions, err := c.Versions(context.Background())
	if err != nil {
		t.Fatalf("Versions() error = %v", err)
	}

	// 4.9.1 is below the 5.0.0 floor; 6.8.0 is what reshade.me advertises.
	want := []Version{
		{Version: "6.8.0", Latest: true},
		{Version: "6.7.3", Latest: false},
	}
	if diff := cmp.Diff(want, versions); diff != "" {
		t.Errorf("Versions() mismatch (-want +got):\n%s", diff)
	}
}

// reshade.me being unreachable must not lose the version list; it only
// costs the "latest" marker.
func TestClientVersionsWithoutLatestMarker(t *testing.T) {
	ts := newTestServer(t)
	now := time.Now()
	c := newTestClient(t, ts, &now)
	c.ReShadeURL = ts.URL + "/does-not-exist"

	versions, err := c.Versions(context.Background())
	if err != nil {
		t.Fatalf("Versions() error = %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("got %d versions, want 2", len(versions))
	}
	for _, v := range versions {
		if v.Latest {
			t.Errorf("%s marked Latest despite reshade.me being unreachable", v.Version)
		}
	}
}

// Addons must round-trip through the cache too.
func TestClientAddons(t *testing.T) {
	ts := newTestServer(t)
	now := time.Now()
	c := newTestClient(t, ts, &now)

	addons, err := c.Addons(context.Background())
	if err != nil {
		t.Fatalf("Addons() error = %v", err)
	}
	if len(addons) != 6 {
		t.Errorf("got %d addons, want 6", len(addons))
	}
}

// The User-Agent is required by reshade.me and courteous to GitHub.
func TestClientSendsUserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("[00]\nPackageName=x\nDownloadUrl=https://e.invalid/a.zip\n"))
	}))
	defer srv.Close()

	c := New(srv.Client(), t.TempDir(), time.Hour, "yarm/1.2.3 (+https://example.invalid)")
	if _, err := c.load(context.Background(), srv.URL, packagesFile); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != "yarm/1.2.3 (+https://example.invalid)" {
		t.Errorf("User-Agent = %q", got)
	}
}

// A cache directory that cannot be written must not fail the load: the
// data is already in hand.
func TestClientSurvivesUnwritableCache(t *testing.T) {
	ts := newTestServer(t)
	now := time.Now()
	c := newTestClient(t, ts, &now)
	c.Dir = filepath.Join(c.Dir, "nonexistent", "\x00invalid")

	if _, err := c.load(context.Background(), ts.URL+"/EffectPackages.ini", packagesFile); err != nil {
		t.Errorf("load should succeed despite an unwritable cache, got %v", err)
	}
}

// Silently truncating an oversized catalog would parse as a valid but
// incomplete list, quietly hiding packages from the user.
func TestClientRejectsOversizedCatalog(t *testing.T) {
	huge := bytes.Repeat([]byte("[00]\nPackageName=x\nDownloadUrl=https://e.invalid/a.zip\n"), 200000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(huge)
	}))
	defer srv.Close()

	if len(huge) <= maxCatalogBytes {
		t.Fatalf("fixture is only %d bytes; it must exceed the %d limit", len(huge), maxCatalogBytes)
	}

	c := New(srv.Client(), t.TempDir(), time.Hour, "yarm/test")
	if _, err := c.load(context.Background(), srv.URL, packagesFile); err == nil {
		t.Error("want an error for an oversized catalog, got nil")
	}
}

// A non-200 response must surface as a StatusError a caller can inspect
// (for a friendlier "rate limited, using cache" message), not just a
// formatted string.
func TestClientStatusError(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		wantRateLimit bool
	}{
		{"github secondary rate limit", http.StatusForbidden, true},
		{"github primary rate limit", http.StatusTooManyRequests, true},
		{"not found", http.StatusNotFound, false},
		{"server error", http.StatusInternalServerError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			c := New(srv.Client(), t.TempDir(), time.Hour, "yarm/test")
			_, err := c.load(context.Background(), srv.URL, packagesFile)

			var se StatusError
			if !errors.As(err, &se) {
				t.Fatalf("error = %v (%T), want a StatusError", err, err)
			}
			if se.Code != tt.status {
				t.Errorf("Code = %d, want %d", se.Code, tt.status)
			}
			if got := se.RateLimited(); got != tt.wantRateLimit {
				t.Errorf("RateLimited() = %v, want %v", got, tt.wantRateLimit)
			}
		})
	}
}

// The RenoDX catalog rides the same TTL and the same offline fallback as
// the others: fresh once, then cached, then the stale copy when upstream
// cannot be reached.
func TestClientRenoDX(t *testing.T) {
	ts := newTestServer(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	c := newTestClient(t, ts, &now)

	mods, err := c.RenoDX(context.Background())
	if err != nil {
		t.Fatalf("RenoDX() error = %v", err)
	}
	if len(mods) == 0 {
		t.Fatal("RenoDX() returned nothing")
	}
	before := ts.hits.Load()

	// Within the TTL, from disk.
	if _, err := c.RenoDX(context.Background()); err != nil {
		t.Fatalf("second RenoDX(): %v", err)
	}
	if got := ts.hits.Load(); got != before {
		t.Errorf("refetched within the TTL: %d hits, want %d", got, before)
	}

	// Upstream down and the copy stale: the cached list is still better
	// than no list, because RenoDX being unreachable must not take the
	// step away.
	now = now.Add(48 * time.Hour)
	ts.fail.Store(true)
	stale, err := c.RenoDX(context.Background())
	if err != nil {
		t.Fatalf("RenoDX() with the server down = %v, want the cached copy", err)
	}
	if len(stale) != len(mods) {
		t.Errorf("stale copy has %d mods, want %d", len(stale), len(mods))
	}
}
