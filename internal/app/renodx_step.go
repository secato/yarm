package app

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/install"
)

// The RenoDX step. RenoDX has a mod per game rather than per taste, so
// unlike the shaders and add-ons steps this one usually has a right
// answer — and upstream publishes the Steam app id that identifies it.
// The step is therefore built around showing that one mod rather than
// around browsing two hundred, with the search as the way out when there
// is no match or the user wants something else.

// renodxNoneID is the row that means "no mod". Choosing nothing has to be
// something you can see and do, not the absence of an action.
const renodxNoneID = ""

// renodxSummary names the choice for the running summary and the hub.
func (s *WizardScreen) renodxSummary() string {
	if s.renodxChoice == "" {
		return "no RenoDX"
	}
	if m, ok := s.renodxMod(s.renodxChoice); ok {
		return "RenoDX: " + m.Title
	}
	return "RenoDX: " + s.renodxChoice
}

// renodxMod finds a mod by id in the loaded catalog.
func (s *WizardScreen) renodxMod(id string) (catalog.RenoMod, bool) {
	for _, m := range s.data.RenoDX {
		if m.ID == id {
			return m, true
		}
	}
	return catalog.RenoMod{}, false
}

// renodxForDownload drops a choice made while the add-on build was on
// offer if the build has since switched back.
//
// The same rule as addonsForDownload, for the same reason: the normal
// build cannot load a RenoDX add-on, Request.Validate rejects a request
// that tries, and so a stale choice here would block the install rather
// than merely not install a mod.
func renodxForDownload(flavor install.Flavor, chosen string) string {
	if !flavor.Addon() {
		return ""
	}
	return chosen
}

// matchRenoDX finds the mod upstream says belongs to this game.
//
// yarm's Steam ids are "steam:<appid>" and RenoDX publishes the same app
// id, so the match is a parse and a compare. It is the whole reason this
// step can lead with an answer instead of a list.
func matchRenoDX(mods []catalog.RenoMod, gameID string) (catalog.RenoMod, bool) {
	rest, ok := strings.CutPrefix(gameID, "steam:")
	if !ok {
		return catalog.RenoMod{}, false
	}
	appID, err := strconv.Atoi(rest)
	if err != nil || appID == 0 {
		return catalog.RenoMod{}, false
	}
	for _, m := range mods {
		if m.SteamAppID == appID {
			return m, true
		}
	}
	return catalog.RenoMod{}, false
}

// renodxRow turns one mod into its row, carrying the reason it cannot be
// installed here when there is one.
func (s *WizardScreen) renodxRow(m catalog.RenoMod, matched bool) selectItem {
	it := selectItem{
		ID:          m.ID,
		Name:        m.Title,
		Description: m.Description,
		Cached:      s.deps.CacheStatus != nil && s.deps.CacheStatus.HasRenoDX(m.ID),
	}
	if it.Description == "" && len(m.Maintainers) > 0 {
		it.Description = "maintained by " + strings.Join(m.Maintainers, ", ")
	}

	switch {
	case m.Unsupported() != "":
		it.Disabled = true
		it.DisabledNote = "yarm does not support " + m.Unsupported() + " yet"
	default:
		if _, ok := m.ArtifactFor(s.exe.Arch); !ok {
			it.Disabled = true
			it.DisabledNote = "no " + string(s.exe.Arch) + " build"
		}
	}

	switch {
	case matched:
		it.Note = "matches this game"
	case m.Beta():
		it.Note, it.NoteWarn = "beta", true
	case m.Unverified():
		it.Note = "unverified"
	}
	return it
}

// renodxRows builds the step's rows: the mod for this game first under
// its own heading, then everything else.
//
// The matched mod is highlighted and named but never pre-ticked. The
// add-on build is the one anti-cheat can detect and the wizard says so on
// every screen it matters; quietly adding a second injected DLL to every
// install would contradict that.
func (s *WizardScreen) renodxRows() []selectItem {
	if s.renodxFull == nil {
		s.renodxFull = s.buildRenoDXRows()
	}
	return s.renodxFull
}

