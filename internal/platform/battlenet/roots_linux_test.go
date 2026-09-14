//go:build linux

package battlenet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultProductDBsFixedPrefix(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory available: %v", err)
	}

	dbs := defaultProductDBs()
	prefix := filepath.Join(home, "Games", "battlenet")
	wantPath := filepath.Join(prefix, "drive_c", "ProgramData", "Battle.net", "Agent", "product.db")
	if len(dbs) != 1 || dbs[0].path != wantPath || dbs[0].prefix != prefix {
		t.Errorf("defaultProductDBs() = %+v, want [{path: %q, prefix: %q}]", dbs, wantPath, prefix)
	}
}
