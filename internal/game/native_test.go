package game

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBytes(t *testing.T, root, rel string, b []byte) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, b, 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", p, err)
	}
}

var (
	elfMagic   = []byte{0x7f, 'E', 'L', 'F', 0x02, 0x01, 0x01, 0x00}
	machOMagic = []byte{0xcf, 0xfa, 0xed, 0xfe, 0x0c, 0x00, 0x00, 0x01}
	peMagic    = []byte{'M', 'Z', 0x90, 0x00, 0x03, 0x00, 0x00, 0x00}
)

func TestHasNativeBuild(t *testing.T) {
	tests := []struct {
		name  string
		files map[string][]byte
		want  bool
	}{
		{
			name:  "linux native at depth 4 (Source layout)",
			files: map[string][]byte{"game/bin/linuxsteamrt64/ridgeline": elfMagic},
			want:  true,
		},
		{
			name:  "unity linux native",
			files: map[string][]byte{"Game.x86_64": elfMagic},
			want:  true,
		},
		{
			name:  "macos app bundle",
			files: map[string][]byte{"Game.app/Contents/MacOS/Game": machOMagic},
			want:  true,
		},
		{
			name:  "windows game is not native",
			files: map[string][]byte{"Game.exe": peMagic},
			want:  false,
		},
		{
			name: "extensionless data files are not native",
			files: map[string][]byte{
				"game/core/gameinfo": []byte("\"GameInfo\"\n{\n}\n"),
				"game/bin/vidcfg":    {0x00, 0x01, 0x02, 0x03},
			},
			want: false,
		},
		{
			name:  "native binary beyond scan depth is not found",
			files: map[string][]byte{"a/b/c/d/game": elfMagic},
			want:  false,
		},
		{
			name:  "native binary inside a skipped dir is not found",
			files: map[string][]byte{"_CommonRedist/helper": elfMagic},
			want:  false,
		},
		{
			name:  "empty root",
			files: map[string][]byte{},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for rel, content := range tt.files {
				writeBytes(t, root, rel, content)
			}
			if got := HasNativeBuild(root); got != tt.want {
				t.Errorf("HasNativeBuild() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A native-only game must produce no installable executables *and* report a
// native build: that pair is what lets the UI explain itself.
func TestNativeGameHasNoExecutables(t *testing.T) {
	root := t.TempDir()
	writeBytes(t, root, "game/bin/linuxsteamrt64/ridgeline", elfMagic)
	writeBytes(t, root, "game/ridgeline.sh", []byte("#!/bin/sh\n"))

	exes, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(exes) != 0 {
		t.Errorf("Scan() found %d executables, want 0", len(exes))
	}
	if !HasNativeBuild(root) {
		t.Error("HasNativeBuild() = false, want true")
	}
}
