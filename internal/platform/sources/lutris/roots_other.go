//go:build !linux

package lutris

// DefaultDBPaths returns no candidates: Lutris is a Linux launcher, so
// there is no pga.db to read anywhere else.
func DefaultDBPaths() []string { return nil }
