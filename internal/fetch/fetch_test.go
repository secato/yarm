package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(srv *httptest.Server) *Client {
	return &Client{
		HTTP:      srv.Client(),
		UserAgent: "yarm/test",
		Retries:   3,
		Backoff:   time.Millisecond,
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestDownload(t *testing.T) {
	body := []byte("the artifact contents")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "yarm/test" {
			t.Errorf("User-Agent = %q", got)
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "nested", "artifact.bin")
	if err := testClient(srv).Download(context.Background(), srv.URL, dst, nil, sha256Hex(body)); err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("content = %q, want %q", got, body)
	}

	// The .part file must be gone once the download succeeds.
	if _, err := os.Stat(dst + PartSuffix); err == nil {
		t.Error(".part file left behind after a successful download")
	}
}

// A pinned artifact that arrives wrong must never be left on disk, or a
// later run would find it and trust it.
func TestDownloadChecksumMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("tampered content"))
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "artifact.bin")
	err := testClient(srv).Download(context.Background(), srv.URL, dst,
		nil, sha256Hex([]byte("expected content")))

	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v, want ErrChecksumMismatch", err)
	}
	for _, p := range []string{dst, dst + PartSuffix} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s should not exist after a checksum mismatch", filepath.Base(p))
		}
	}
}

func TestDownloadRetriesServerErrors(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 3 {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "artifact.bin")
	if err := testClient(srv).Download(context.Background(), srv.URL, dst, nil, ""); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got := hits.Load(); got != 3 {
		t.Errorf("made %d requests, want 3 (two failures then success)", got)
	}
}

// A 404 will not improve by asking again.
func TestDownloadDoesNotRetryClientErrors(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "artifact.bin")
	if err := testClient(srv).Download(context.Background(), srv.URL, dst, nil, ""); err == nil {
		t.Fatal("want an error for 404, got nil")
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("made %d requests for a 404, want 1", got)
	}
}

// Rate limiting is worth waiting out.
func TestDownloadRetriesRateLimit(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "artifact.bin")
	if err := testClient(srv).Download(context.Background(), srv.URL, dst, nil, ""); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("made %d requests, want 2", got)
	}
}

func TestDownloadGivesUpAfterRetries(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "artifact.bin")
	if err := testClient(srv).Download(context.Background(), srv.URL, dst, nil, ""); err == nil {
		t.Fatal("want an error, got nil")
	}
	if got := hits.Load(); got != 4 {
		t.Errorf("made %d requests, want 4 (initial + 3 retries)", got)
	}
	if _, err := os.Stat(dst + PartSuffix); err == nil {
		t.Error(".part file left behind after a failed download")
	}
}

func TestDownloadReportsProgress(t *testing.T) {
	body := make([]byte, 512*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "524288")
		for chunk := 0; chunk < 8; chunk++ {
			_, _ = w.Write(body[chunk*65536 : (chunk+1)*65536])
			w.(http.Flusher).Flush()
			time.Sleep(30 * time.Millisecond)
		}
	}))
	defer srv.Close()

	var last Progress
	var calls atomic.Int64
	dst := filepath.Join(t.TempDir(), "artifact.bin")

	err := testClient(srv).Download(context.Background(), srv.URL, dst, func(p Progress) {
		calls.Add(1)
		last = p
	}, "")
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	if calls.Load() == 0 {
		t.Error("progress callback was never invoked")
	}
	if last.Total != int64(len(body)) {
		t.Errorf("Total = %d, want %d", last.Total, len(body))
	}
}

func TestDownloadCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	dst := filepath.Join(t.TempDir(), "artifact.bin")
	if err := testClient(srv).Download(ctx, srv.URL, dst, nil, ""); err == nil {
		t.Error("want an error when the context is canceled, got nil")
	}
}

func TestProgressPercent(t *testing.T) {
	tests := []struct {
		p    Progress
		want float64
	}{
		{Progress{Downloaded: 50, Total: 100}, 50},
		{Progress{Downloaded: 100, Total: 100}, 100},
		{Progress{Downloaded: 10, Total: -1}, -1}, // unknown length
		{Progress{Downloaded: 10, Total: 0}, -1},
	}
	for _, tt := range tests {
		if got := tt.p.Percent(); got != tt.want {
			t.Errorf("Progress%+v.Percent() = %v, want %v", tt.p, got, tt.want)
		}
	}
}

func TestEqualHexIsCaseInsensitive(t *testing.T) {
	if !equalHex("ABCdef123", "abcDEF123") {
		t.Error("equalHex should ignore case")
	}
	if equalHex("abc", "abcd") {
		t.Error("equalHex should reject different lengths")
	}
}

// A host that streams without end must not be able to fill the disk.
func TestDownloadEnforcesSizeLimit(t *testing.T) {
	chunk := make([]byte, 32*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No Content-Length: the client cannot know the size in advance,
		// so only the cap protects it.
		for range 200 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	c := testClient(srv)
	c.MaxBytes = 128 * 1024

	dst := filepath.Join(t.TempDir(), "artifact.bin")
	err := c.Download(context.Background(), srv.URL, dst, nil, "")
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v, want ErrTooLarge", err)
	}
	for _, p := range []string{dst, dst + PartSuffix} {
		if _, statErr := os.Stat(p); statErr == nil {
			t.Errorf("%s should not remain after an oversized download", filepath.Base(p))
		}
	}
}

