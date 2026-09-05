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
	"time"

	"github.com/secato/yarm/internal/fsutil"
)

// Upstream catalog sources (docs/plan/04-external-sources.md §4.1–4.3).
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
	metaSuffix   = ".meta.json"
)

// maxCatalogBytes caps how much we read from a catalog endpoint. The real
// files are tens of kilobytes; this only stops a misbehaving or hijacked
// endpoint from filling the cache directory.
const maxCatalogBytes = 8 << 20

// Doer is the HTTP surface Client needs. *http.Client satisfies it, and
// step 3's fetch client will too.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client loads catalogs, caching each one on disk and re-fetching only
// once its TTL has expired. Every read falls back to the cached copy —
// even an expired one — when the network is unavailable, so the app keeps
// working offline.
type Client struct {
	HTTP      Doer
	Dir       string        // cache/catalog
	TTL       time.Duration // from config.catalog_ttl_hours
	UserAgent string

	// Source URLs, overridable so tests can point at an httptest server.
	PackagesURL string
	AddonsURL   string
	ReShadeURL  string
	TagsURL     string

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
		Now:         time.Now,
	}
}

// meta records when a cached catalog file was fetched.
type meta struct {
	FetchedAt time.Time `json:"fetched_at"`
}

// Packages returns the effect package catalog.
func (c *Client) Packages(ctx context.Context) ([]Package, error) {
	data, err := c.load(ctx, c.PackagesURL, packagesFile)
	if err != nil {
		return nil, err
	}
	return ParsePackages(bytes.NewReader(data))
}

// Addons returns the add-on catalog.
func (c *Client) Addons(ctx context.Context) ([]Addon, error) {
	data, err := c.load(ctx, c.AddonsURL, addonsFile)
	if err != nil {
		return nil, err
	}
	return ParseAddons(bytes.NewReader(data))
}

// Versions returns the installable ReShade versions, newest first, with
// the one advertised on reshade.me marked Latest.
//
// The version list and the "latest" marker come from different hosts. A
// failure to reach reshade.me is not fatal: the list is still useful
// unmarked, so it is logged and the versions are returned as-is.
func (c *Client) Versions(ctx context.Context) ([]Version, error) {
	data, err := c.load(ctx, c.TagsURL, versionsFile)
	if err != nil {
		return nil, err
	}

	tags, err := parseTags(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	latest, err := c.latestVersion(ctx)
	if err != nil {
		slog.Warn("could not determine latest ReShade version", "error", err)
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

// load returns the contents of a catalog, from disk when the cached copy
// is younger than the TTL and from the network otherwise. A failed fetch
// falls back to any cached copy regardless of age.
func (c *Client) load(ctx context.Context, url, name string) ([]byte, error) {
	path := filepath.Join(c.Dir, name)

	if cached, ok := c.readFresh(path); ok {
		return cached, nil
	}

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

// readFresh returns the cached file when its recorded fetch time is within
// the TTL.
func (c *Client) readFresh(path string) ([]byte, bool) {
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
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes))
	if err != nil {
		return nil, err
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
