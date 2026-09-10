package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/config"
)

func settingsKey(code rune) tea.KeyPressMsg     { return tea.KeyPressMsg{Code: code, Text: string(code)} }
func settingsSpecial(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func TestSettingsScreenFlavorToggle(t *testing.T) {
	cfg := config.Default()
	cfg.Defaults.ReshadeFlavor = "addon"
	s := NewSettingsScreen(t.TempDir(), cfg)

	next, _ := s.Update(settingsSpecial(tea.KeyEnter), wizardEnv()) // toggles row 0 (flavor)
	s = next.(*SettingsScreen)
	if s.pending.Defaults.ReshadeFlavor != "normal" {
		t.Errorf("flavor = %q, want normal after one toggle", s.pending.Defaults.ReshadeFlavor)
	}

	next, _ = s.Update(settingsSpecial(tea.KeyEnter), wizardEnv())
	s = next.(*SettingsScreen)
	if s.pending.Defaults.ReshadeFlavor != "addon" {
		t.Errorf("flavor = %q, want addon after a second toggle", s.pending.Defaults.ReshadeFlavor)
	}
}

func TestSettingsScreenEditCacheDir(t *testing.T) {
	s := NewSettingsScreen(t.TempDir(), config.Default())
	s.cursor = int(rowCacheDir)

	next, _ := s.Update(settingsSpecial(tea.KeyEnter), wizardEnv()) // enter edit mode
	s = next.(*SettingsScreen)
	if s.mode != modeEditText {
		t.Fatalf("mode = %v, want modeEditText", s.mode)
	}

	for _, r := range "/custom/cache" {
		next, _ = s.Update(settingsKey(r), wizardEnv())
		s = next.(*SettingsScreen)
	}
	next, _ = s.Update(settingsSpecial(tea.KeyEnter), wizardEnv()) // commit
	s = next.(*SettingsScreen)

	if s.mode != modeRows {
		t.Fatalf("mode = %v, want modeRows after commit", s.mode)
	}
	if s.pending.CacheDir != "/custom/cache" {
		t.Errorf("CacheDir = %q, want /custom/cache", s.pending.CacheDir)
	}
	if !s.dirty() {
		t.Error("the working copy should be dirty after an edit")
	}
}

// Escaping out of an edit must discard it, not commit a partial value.
func TestSettingsScreenEditCancel(t *testing.T) {
	s := NewSettingsScreen(t.TempDir(), config.Default())
	s.cursor = int(rowCacheDir)

	next, _ := s.Update(settingsSpecial(tea.KeyEnter), wizardEnv())
	s = next.(*SettingsScreen)
	for _, r := range "/typo" {
		next, _ = s.Update(settingsKey(r), wizardEnv())
		s = next.(*SettingsScreen)
	}

	_, _, handled := s.HandleBack()
	if !handled {
		t.Fatal("esc should be handled while editing")
	}
	if s.mode != modeRows {
		t.Fatalf("mode after esc = %v, want modeRows", s.mode)
	}
	if s.pending.CacheDir != "" {
		t.Errorf("CacheDir = %q, want unchanged (empty)", s.pending.CacheDir)
	}
}

// A non-positive or non-numeric TTL must be rejected, not silently
// accepted as a value that would break the catalog client.
func TestSettingsScreenTTLValidation(t *testing.T) {
	tests := []struct {
		input   string
		wantSet bool
	}{
		{"48", true},
		{"0", false},
		{"-5", false},
		{"not a number", false},
	}

	for _, tt := range tests {
		s := NewSettingsScreen(t.TempDir(), config.Default())
		s.cursor = int(rowTTL)
		next, _ := s.Update(settingsSpecial(tea.KeyEnter), wizardEnv())
		s = next.(*SettingsScreen)
		s.input.SetValue("") // clear the prefilled "24"

		for _, r := range tt.input {
			next, _ = s.Update(settingsKey(r), wizardEnv())
			s = next.(*SettingsScreen)
		}
		next, _ = s.Update(settingsSpecial(tea.KeyEnter), wizardEnv())
		s = next.(*SettingsScreen)

		got := s.pending.RenodxTTLHours
		if tt.wantSet {
			want := 48
			if got != want {
				t.Errorf("input %q: RenodxTTLHours = %d, want %d", tt.input, got, want)
			}
			if s.mode != modeRows {
				t.Errorf("input %q: should have committed and returned to modeRows", tt.input)
			}
		} else {
			if got != config.Default().RenodxTTLHours {
				t.Errorf("input %q: RenodxTTLHours = %d, want unchanged", tt.input, got)
			}
			if s.mode != modeEditText {
				t.Errorf("input %q: an invalid value should stay in edit mode, not commit", tt.input)
			}
		}
	}
}

// Adding a manual game reuses AddFolderScreen; its handler must mutate
// this exact settings screen's pending config, not a copy.
func TestSettingsScreenAddManualGame(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "MyGame"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	s := NewSettingsScreen(t.TempDir(), config.Default())
	s.cursor = int(rowManualGames)

	next, _ := s.Update(settingsSpecial(tea.KeyEnter), wizardEnv())
	s = next.(*SettingsScreen)
	if s.mode != modeManualGames {
		t.Fatalf("mode = %v, want modeManualGames", s.mode)
	}

	_, cmd := s.updateManualGames(settingsKey('a'))
	if cmd == nil {
		t.Fatal("'a' should push the add-folder screen")
	}
	push, ok := cmd().(pushScreenMsg)
	if !ok {
		t.Fatalf("message = %T, want pushScreenMsg", cmd())
	}
	af, ok := push.screen.(*AddFolderScreen)
	if !ok {
		t.Fatalf("pushed screen = %T, want *AddFolderScreen", push.screen)
	}

	// Simulate the user typing the path and pressing enter in AddFolder.
	for _, r := range filepath.Join(dir, "MyGame") {
		next, _ := af.Update(settingsKey(r), wizardEnv())
		af = next.(*AddFolderScreen)
	}
	_, submitCmd := af.Update(settingsSpecial(tea.KeyEnter), wizardEnv())
	if submitCmd == nil {
		t.Fatal("submitting a valid path should return a command")
	}
	submitCmd() // runs the handler that appends into s.pending, via closure

	if len(s.pending.ManualGames) != 1 {
		t.Fatalf("ManualGames = %v, want 1 entry", s.pending.ManualGames)
	}
	if s.pending.ManualGames[0].Name != "MyGame" {
		t.Errorf("added game name = %q, want MyGame", s.pending.ManualGames[0].Name)
	}
}

func TestSettingsScreenRemoveManualGame(t *testing.T) {
	cfg := config.Default()
	cfg.ManualGames = []config.ManualGame{
		{Name: "A", Path: "/a"},
		{Name: "B", Path: "/b"},
	}
	s := NewSettingsScreen(t.TempDir(), cfg)
	s.mode = modeManualGames
	s.cursor = 0

	next, _ := s.updateManualGames(settingsKey('d'))
	s = next.(*SettingsScreen)

	if len(s.pending.ManualGames) != 1 || s.pending.ManualGames[0].Name != "B" {
		t.Fatalf("ManualGames after remove = %v, want just B", s.pending.ManualGames)
	}
}

// Saving with an invalid pending config (Validate failing) must not
// write anything and must report the problem instead.
func TestSettingsScreenSaveRejectsInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	s := NewSettingsScreen(dir, config.Default())
	s.pending.RenodxTTLHours = -1 // invalid

	next, _ := s.save()
	s = next.(*SettingsScreen)

	if _, err := os.Stat(config.Path(dir)); err == nil {
		t.Error("an invalid config should not have been written to disk")
	}
	if s.status == "" {
		t.Error("a validation failure should set a status message")
	}
	if !s.dirty() {
		t.Error("a rejected save should leave the screen dirty")
	}
}

