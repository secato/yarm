package gog

// defaultRegistry returns nil: there is no registry to read on Linux, so
// GOG discovery runs on the launcher sources alone.
func defaultRegistry() registrySource { return nil }