// buildRenoDXRows assembles the unfiltered RenoDX list once per data load:
// one cache probe per mod makes rebuilding it per keystroke a stat storm.
func (s *WizardScreen) buildRenoDXRows() []selectItem {
	none := selectItem{ID: renodxNoneID, Name: "No RenoDX mod"}
	rows := []selectItem{none}

	games := s.data.RenoDXGames()
	matched, hasMatch := matchRenoDX(games, s.entry.ID)
	if hasMatch {
		rows = append(rows,
			selectItem{Header: true, Name: "── For this game ──"},
			s.renodxRow(matched, true),
			selectItem{Header: true, Name: "── All mods ──"})
	}
	for _, m := range games {
		if hasMatch && m.ID == matched.ID {
			continue
		}
		rows = append(rows, s.renodxRow(m, false))
	}
	return rows
}

// filterRenoDX narrows the rows to a case-insensitive substring of the
// title or id.
//
// Unlike curate, this does not keep the chosen row visible: a search that
// cannot hide everything is not a search. That is exactly why the answer
// lives in renodxChoice rather than being read back out of the visible
// rows — see the field's comment.
//
// Headings go too. There is no "for this game" grouping to preserve
// inside a set of search results, and the "no mod" row would be a
// confusing thing to match "no" against.
func filterRenoDX(items []selectItem, query string) []selectItem {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return items
	}
	out := make([]selectItem, 0, len(items))
	for _, it := range items {
		if it.Header || it.ID == renodxNoneID {
			continue
		}
		if strings.Contains(strings.ToLower(it.Name), q) || strings.Contains(strings.ToLower(it.ID), q) {
			out = append(out, it)
		}
	}
	return out
}

// renodxVersionOK reports whether the chosen ReShade is new enough to
// load a RenoDX add-on at all.
func (s *WizardScreen) renodxVersionOK() bool {
	v, ok := s.selectedVersion()
	if !ok {
		return true
	}
	return catalog.CompareVersions(v.Version, catalog.RenoDXMinReShade) >= 0
}

// handleRenoDXKey routes the step's keys, with the search taking
// precedence over everything while it is open.
func (s *WizardScreen) handleRenoDXKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	if s.renodxFiltering {
		// Compared by string rather than through the keymap because the
		// screen's own bindings are bypassed wholesale while typing —
		// the same shape GamesScreen's filter uses.
		switch msg.String() {
		case "enter": // keep the query, leave the field
			s.renodxFiltering = false
			s.renodxFilter.Blur()
			return s, nil
		case "esc": // clear the query and show everything again
			s.renodxFiltering = false
			s.renodxFilter.Blur()
			s.renodxFilter.SetValue("")
			s.refreshRenoDX()
			return s, nil
		}
		var cmd tea.Cmd
		s.renodxFilter, cmd = s.renodxFilter.Update(msg)
		s.refreshRenoDX()
		return s, cmd
	}

	switch {
	case key.Matches(msg, s.keys.Filter):
		s.renodxFiltering = true
		return s, s.renodxFilter.Focus()
	case key.Matches(msg, s.keys.Up):
		s.renodx.up()
	case key.Matches(msg, s.keys.Down):
		s.renodx.down()
	case key.Matches(msg, s.keys.Toggle):
		s.chooseRenoDX()
		s.missingGen++
	case key.Matches(msg, s.keys.Enter):
		s.step = s.afterStep(stepRenoDX)
	}
	return s, nil
}

// chooseRenoDX takes the row under the cursor as the answer.
func (s *WizardScreen) chooseRenoDX() {
	i := s.renodx.cursor
	if i < 0 || i >= len(s.renodx.items) {
		return
	}
	it := s.renodx.items[i]
	// A disabled row names something that cannot be installed here; the
	// "no mod" row is not disabled and is a real answer.
	if it.Header || it.Disabled {
		return
	}
	s.renodxChoice = it.ID
	s.renodx.chooseOnly(it.ID)
}

// CapturesInput tells the shell to route plain keys here while the RenoDX
// search is open.
//
// Without it the shell's global bindings win: "q" would quit the app
// mid-search and "esc" would step the wizard back rather than close the
// search. Checked at Model.handleKey before any global binding.
func (s *WizardScreen) CapturesInput() bool {
	return s.step == stepRenoDX && s.renodxFiltering
}

