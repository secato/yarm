//go:build windows

package battlenet

import (
	"os"
	"path/filepath"
)

// defaultProductDBs returns the Battle.net agent's database location on
// Windows. The agent holds the file open while it works; reading it is
// still fine, and a torn read simply fails to parse and is retried on
// the next discovery. No prefix: product.db's install paths are already
// real Windows paths here, since Battle.net runs natively.
func defaultProductDBs() []productDB {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return []productDB{{path: filepath.Join(base, "Battle.net", "Agent", "product.db")}}
}
