//go:build !windows

package epic

// defaultManifestDirs returns no candidates: the Epic launcher's
// manifest directory only exists where the launcher runs, and on every
// other OS the store's games come from the launcher sources.
func defaultManifestDirs() []string { return nil }
