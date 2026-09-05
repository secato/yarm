package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestLoadCreatesDefaultOnFirstRun(t *testing.T) {
	dir := t.TempDir()

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if diff := cmp.Diff(Default(), cfg); diff != "" {
		t.Errorf("Load() mismatch (-want +got):\n%s", diff)
	}

	if _, err := os.Stat(Path(dir)); err != nil {
		t.Errorf("config.yaml was not created: %v", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	cfg := Config{
		CacheDir: filepath.Join(dir, "cache"),
		ManualGames: []ManualGame{
			{Name: "My GOG game", Path: "/games/foo"},
		},
		Steam: SteamConfig{
			Enabled:           false,
			ExtraLibraryPaths: []string{"/mnt/extra"},
		},
		Defaults: DefaultsConfig{
			ReshadeFlavor: "normal",
			Packages:      []string{"standard", "sweetfx"},
		},
		CatalogTTLHours: 48,
	}

	if err := Save(dir, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.CacheDir != cfg.CacheDir ||
		len(got.ManualGames) != 1 || got.ManualGames[0] != cfg.ManualGames[0] ||
		got.Steam.Enabled != cfg.Steam.Enabled ||
		len(got.Steam.ExtraLibraryPaths) != 1 || got.Steam.ExtraLibraryPaths[0] != "/mnt/extra" ||
		got.Defaults.ReshadeFlavor != cfg.Defaults.ReshadeFlavor ||
		len(got.Defaults.Packages) != 2 ||
		got.CatalogTTLHours != cfg.CatalogTTLHours {
		t.Errorf("round trip mismatch: got %+v, want %+v", got, cfg)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"valid defaults", Default(), false},
		{"bad flavor", func() Config { c := Default(); c.Defaults.ReshadeFlavor = "bogus"; return c }(), true},
		{"zero ttl", func() Config { c := Default(); c.CatalogTTLHours = 0; return c }(), true},
		{"negative ttl", func() Config { c := Default(); c.CatalogTTLHours = -1; return c }(), true},
		{"manual game missing name", func() Config {
			c := Default()
			c.ManualGames = []ManualGame{{Name: "", Path: "/x"}}
			return c
		}(), true},
		{"manual game missing path", func() Config {
			c := Default()
			c.ManualGames = []ManualGame{{Name: "x", Path: ""}}
			return c
		}(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	bad := []byte("defaults:\n  reshade_flavor: bogus\n  packages: []\ncatalog_ttl_hours: 24\n")
	if err := os.WriteFile(Path(dir), bad, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := Load(dir); err == nil {
		t.Error("Load() with invalid flavor: expected error, got nil")
	}
}
