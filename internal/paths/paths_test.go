package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/adrg/xdg"
)

func TestResolveYarmHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "yarm-home")
	t.Setenv("YARM_HOME", home)

	d := Resolve()

	want := Dirs{
		Config: filepath.Join(home, "config"),
		Data:   filepath.Join(home, "data"),
		Cache:  filepath.Join(home, "cache"),
	}
	if d != want {
		t.Errorf("Resolve() = %+v, want %+v", d, want)
	}
}

func TestResolveXDGDefaults(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("YARM_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	d := Resolve()

	if runtime.GOOS == "windows" {
		// ConfigHome and DataHome both collapse to the same base on
		// Windows; only assert the invariants that hold there.
		if d.Config != d.Data {
			t.Errorf("Config and Data should match on windows: %+v", d)
		}
		if filepath.Dir(d.Cache) != d.Config {
			t.Errorf("Cache should be a subfolder of Config on windows: %+v", d)
		}
		return
	}

	want := Dirs{
		Config: filepath.Join(tmp, "config", appName),
		Data:   filepath.Join(tmp, "data", appName),
		Cache:  filepath.Join(tmp, "cache", appName),
	}
	if d != want {
		t.Errorf("Resolve() = %+v, want %+v", d, want)
	}
}

func TestEnsureAll(t *testing.T) {
	tmp := t.TempDir()
	d := Dirs{
		Config: filepath.Join(tmp, "config"),
		Data:   filepath.Join(tmp, "data"),
		Cache:  filepath.Join(tmp, "cache"),
	}

	if err := d.EnsureAll(); err != nil {
		t.Fatalf("EnsureAll() error = %v", err)
	}

	for _, dir := range []string{d.Config, d.Data, d.Cache} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("stat %s: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}
}
