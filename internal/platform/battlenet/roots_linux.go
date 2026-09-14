//go:build linux

package battlenet

import (
	"os"
	"path/filepath"
)

// defaultProductDBs returns the product.db locations known without
// querying anything. Lutris-managed prefixes are found separately, at
// Discover time, via lutrisDBs — but Omarchy's Battle.net installer
// (omarchy-install-gaming-battlenet) sets none of that up: it runs
// umu-launcher with GE-Proton against a wine prefix at a fixed path,
// $HOME/Games/battlenet, with no Lutris, Steam or Heroic tracking it
// anywhere. WINEPREFIX is the prefix root itself there, so drive_c sits
// directly inside it, unlike Proton's own compat-data convention which
// nests the prefix one level deeper under a "pfx" directory.
func defaultProductDBs() []productDB {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	prefix := filepath.Join(home, "Games", "battlenet")
	return []productDB{{
		path:   filepath.Join(prefix, "drive_c", "ProgramData", "Battle.net", "Agent", "product.db"),
		prefix: prefix,
	}}
}
