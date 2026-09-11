// Package fetch downloads files over HTTP with retries, progress
// reporting and optional checksum verification. Downloads land in a
// ".part" file and are renamed into place only once complete and
// verified, so an interrupted run never leaves a corrupt artifact behind
// that a later run would mistake for a good one.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Defaults for a Client created by New.
const (
	DefaultTimeout = 5 * time.Minute
	DefaultRetries = 3
	DefaultBackoff = time.Second
	// DefaultMaxBytes bounds a single download. The largest artifact YARM
	// fetches is the ~38 MB Firefox installer, so this is generous; it
	// exists so a hostile or malfunctioning host cannot stream until the
	// disk is full.
	DefaultMaxBytes = 2 << 30
)

// PartSuffix is appended to the destination while a download is in
// flight.
const PartSuffix = ".part"

// Progress reports how far a download has got. Total is -1 when the
// server does not send a Content-Length.
type Progress struct {
	Downloaded int64
	Total      int64
}

// Percent returns completion in [0,100], or -1 when the total is unknown.
func (p Progress) Percent() float64 {
	if p.Total <= 0 {
		return -1
	}
	return float64(p.Downloaded) / float64(p.Total) * 100
}

// ProgressFunc is called periodically during a download. It must not
// block: it is invoked on the goroutine doing the copy.
type ProgressFunc func(Progress)

// Client downloads files. The zero value is not usable; call New.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Retries is the number of attempts after the first, so 3 means up to
	// four requests in total.
	Retries int
	// Backoff is the delay before the first retry; it doubles each time.
	Backoff time.Duration
	// MaxBytes caps a single download. Zero means DefaultMaxBytes.
	MaxBytes int64
}

// New returns a Client with sensible defaults.
func New(userAgent string) *Client {
	return &Client{
		HTTP: &http.Client{
			Timeout:       DefaultTimeout,
			Transport:     DefaultTransport(),
			CheckRedirect: CheckRedirect,
		},
		UserAgent: userAgent,
		Retries:   DefaultRetries,
		Backoff:   DefaultBackoff,
		MaxBytes:  DefaultMaxBytes,
	}
}

// ErrInsecureURL reports a URL yarm refuses to fetch because the bytes
// would not be authenticated in transit.
var ErrInsecureURL = errors.New("refusing a URL that is not https")

// maxRedirects mirrors net/http's own default. Setting CheckRedirect
// replaces that default, so the cap has to be restated here.
const maxRedirects = 10

// EnsureSecureURL rejects anything yarm should not fetch: every artifact
// it downloads ends up as a DLL loaded into a game process, so the bytes
// have to be authenticated in transit. Package and add-on URLs come
// verbatim out of a downloaded ini, which makes this the boundary where a
// hostile catalog would otherwise get to choose "http".
//
// Plain http to loopback is allowed: it cannot be tampered with by anyone
// who is not already inside the machine, and it is what lets the tests
// drive real httptest servers instead of a mock.
func EnsureSecureURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse %q: %w", raw, err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopback(u.Hostname()) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrInsecureURL, raw)
}

// isLoopback reports whether a host names this machine.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CheckRedirect refuses a redirect that drops out of https.
//
// Cross-host redirects are deliberately allowed: GitHub serves every
// package archive by redirecting github.com to codeload.github.com, and
// release assets to objects.githubusercontent.com, so pinning the host
// across hops would break the catalog as it stands today. The scheme is
// what matters — a downgrade to http is what would let someone rewrite
// the bytes on the way.
func CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	return EnsureSecureURL(req.URL.String())
}

// DefaultTransport returns a transport with connection reuse tuned for a
// client that talks to a handful of hosts repeatedly: idle connections
// persist across the sequential catalog and artifact fetches instead of
// paying a handshake per request.
func DefaultTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	}
}

// ErrTooLarge is returned when a response exceeds the client's MaxBytes.
var ErrTooLarge = errors.New("download exceeds the size limit")

