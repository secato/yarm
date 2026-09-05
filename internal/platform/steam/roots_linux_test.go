//go:build linux

package steam

import (
	"os"
	"strings"
	"testing"
)

func TestDefaultRoots(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory available: %v", err)
	}

	roots := defaultRoots()
	if len(roots) == 0 {
		t.Fatal("defaultRoots() returned no candidates")
	}
	for _, r := range roots {
		if !strings.HasPrefix(r, home) {
			t.Errorf("defaultRoots() candidate %q is not under the home directory %q", r, home)
		}
	}
}

func TestNewUsesDefaultRoots(t *testing.T) {
	p := New(nil)
	if len(p.roots) == 0 {
		t.Error("New() should populate roots from defaultRoots()")
	}
}
