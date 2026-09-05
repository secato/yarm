// Package buildinfo exposes version metadata set at link time via -ldflags.
package buildinfo

var (
	// Version is the released version, or "dev" for local builds.
	Version = "dev"
	// Commit is the short git commit hash the binary was built from.
	Commit = "none"
	// Date is the build timestamp, set via -ldflags.
	Date = "unknown"
)

// String returns a one-line human-readable summary.
func String() string {
	return "yarm " + Version + " (" + Commit + ", " + Date + ")"
}
