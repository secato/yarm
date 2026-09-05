package app

import (
	"errors"
	"io/fs"
	"net"
	"os"

	"github.com/secato/yarm/internal/catalog"
)

// friendlyError rewrites a handful of common, expected failures into a
// plain-language explanation, so the error overlay does not show
// something like `Get "https://api.github.com/...": dial tcp: lookup
// api.github.com: no such host` for what is just "you're offline".
// Anything not recognized is returned as the original error's own text —
// this only adds phrasing, it never hides information.
func friendlyError(err error) string {
	if err == nil {
		return ""
	}

	var statusErr catalog.StatusError
	if errors.As(err, &statusErr) && statusErr.RateLimited() {
		return "GitHub's API rate limit was reached. " +
			"Cached data will be used where available; try again in a while."
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "Could not reach " + dnsErr.Name + " — you appear to be offline. " +
			"Cached data will be used where available."
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return "A network request failed (" + opErr.Op + "). " +
			"Cached data will be used where available."
	}

	if errors.Is(err, os.ErrPermission) {
		return "Permission denied: " + permissionPath(err) +
			". Check that yarm has access to this location."
	}

	return err.Error()
}

// permissionPath extracts the path from a permission error, when the
// error carries one (most os and io/fs errors do via *fs.PathError), so
// the message can name what needs a permission fix.
func permissionPath(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Path
	}
	return "the requested path"
}