// Oversized is not a transient condition, so it must not burn retries.
func TestDownloadDoesNotRetryOversized(t *testing.T) {
	var hits atomic.Int64
	chunk := make([]byte, 32*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		for range 20 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	c := testClient(srv)
	c.MaxBytes = 64 * 1024

	dst := filepath.Join(t.TempDir(), "artifact.bin")
	if err := c.Download(context.Background(), srv.URL, dst, nil, ""); err == nil {
		t.Fatal("want an error, got nil")
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("made %d requests for an oversized body, want 1", got)
	}
}

// A download exactly at the cap is fine; the limit is inclusive.
func TestDownloadAllowsExactlyMaxBytes(t *testing.T) {
	body := make([]byte, 64*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := testClient(srv)
	c.MaxBytes = int64(len(body))

	dst := filepath.Join(t.TempDir(), "artifact.bin")
	if err := c.Download(context.Background(), srv.URL, dst, nil, ""); err != nil {
		t.Errorf("a body exactly at the cap should succeed, got %v", err)
	}
}

// Every artifact yarm downloads ends up as a DLL loaded into a game
// process, and package URLs come verbatim out of a downloaded ini — so
// the scheme is checked before anything is fetched.
func TestEnsureSecureURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		ok   bool
	}{
		{"https", "https://github.com/x/y/archive/main.zip", true},
		{"plain http", "http://github.com/x/y/archive/main.zip", false},
		{"http to loopback ip", "http://127.0.0.1:8080/pkg.zip", true},
		{"http to loopback name", "http://localhost:8080/pkg.zip", true},
		{"http to ipv6 loopback", "http://[::1]:8080/pkg.zip", true},
		{"http to a host that merely starts with localhost", "http://localhost.evil.test/pkg.zip", false},
		{"file", "file:///etc/passwd", false},
		{"ftp", "ftp://example.test/pkg.zip", false},
		{"no scheme", "example.test/pkg.zip", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := EnsureSecureURL(tt.url)
			if tt.ok && err != nil {
				t.Errorf("EnsureSecureURL(%q) = %v, want nil", tt.url, err)
			}
			if !tt.ok && err == nil {
				t.Errorf("EnsureSecureURL(%q) = nil, want an error", tt.url)
			}
		})
	}
}

func TestDownloadRefusesInsecureURL(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "pkg.zip")

	err := (&Client{HTTP: http.DefaultClient}).
		Download(context.Background(), "http://example.invalid/pkg.zip", dst, nil, "")
	if !errors.Is(err, ErrInsecureURL) {
		t.Fatalf("Download() error = %v, want ErrInsecureURL", err)
	}
	// Refused before anything touches the disk: not even the directory
	// or the .part file may appear.
	for _, p := range []string{dst, dst + PartSuffix, filepath.Dir(dst)} {
		if _, err := os.Stat(p); err == nil && p != filepath.Dir(dst) {
			t.Errorf("%s exists after a refused download", p)
		}
	}
}

// A redirect is the other half of the same rule: a host may hand the
// download off (GitHub sends every archive to codeload), but never down
// to plain http.
func TestCheckRedirect(t *testing.T) {
	req := func(url string) *http.Request {
		r, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		return r
	}

	if err := CheckRedirect(req("https://codeload.github.com/x/y/zip/main"), []*http.Request{
		req("https://github.com/x/y/archive/main.zip"),
	}); err != nil {
		t.Errorf("cross-host https redirect refused: %v", err)
	}

	if err := CheckRedirect(req("http://evil.test/payload.zip"), []*http.Request{
		req("https://github.com/x/y/archive/main.zip"),
	}); !errors.Is(err, ErrInsecureURL) {
		t.Errorf("https to http redirect = %v, want ErrInsecureURL", err)
	}

	var via []*http.Request
	for range maxRedirects {
		via = append(via, req("https://example.test/"))
	}
	if err := CheckRedirect(req("https://example.test/"), via); err == nil {
		t.Error("a redirect loop past the cap was allowed")
	}
}

// End to end: the first hop is a loopback test server, which is allowed,
// and the hop it points at is not.
func TestDownloadRefusesRedirectOffHTTPS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.invalid/payload.zip", http.StatusFound)
	}))
	defer srv.Close()

	c := testClient(srv)
	c.HTTP = &http.Client{Transport: srv.Client().Transport, CheckRedirect: CheckRedirect}
	c.Retries = 0

	dst := filepath.Join(t.TempDir(), "pkg.zip")
	if err := c.Download(context.Background(), srv.URL+"/pkg.zip", dst, nil, ""); err == nil {
		t.Fatal("Download() followed a redirect off https")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("a file was written despite the refused redirect")
	}
}
