package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/secato/yarm/internal/fsutil"
)

// Upstream catalog sources.
const (
	PackagesURL = "https://raw.githubusercontent.com/crosire/reshade-shaders/list/EffectPackages.ini"
	AddonsURL   = "https://raw.githubusercontent.com/crosire/reshade-shaders/list/Addons.ini"
	ReShadeURL  = "https://reshade.me/"
	TagsURL     = "https://api.github.com/repos/crosire/reshade/tags?per_page=100"
)

// Cached file names inside the catalog cache directory.
const (
	packagesFile = "EffectPackages.ini"
	addonsFile   = "Addons.ini"
	versionsFile = "reshade-versions.json"
	renodxFile   = "renodx-metadata.json"
	metaSuffix   = ".meta.json"
)

// DefaultRenoDXTTL is how long the RenoDX mod index is trusted when no
// configuration says otherwise: a rolling release, slower-moving than the
// ReShade version list but less static than the shader catalogs.
const DefaultRenoDXTTL = 7 * 24 * time.Hour

// maxCatalogBytes caps how much we read from a catalog endpoint. The real
// files are tens of kilobytes; this only stops a misbehaving or hijacked
// endpoint from filling the cache directory.
const maxCatalogBytes = 8 << 20

// StatusError is a non-200 response from a catalog source, exposed as a
// typed error (rather than only a formatted string) so a caller — the TUI,
// showing a friendlier message for a GitHub rate limit — can inspect the
// status code without parsing error text.
type StatusError struct {
	Code   int
	Status string
}

func (e StatusError) Error() string { return "unexpected status " + e.Status }

// RateLimited reports whether this response is the shape GitHub's API
// uses for both an authenticated-quota rate limit (403) and the
// unauthenticated one (429): both mean "wait and the cached copy is fine
// meanwhile", not "something is broken".
func (e StatusError) RateLimited() bool {
	return e.Code == http.StatusForbidden || e.Code == http.StatusTooManyRequests
}

// Doer is the HTTP surface Client needs. *http.Client satisfies it, and
// step 3's fetch client will too.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client loads catalogs, caching each one on disk. The ReShade, package
// and add-on catalogs are static snapshots: once fetched they are trusted
// indefinitely and only refreshed on explicit request (see Refresh), so
// starting the app never waits on the network for data it already has.
// The RenoDX index is a rolling release and keeps a TTL instead. Every
// read falls back to the cached copy — even an expired one — when the
// network is unavailable, so the app keeps working offline.
type Client struct {
	HTTP Doer
	Dir  string        // cache/catalog
	TTL  time.Duration // fallback trust window
	// RenoDXTTL is how long the RenoDX index is trusted. Zero means TTL.
	RenoDXTTL time.Duration
	UserAgent string

	// Source URLs, overridable so tests can point at an httptest server.
	PackagesURL string
	AddonsURL   string
	ReShadeURL  string
	TagsURL     string
	RenoDXURL   string

	// Now is overridable so tests can age the cache without sleeping.
	Now func() time.Time
}

// New returns a Client writing into dir, reading the real upstream
// catalogs.
func New(httpClient Doer, dir string, ttl time.Duration, userAgent string) *Client {
	return &Client{
		HTTP:        httpClient,
		Dir:         dir,
		TTL:         ttl,
		UserAgent:   userAgent,
		PackagesURL: PackagesURL,
		AddonsURL:   AddonsURL,
		ReShadeURL:  ReShadeURL,
		TagsURL:     TagsURL,
		RenoDXURL:   RenoDXMetadataURL,
		Now:         time.Now,
	}
}

// meta records when a cached catalog file was fetched.
type meta struct {
	FetchedAt time.Time `json:"fetched_at"`
}

// Packages returns the effect package catalog.
func (c *Client) Packages(ctx context.Context) ([]Package, error) {
	data, err := c.loadStatic(ctx, c.PackagesURL, packagesFile)
	if err != nil {
		return nil, err
	}
	return ParsePackages(bytes.NewReader(data))
}

// Addons returns the add-on catalog.
func (c *Client) Addons(ctx context.Context) ([]Addon, error) {
	data, err := c.loadStatic(ctx, c.AddonsURL, addonsFile)
	if err != nil {
		return nil, err
	}
	return ParseAddons(bytes.NewReader(data))
}

// RenoDX returns the RenoDX mod catalog.
//
// Worth knowing: this file is about 200 KB against maxCatalogBytes, an
// order of magnitude of headroom. If upstream ever crosses that cap the
// mods disappear from the wizard rather than erroring loudly, because
// WizardData deliberately does not count them when deciding whether a
// catalog load was empty.
func (c *Client) RenoDX(ctx context.Context) ([]RenoMod, error) {
	data, err := c.loadTTL(ctx, c.RenoDXURL, renodxFile, c.renoDXTTL())
	if err != nil {
		return nil, err
	}
	return ParseRenoDX(bytes.NewReader(data))
}

// renoDXTTL resolves the effective RenoDX trust window.
func (c *Client) renoDXTTL() time.Duration {
	if c.RenoDXTTL > 0 {
		return c.RenoDXTTL
	}
	return c.TTL
}

