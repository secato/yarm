package fsutil

import "testing"

// FreeSpace is exercised against the real filesystem: a temp dir always
// exists on some real mount, so this is naturally an integration test
// rather than one needing a fixture.
func TestFreeSpace(t *testing.T) {
	got, err := FreeSpace(t.TempDir())
	if err != nil {
		t.Fatalf("FreeSpace() error = %v", err)
	}
	if got <= 0 {
		t.Errorf("FreeSpace() = %d, want a positive byte count", got)
	}
}

func TestFreeSpaceMissingPath(t *testing.T) {
	if _, err := FreeSpace("/this/path/definitely/does/not/exist/anywhere"); err == nil {
		t.Error("FreeSpace() on a missing path: want an error, got nil")
	}
}
