//go:build !windows && !linux

package battlenet

// defaultProductDBs returns no candidates: yarm does not ship builds
// for this platform, and neither Battle.net nor any known wine-prefix
// convention for it exists here. The stub exists so the package keeps
// compiling, like the steam provider's darwin roots.
func defaultProductDBs() []productDB { return nil }
