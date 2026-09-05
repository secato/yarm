package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/secato/yarm/internal/catalog"
)

func TestFriendlyErrorRateLimit(t *testing.T) {
	err := catalog.StatusError{Code: http.StatusForbidden, Status: "403 Forbidden"}
	got := friendlyError(err)
	if !strings.Contains(got, "rate limit") {
		t.Errorf("friendlyError() = %q, want it to mention the rate limit", got)
	}
	if !strings.Contains(strings.ToLower(got), "cache") {
		t.Errorf("friendlyError() = %q, want it to mention cached data", got)
	}
}

func TestFriendlyErrorDNS(t *testing.T) {
	err := &net.DNSError{Err: "no such host", Name: "api.github.com", IsNotFound: true}
	got := friendlyError(err)
	if !strings.Contains(got, "offline") {
		t.Errorf("friendlyError() = %q, want it to say offline", got)
	}
	if !strings.Contains(got, "api.github.com") {
		t.Errorf("friendlyError() = %q, want it to name the host", got)
	}
}

func TestFriendlyErrorNetworkOp(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	got := friendlyError(err)
	if !strings.Contains(got, "network request failed") {
		t.Errorf("friendlyError() = %q, want it to mention a network failure", got)
	}
}

func TestFriendlyErrorPermission(t *testing.T) {
	err := &os.PathError{Op: "open", Path: "/games/SomeGame", Err: os.ErrPermission}
	got := friendlyError(err)
	if !strings.Contains(got, "Permission denied") {
		t.Errorf("friendlyError() = %q, want it to say permission denied", got)
	}
	if !strings.Contains(got, "/games/SomeGame") {
		t.Errorf("friendlyError() = %q, want it to name the path", got)
	}
}

// Anything not recognized must pass through unchanged — this only adds
// phrasing for known cases, never hides real information.
func TestFriendlyErrorUnrecognized(t *testing.T) {
	err := errors.New("something specific and unexpected")
	if got := friendlyError(err); got != err.Error() {
		t.Errorf("friendlyError() = %q, want the original message unchanged", got)
	}
}

func TestFriendlyErrorNil(t *testing.T) {
	if got := friendlyError(nil); got != "" {
		t.Errorf("friendlyError(nil) = %q, want empty", got)
	}
}

// A real permission-denied error, produced by the actual filesystem, must
// be classified the same way as the constructed one above.
func TestFriendlyErrorRealPermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root ignores directory permissions")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	_, err := os.ReadDir(locked)
	if err == nil {
		t.Fatal("test setup: reading a 0o000 directory should fail")
	}

	got := friendlyError(err)
	if !strings.Contains(got, "Permission denied") {
		t.Errorf("friendlyError() on a real permission error = %q", got)
	}
	if !strings.Contains(got, locked) {
		t.Errorf("friendlyError() = %q, want it to name %q", got, locked)
	}
}

// A real DNS failure, from an actual (sandboxed, no-network) HTTP client
// call, must classify the same way as the constructed one above.
func TestFriendlyErrorRealDNSFailure(t *testing.T) {
	cl := catalog.New(&http.Client{Timeout: 2 * time.Second}, t.TempDir(), time.Hour, "yarm/test")
	cl.PackagesURL = "https://this-host-genuinely-does-not-exist.invalid/EffectPackages.ini"

	_, err := cl.Packages(context.Background())
	if err == nil {
		t.Skip("test setup: expected a DNS failure but the request succeeded")
	}

	got := friendlyError(err)
	if !strings.Contains(got, "offline") {
		t.Errorf("friendlyError() on a real DNS failure = %q, want it to say offline", got)
	}
}
