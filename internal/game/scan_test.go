package game

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, root string, rel string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", p, err)
	}
}

func TestScan(t *testing.T) {
	root := t.TempDir()

	kept := []string{
		"Game.exe",                   // depth 1
		"Game/GameBin/eldenring.exe", // depth 3 (containing dir GameBin is depth 2, allowed)
	}
	skippedButPresent := []string{
		"unins000.exe",                   // skip filename
		"dotnetfx.exe",                   // skip filename
		"Game/GameBin/CrashReporter.exe", // skip filename, inside an otherwise-normal dir
	}
	excludedEntirely := []string{
		"Game/GameBin/x64/eldenring64.exe",          // depth 4, beyond max scan depth
		"Redist/CrashRep.exe",                       // "Redist" is a skip dir
		"_CommonRedist/vcredist_x64.exe",            // "_CommonRedist" is a skip dir
		"Support/EasyAntiCheat/EasyAntiCheat.exe",   // "Support" is a skip dir
		"Engine/Extras/ThirdPartyNotices/notes.exe", // "Engine/Extras" is a skip sequence
	}
	notAnExe := "notes.txt"

	for _, f := range kept {
		touch(t, root, f)
	}
	for _, f := range skippedButPresent {
		touch(t, root, f)
	}
	for _, f := range excludedEntirely {
		touch(t, root, f)
	}
	touch(t, root, notAnExe)

	got, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	byPath := make(map[string]Executable, len(got))
	for _, e := range got {
		byPath[filepath.ToSlash(e.Path)] = e
	}

	for _, path := range kept {
		e, ok := byPath[path]
		if !ok {
			t.Errorf("expected %s to be scanned", path)
			continue
		}
		if e.Skipped {
			t.Errorf("%s: Skipped = true, want false", path)
		}
	}

	for _, path := range skippedButPresent {
		e, ok := byPath[path]
		if !ok {
			t.Errorf("expected %s to still be scanned (flagged, not hidden)", path)
			continue
		}
		if !e.Skipped {
			t.Errorf("%s: Skipped = false, want true", path)
		}
	}

	for _, path := range excludedEntirely {
		if _, ok := byPath[path]; ok {
			t.Errorf("%s should not appear in scan results at all", path)
		}
	}

	if _, ok := byPath[notAnExe]; ok {
		t.Errorf("non-.exe file should not be scanned")
	}
}

func TestIsSkippedDir(t *testing.T) {
	tests := []struct {
		rel  string
		want bool
	}{
		{".", false},
		{"Game", false},
		{"Game/GameBin", false},
		{"_CommonRedist", true},
		{"redist", true},
		{"REDIST", true},
		{"Support", true},
		{"EasyAntiCheat", true},
		{"Engine/Extras", true},
		{"Engine/Extras/More", true},
		{"Foo/Engine/Extras", true},
		{"Engine", false},
		{"Extras", false},
	}

	for _, tt := range tests {
		if got := isSkippedDir(tt.rel); got != tt.want {
			t.Errorf("isSkippedDir(%q) = %v, want %v", tt.rel, got, tt.want)
		}
	}
}