// maxBytes resolves the effective cap.
func (c *Client) maxBytes() int64 {
	if c.MaxBytes > 0 {
		return c.MaxBytes
	}
	return DefaultMaxBytes
}

// ErrChecksumMismatch is returned when a download's SHA-256 does not match
// what the caller expected. The partial file is removed.
var ErrChecksumMismatch = errors.New("checksum mismatch")

// Download fetches url into dst.
//
// When expectedSHA is non-empty the download must hash to it, or the file
// is deleted and ErrChecksumMismatch returned — a pinned artifact that
// arrives wrong is never left on disk for a later run to trust.
//
// A URL that is not https is refused before anything is created on disk,
// and so is a redirect that leaves https part-way through.
//
// onProgress may be nil.
func (c *Client) Download(ctx context.Context, url, dst string, onProgress ProgressFunc, expectedSHA string) error {
	if err := EnsureSecureURL(url); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	part := dst + PartSuffix
	var lastErr error

	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			delay := c.Backoff << (attempt - 1)
			slog.Debug("retrying download", "url", url, "attempt", attempt, "delay", delay)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		sum, err := c.attempt(ctx, url, part, onProgress)
		if err == nil {
			if expectedSHA != "" && !equalHex(sum, expectedSHA) {
				_ = os.Remove(part)
				return fmt.Errorf("%w for %s: got %s, want %s",
					ErrChecksumMismatch, url, sum, expectedSHA)
			}
			return os.Rename(part, dst)
		}

		lastErr = err
		if ctx.Err() != nil || !retryable(err) {
			break
		}
	}

	_ = os.Remove(part)
	return fmt.Errorf("download %s: %w", url, lastErr)
}

// attempt performs one request, streaming the body to part and returning
// the hex SHA-256 of what was written.
func (c *Client) attempt(ctx context.Context, url, part string, onProgress ProgressFunc) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", statusError{Code: resp.StatusCode, Status: resp.Status}
	}

	f, err := os.Create(part)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	w := io.MultiWriter(f, h)
	if onProgress != nil {
		w = io.MultiWriter(f, h, &progressWriter{
			total: resp.ContentLength,
			fn:    onProgress,
		})
	}

	// Read one byte past the cap so that hitting it is distinguishable
	// from a body that simply ended.
	limit := c.maxBytes()
	written, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", err
	}
	if written > limit {
		return "", fmt.Errorf("%w: %d bytes", ErrTooLarge, limit)
	}
	if err := f.Sync(); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// progressWriter turns writes into Progress callbacks, throttled so a fast
// download does not flood the UI with messages.
type progressWriter struct {
	total      int64
	downloaded int64
	lastReport time.Time
	fn         ProgressFunc
}

// progressInterval is the minimum gap between progress callbacks.
const progressInterval = 100 * time.Millisecond

func (w *progressWriter) Write(p []byte) (int, error) {
	w.downloaded += int64(len(p))
	if now := time.Now(); now.Sub(w.lastReport) >= progressInterval {
		w.lastReport = now
		w.fn(Progress{Downloaded: w.downloaded, Total: w.total})
	}
	return len(p), nil
}

// statusError is a non-200 response.
type statusError struct {
	Code   int
	Status string
}

func (e statusError) Error() string { return "unexpected status " + e.Status }

// retryable reports whether an error is worth another attempt: transport
// failures, and the server-side or rate-limit statuses. A 404 or 403 will
// not improve by asking again.
func retryable(err error) bool {
	if errors.Is(err, ErrTooLarge) {
		return false
	}
	var se statusError
	if errors.As(err, &se) {
		return se.Code >= 500 || se.Code == http.StatusTooManyRequests || se.Code == http.StatusRequestTimeout
	}
	return true
}

// equalHex compares two hex digests case-insensitively.
func equalHex(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		if lowerHex(a[i]) != lowerHex(b[i]) {
			return false
		}
	}
	return true
}

func lowerHex(c byte) byte {
	if c >= 'A' && c <= 'F' {
		return c + ('a' - 'A')
	}
	return c
}
