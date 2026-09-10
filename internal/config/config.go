// Package config handles loading, saving, defaulting and validating the
// user-edited config.yaml.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"

	"github.com/secato/yarm/internal/fsutil"
)

// FileName is the config file's name inside the config directory.
const FileName = "config.yaml"

// Config is the parsed contents of config.yaml.
type Config struct {
	CacheDir        string         `yaml:"cache_dir"`
	ManualGames     []ManualGame   `yaml:"manual_games"`
	Steam           SteamConfig    `yaml:"steam"`
	Defaults        DefaultsConfig `yaml:"defaults"`
	CatalogTTLHours int            `yaml:"catalog_ttl_hours"`
}

// ManualGame is a user-added game folder.
type ManualGame struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// SteamConfig controls Steam library auto-detection.
type SteamConfig struct {
	Enabled           bool     `yaml:"enabled"`
	ExtraLibraryPaths []string `yaml:"extra_library_paths"`
}

// DefaultsConfig holds install-wizard preselections.
type DefaultsConfig struct {
	ReshadeFlavor string   `yaml:"reshade_flavor"` // "normal" or "addon"
	Packages      []string `yaml:"packages"`
}

// Default returns the built-in configuration used when no config.yaml
// exists yet.
func Default() Config {
	return Config{
		CacheDir:    "",
		ManualGames: []ManualGame{},
		Steam: SteamConfig{
			Enabled:           true,
			ExtraLibraryPaths: []string{},
		},
		Defaults: DefaultsConfig{
			// Normal, not addon: the add-on build is what anti-cheat
			// detects, so it has to be something the user asked for.
			ReshadeFlavor: "normal",
			Packages:      []string{"standard"},
		},
		CatalogTTLHours: 24,
	}
}

const defaultTemplate = `# YARM configuration
cache_dir: ""                 # empty = default cache dir
manual_games: []               # folders added by the user
  # - name: "My GOG game"
  #   path: "/games/foo"
steam:
  enabled: true
  extra_library_paths: []     # if auto-detection misses a library
defaults:
  reshade_flavor: normal      # normal | addon (addon builds are detectable by anti-cheat)
  packages: ["standard"]      # package ids preselected in the wizard
catalog_ttl_hours: 24         # how long the RenoDX mod index is trusted (placeholder; final cache times TBD)
`

// Path returns the full path to config.yaml inside dir.
func Path(dir string) string {
	return filepath.Join(dir, FileName)
}

// Load reads config.yaml from dir, creating it with commented defaults if
// missing. The returned config has been validated.
func Load(dir string) (Config, error) {
	path := Path(dir)

	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		if err := fsutil.AtomicWrite(path, []byte(defaultTemplate), 0o644); err != nil {
			return Config{}, fmt.Errorf("write default config: %w", err)
		}
		data = []byte(defaultTemplate)
	case err != nil:
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}

	return cfg, nil
}

// Save writes cfg to dir/config.yaml atomically.
func Save(dir string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return fsutil.AtomicWrite(Path(dir), data, 0o644)
}

var validFlavors = map[string]bool{"normal": true, "addon": true}

// Validate checks invariants that struct tags can't express.
func (c Config) Validate() error {
	if !validFlavors[c.Defaults.ReshadeFlavor] {
		return fmt.Errorf("defaults.reshade_flavor must be %q or %q, got %q",
			"normal", "addon", c.Defaults.ReshadeFlavor)
	}
	if c.CatalogTTLHours <= 0 {
		return fmt.Errorf("catalog_ttl_hours must be positive, got %d", c.CatalogTTLHours)
	}
	for i, g := range c.ManualGames {
		if g.Name == "" {
			return fmt.Errorf("manual_games[%d]: name is required", i)
		}
		if g.Path == "" {
			return fmt.Errorf("manual_games[%d]: path is required", i)
		}
	}
	return nil
}