// Versions returns the installable ReShade versions, newest first, with
// the one advertised on reshade.me marked Latest.
//
// The tag list comes from disk once fetched; the "latest" marker is a
// live scrape done alongside it. A failure to reach reshade.me is not
// fatal: the list is still useful unmarked, so it is logged and the
// versions are returned as-is.
func (c *Client) Versions(ctx context.Context) ([]Version, error) {
	var data []byte
	var tagsErr error
	var latest string
	var latestErr error

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		data, tagsErr = c.loadStatic(ctx, c.TagsURL, versionsFile)
	}()
	go func() {
		defer wg.Done()
		latest, latestErr = c.latestVersion(ctx)
	}()
	wg.Wait()

	if tagsErr != nil {
		return nil, tagsErr
	}

	tags, err := parseTags(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	if latestErr != nil {
		slog.Warn("could not determine latest ReShade version", "error", latestErr)
	}

	out := make([]Version, len(tags))
	for i, v := range tags {
		out[i] = Version{Version: v, Latest: v == latest}
	}
	return out, nil
}

// latestVersion scrapes reshade.me's front page. It is not cached on disk:
// it is one small request, and a stale "latest" marker is worse than none.
func (c *Client) latestVersion(ctx context.Context) (string, error) {
	body, err := c.get(ctx, c.ReShadeURL)
	if err != nil {
		return "", err
	}
	return parseLatestVersion(body)
}

// RefreshCatalog re-fetches every catalog source regardless of cache
// state — the explicit "I want fresh data" behind the rescan key. Sources
// are fetched concurrently; a source that fails keeps its previous disk
// copy and contributes its error to the joined return, so one downed host
// does not wipe out the others.
func (c *Client) RefreshCatalog(ctx context.Context) error {
	type source struct {
		url, name string
	}
	sources := []source{
		{c.PackagesURL, packagesFile},
		{c.AddonsURL, addonsFile},
		{c.TagsURL, versionsFile},
		{c.RenoDXURL, renodxFile},
	}

	errs := make([]error, len(sources))
	var wg sync.WaitGroup
	for i, s := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := c.get(ctx, s.url)
			if err != nil {
				errs[i] = fmt.Errorf("refresh %s: %w", s.url, err)
				return
			}
			if err := c.store(filepath.Join(c.Dir, s.name), body); err != nil {
				errs[i] = fmt.Errorf("refresh %s: %w", s.url, err)
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// loadStatic returns the contents of a static catalog: the disk copy when
// one exists, whatever its age, fetching only when there is nothing cached
// yet. Freshness comes from RefreshCatalog, never from time passing. A
// failed fetch falls back to any cached copy.
func (c *Client) loadStatic(ctx context.Context, url, name string) ([]byte, error) {
	path := filepath.Join(c.Dir, name)
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	}
	return c.fetchAndStore(ctx, url, path)
}

// load returns the contents of a catalog, from disk when the cached copy
// is younger than the TTL and from the network otherwise. A failed fetch
// falls back to any cached copy regardless of age.
func (c *Client) load(ctx context.Context, url, name string) ([]byte, error) {
	return c.loadTTL(ctx, url, name, c.TTL)
}

// loadTTL is load with an explicit trust window, for sources whose
// freshness rules differ from the client's default.
func (c *Client) loadTTL(ctx context.Context, url, name string, ttl time.Duration) ([]byte, error) {
	path := filepath.Join(c.Dir, name)

	if cached, ok := c.readFreshTTL(path, ttl); ok {
		return cached, nil
	}
	return c.fetchAndStore(ctx, url, path)
}

// fetchAndStore downloads url, falls back to any cached copy on failure,
// and stores a good download for next time.
func (c *Client) fetchAndStore(ctx context.Context, url, path string) ([]byte, error) {
	body, err := c.get(ctx, url)
	if err != nil {
		if stale, readErr := os.ReadFile(path); readErr == nil {
			slog.Warn("catalog fetch failed, using cached copy",
				"url", url, "error", err)
			return stale, nil
		}
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}

	if err := c.store(path, body); err != nil {
		// A cache write failure must not block the app: we have the data.
		slog.Warn("could not cache catalog", "path", path, "error", err)
	}

	return body, nil
}

// readFreshTTL returns the cached file when its recorded fetch time is
// within the trust window.
func (c *Client) readFreshTTL(path string, ttl time.Duration) ([]byte, bool) {
	raw, err := os.ReadFile(path + metaSuffix)
	if err != nil {
		return nil, false
	}

	var m meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	if c.now().Sub(m.FetchedAt) >= c.TTL {
		return nil, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// store writes the catalog and its fetch timestamp.
func (c *Client) store(path string, body []byte) error {
	if err := fsutil.AtomicWrite(path, body, 0o644); err != nil {
		return err
	}
	raw, err := json.Marshal(meta{FetchedAt: c.now()})
	if err != nil {
		return err
	}
	return fsutil.AtomicWrite(path+metaSuffix, raw, 0o644)
}

// get performs one GET and returns the body.
func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, StatusError{Code: resp.StatusCode, Status: resp.Status}
	}

	// Read one byte past the cap: silently truncating a catalog would
	// parse as a valid but incomplete list, quietly hiding packages.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxCatalogBytes {
		return nil, fmt.Errorf("catalog is larger than the %d byte limit", maxCatalogBytes)
	}
	if len(body) == 0 {
		return nil, errors.New("empty response body")
	}
	return body, nil
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
