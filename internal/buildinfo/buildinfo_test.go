package buildinfo

import "testing"

func TestString(t *testing.T) {
	// Save and restore: these are package-level vars set at link time via
	// -ldflags, and other tests (or a real build) must not see values
	// this test injected.
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldVersion, oldCommit, oldDate })

	Version, Commit, Date = "1.2.3", "abc1234", "2026-09-05T00:00:00Z"

	want := "yarm 1.2.3 (abc1234, 2026-09-05T00:00:00Z)"
	if got := String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// The unbuilt defaults must themselves produce a sane, non-empty summary
// — this is what `go run`/`go test` actually see, since -ldflags only
// applies to a real release build.
func TestStringDefaults(t *testing.T) {
	if got := String(); got != "yarm dev (none, unknown)" {
		t.Errorf("String() with default vars = %q, want %q", got, "yarm dev (none, unknown)")
	}
}
