package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/fetch"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
	"github.com/secato/yarm/internal/platform"
)

// A fresh cache misses; a set one hits until it expires or is cleared.
func TestGamesCacheHitMissExpireClear(t *testing.T) {
	now := time.Now()
	c := &GamesCache{Now: func() time.Time { return now }}

	if _, _, ok := c.Get(); ok {
		t.Fatal("a fresh cache should miss")
	}

	entries := []GameEntry{{Game: game.Game{ID: "manual:x", Name: "X"}}}
	c.Set(entries, nil)
	got, err, ok := c.Get()
	if !ok || err != nil || len(got) != 1 {
		t.Fatalf("Get() = %d entries, %v, %v; want the one set", len(got), err, ok)
	}
	// A copy, not an alias: screens must not be able to poison the cache.
	got[0].Name = "MUTATED"
	if again, _, _ := c.Get(); again[0].Name != "X" {
		t.Error("Get should return a copy of the cached entries")
	}

	now = now.Add(gamesCacheTTL)
	if _, _, ok := c.Get(); ok {
		t.Error("an expired cache should miss")
	}

	now = time.Now()
	c.Set(entries, nil)
	c.Clear()
	if _, _, ok := c.Get(); ok {
		t.Error("a cleared cache should miss")
	}

	// Nil-safe throughout: callers with no cache never check.
	var nilCache *GamesCache
	nilCache.Set(entries, nil)
	nilCache.Clear()
	if _, _, ok := nilCache.Get(); ok {
		t.Error("a nil cache should always miss")
	}
}

// countingProvider records how often discovery ran.
type countingProvider struct {
	calls int
	games []game.Game
	err   error
}

func (p *countingProvider) Name() string { return "counting" }

func (p *countingProvider) Discover(context.Context) ([]game.Game, error) {
	p.calls++
	return p.games, p.err
}

// Discovery runs once no matter how often the games screen loads: reopening
// the wizard, popping back up the stack, and the post-apply reload all hit
// the cache instead of re-walking the disk.
func TestLoaderCachesDiscovery(t *testing.T) {
	root := t.TempDir()
	p := &countingProvider{games: []game.Game{{ID: "manual:x", Name: "X", Root: root}}}
	l := ProviderLoader{Providers: []platform.Provider{p}, StateDir: t.TempDir(), Cache: NewGamesCache()}

	for range 3 {
		entries, err := l.LoadGames(context.Background())
		if err != nil {
			t.Fatalf("LoadGames() error = %v", err)
		}
		if len(entries) != 1 || entries[0].Name != "X" {
			t.Fatalf("entries = %v, want the one game", entries)
		}
	}
	if p.calls != 1 {
		t.Errorf("discovery ran %d times for 3 loads, want once", p.calls)
	}

	l.InvalidateCache()
	if _, err := l.LoadGames(context.Background()); err != nil {
		t.Fatalf("LoadGames() error = %v", err)
	}
	if p.calls != 2 {
		t.Errorf("discovery ran %d times after invalidate, want twice total", p.calls)
	}
}

// invalidatingLoader records rescan invalidations.
type invalidatingLoader struct {
	fakeLoader
	invalidated int
}

func (l *invalidatingLoader) InvalidateCache() { l.invalidated++ }

// R means "look again out there": it drops the memoized discovery before
// reloading, alongside the catalog refresh.
func TestRescanInvalidatesGameCache(t *testing.T) {
	l := &invalidatingLoader{fakeLoader: fakeLoader{entries: sampleEntries()}}
	s := NewGamesScreen(l, fakeDeps(), false)

	_, cmd := s.Update(tea.KeyPressMsg{Code: 'r', Text: "r"}, wizardEnv())
	if cmd == nil {
		t.Fatal("rescan should return a command")
	}
	drainCmd(cmd)

	if l.invalidated != 1 {
		t.Errorf("rescan invalidated %d times, want once", l.invalidated)
	}
}

// installTestCache fabricates a cache holding one ReShade build: enough
// for the real installer to resolve without touching the network.
func installTestCache(t *testing.T) *cache.Cache {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "reshade", "6.8.0", "normal")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, n := range []string{artifacts.ReShade32, artifacts.ReShade64} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n+" body"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return cache.New(root, nil)
}

func installTestRequest(gameRoot string) install.Request {
	return install.Request{
		Game:     game.Game{ID: "manual:x", Name: "X", Root: gameRoot},
		Exe:      game.Executable{Path: "Game/ember.exe", Arch: game.ArchX64, API: game.APID3D12},
		Version:  "6.8.0",
		Flavor:   install.FlavorNormal,
		DLLName:  "dxgi.dll",
		TargetOS: artifacts.OSWindows,
	}
}

// Landing or removing an install clears the cache: the games list reloads
// after every apply and must show what just happened.
func TestApplyClearsGameCache(t *testing.T) {
	gameRoot := t.TempDir()
	stateDir := t.TempDir()
	games := NewGamesCache()
	stale := []GameEntry{{Game: game.Game{ID: "manual:x", Name: "X", Root: gameRoot}}}

	installer := &RealInstaller{Cache: installTestCache(t), StateDir: stateDir, Games: games}
	games.Set(stale, nil)
	if _, err := installer.Install(context.Background(), installTestRequest(gameRoot), func(ProgressUpdate) {}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, _, ok := games.Get(); ok {
		t.Error("a landed install should clear the game cache")
	}

	uninstaller := RealUninstaller{StateDir: stateDir, Games: games}
	games.Set(stale, nil)
	if _, err := uninstaller.Uninstall(install.UninstallRequest{
		GameID: "manual:x",
		Exe:    "Game/ember.exe",
	}); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if _, _, ok := games.Get(); ok {
		t.Error("a landed uninstall should clear the game cache")
	}
}

// A failed apply changes nothing, so the cache survives it.
func TestFailedApplyKeepsGameCache(t *testing.T) {
	games := NewGamesCache()
	stale := []GameEntry{{Game: game.Game{ID: "manual:x", Name: "X"}}}

	// No ReShade in this cache, and nowhere to fetch it from: resolve
	// fails before anything is written.
	f := fetch.New("yarm/test")
	f.Retries = 0 // fail the one attempt instead of backing off for seconds
	empty := cache.New(t.TempDir(), f)
	empty.SetupURL = func(string, bool) string { return "http://127.0.0.1:1/ReShade_Setup_6.8.0.exe" }
	installer := &RealInstaller{Cache: empty, StateDir: t.TempDir(), Games: games}
	games.Set(stale, nil)
	if _, err := installer.Install(context.Background(), installTestRequest(t.TempDir()), func(ProgressUpdate) {}); err == nil {
		t.Fatal("want a resolve error, got nil")
	} else if !strings.Contains(err.Error(), "reshade") {
		t.Fatalf("want a missing-artifact error, got %v", err)
	}
	if _, _, ok := games.Get(); !ok {
		t.Error("a failed install should leave the game cache alone")
	}
}
