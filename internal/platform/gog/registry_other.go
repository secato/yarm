//go:build !windows

package gog

// defaultRegistry returns nil: there is no Windows registry to read on
// this OS, so GOG discovery runs on the launcher sources alone.
func defaultRegistry() registrySource { return nil }