// A successful save writes config.yaml and clears the dirty flag.
func TestSettingsScreenSaveWritesConfig(t *testing.T) {
	dir := t.TempDir()
	s := NewSettingsScreen(dir, config.Default())
	s.pending.RenodxTTLHours = 48

	if !s.dirty() {
		t.Fatal("test setup: expected the pending copy to differ from saved")
	}

	next, _ := s.save()
	s = next.(*SettingsScreen)

	if s.dirty() {
		t.Error("a successful save should clear the dirty flag")
	}

	reloaded, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load() after save: %v", err)
	}
	if reloaded.RenodxTTLHours != 48 {
		t.Errorf("saved RenodxTTLHours = %d, want 48", reloaded.RenodxTTLHours)
	}
}

// esc at the top-level row view must defer to the shell (pop back to
// Games), not be swallowed here.
func TestSettingsScreenBackAtRowsDefers(t *testing.T) {
	s := NewSettingsScreen(t.TempDir(), config.Default())
	_, _, handled := s.HandleBack()
	if handled {
		t.Error("HandleBack() at the row list should defer to the shell")
	}
}

// The manual-games list is as long as the user has made it, so it must
// scroll around the cursor rather than printing past the window.
func TestSettingsManualGamesListScrolls(t *testing.T) {
	cfg := config.Default()
	for i := 0; i < 40; i++ {
		cfg.ManualGames = append(cfg.ManualGames, config.ManualGame{
			Name: fmt.Sprintf("Game %02d", i), Path: fmt.Sprintf("/games/g%02d", i),
		})
	}
	s := NewSettingsScreen(t.TempDir(), cfg)
	s.mode = modeManualGames
	s.cursor = 30

	env := Env{Styles: NewStyles(true), Width: 80, Height: 21}
	body := s.View(env)
	if got := countLines(body); got > env.Height {
		t.Errorf("manual games rendered %d lines into a height of %d:\n%s", got, env.Height, body)
	}
	if !strings.Contains(body, "more above") || !strings.Contains(body, "more below") {
		t.Errorf("a scrolled list should say how many rows are hidden at each end:\n%s", body)
	}
	if !strings.Contains(body, "Game 30") {
		t.Errorf("the row under the cursor must stay visible:\n%s", body)
	}
}