// viewRenoDX renders the step.
func (s *WizardScreen) viewRenoDX(b *strings.Builder, env Env, height int) {
	if len(s.data.RenoDX) == 0 {
		b.WriteString(env.Styles.Faint.Render(wrap(
			"RenoDX's mod list could not be loaded — offline, with nothing cached yet? "+
				"Everything else about this install still works.", env.Width-1)))
		b.WriteString("\n")
		return
	}

	if !s.renodxVersionOK() {
		v, _ := s.selectedVersion()
		b.WriteString(env.Styles.Warn.Render(wrap(fmt.Sprintf(
			"RenoDX needs ReShade %s or newer, and you chose %s.",
			catalog.RenoDXMinReShade, v.Version), env.Width-1)))
		b.WriteString("\n\n")
		height -= 2
	}

	games := s.data.RenoDXGames()
	if _, ok := matchRenoDX(games, s.entry.ID); !ok && s.renodxFilter.Value() == "" {
		b.WriteString(env.Styles.Faint.Render(wrap(fmt.Sprintf(
			"RenoDX has no mod for %s. Press / to search all %d, or enter to skip.",
			s.entry.Name, len(games)), env.Width-1)))
		b.WriteString("\n\n")
		height -= 2
	}

	// One row for the filter line below.
	writeSelectList(b, env, s.renodx, height-1)

	if s.renodxFiltering || s.renodxFilter.Value() != "" {
		b.WriteString(s.renodxFilter.View())
		b.WriteString("\n")
	}
}

// hubRenoDXLines is the step's pane in edit mode.
func (s *WizardScreen) hubRenoDXLines(env Env, width int) []string {
	if s.renodxChoice == "" {
		return []string{env.Styles.Faint.Render("no RenoDX mod")}
	}
	m, ok := s.renodxMod(s.renodxChoice)
	if !ok {
		// Recorded but no longer in the catalog: still installed, still
		// removable, just not describable.
		return []string{env.Styles.Warn.Render(clipTail(s.renodxChoice+" (not in the catalog)", width))}
	}
	return []string{clipTail(m.Title, width)}
}

// renodxUnmet reports what would stop a chosen mod from working, for the
// review page's Check block. Reported rather than blocked: both are
// decided on earlier steps and are still changeable.
func (s *WizardScreen) renodxUnmet() []string {
	id := renodxForDownload(s.flavor, s.renodxChoice)
	if id == "" {
		return nil
	}

	var out []string
	if !s.renodxVersionOK() {
		v, _ := s.selectedVersion()
		out = append(out, fmt.Sprintf("RenoDX needs ReShade %s or newer (you chose %s)",
			catalog.RenoDXMinReShade, v.Version))
	}
	// Both replace the game's tone mapping, so running them together
	// gives whichever loads second — not a blend, and not a choice.
	if s.addons.selected[autoHDRAddonID] {
		out = append(out, "RenoDX and AutoHDR both replace the game's tone mapping; pick one")
	}
	return out
}

// addonRenoDXUnmet is renodxUnmet's counterpart for RenoDX's utility
// mods, chosen from the Add-ons list rather than the RenoDX step: they
// link the same framework, so the same ReShade-version floor applies.
func (s *WizardScreen) addonRenoDXUnmet() []string {
	if s.renodxVersionOK() {
		return nil
	}
	v, _ := s.selectedVersion()
	var out []string
	for _, id := range addonsForDownload(s.flavor, s.addons) {
		m, ok := s.renodxMod(id)
		if !ok || !m.Utility {
			continue
		}
		out = append(out, fmt.Sprintf("%s needs ReShade %s or newer (you chose %s)",
			m.Title, catalog.RenoDXMinReShade, v.Version))
	}
	return out
}

// renodxCatalogAddonID is RenoDX's own entry in crosire's add-on
// catalog, which carries no download URL and so renders as manual-only.
const renodxCatalogAddonID = "renodx-by-shortfuse"

// autoHDRAddonID is the catalog add-on that conflicts with RenoDX.
const autoHDRAddonID = "autohdr-by-endlesslyflowering-original-by-majorpainthecactus"
