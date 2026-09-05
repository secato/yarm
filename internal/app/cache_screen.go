package app

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"github.com/secato/yarm/internal/artifacts"
	"github.com/secato/yarm/internal/cache"
	"github.com/secato/yarm/internal/catalog"
	"github.com/secato/yarm/internal/fsutil"
)

// cacheSortCycle is the order "s" steps through, per §6.1
// ("s cycles sort (size/date/name)").
var cacheSortCycle = []cache.SortBy{cache.SortBySize, cache.SortByDate, cache.SortByName}

// cacheLoadedMsg carries a refreshed listing plus disk totals.
type cacheLoadedMsg struct {
	entries []cache.Entry
	total   int64
	free    int64
	freeErr error
}

// cacheResultMsg is what every cache-screen operation (load, delete,
// refresh) resolves to. Deliberately not routed through Async's generic
// error-to-overlay handling: an error there bypasses the screen entirely,
// which would leave "loading…" or "refreshing…" showing forever once the
// error dialog closed, with no way to tell the flag was ever supposed to
// clear.
type cacheResultMsg struct {
	data cacheLoadedMsg
	err  error
}

// cacheCmd wraps work that produces a cache listing (or fails) into a
// tea.Cmd that always reaches CacheScreen.Update, success or not.
func cacheCmd(work func() (cacheLoadedMsg, error)) tea.Cmd {
	return func() tea.Msg {
		data, err := work()
		return cacheResultMsg{data: data, err: err}
	}
}

// cacheDataFor bundles what cacheLoadedMsg needs, computed off the UI
// goroutine.
func cacheDataFor(c *cache.Cache, by cache.SortBy) (cacheLoadedMsg, error) {
	entries, err := c.List(by, true)
	if err != nil {
		return cacheLoadedMsg{}, err
	}
	total, err := c.Total()
	if err != nil {
		return cacheLoadedMsg{}, err
	}
	free, freeErr := fsutil.FreeSpace(c.Root)
	return cacheLoadedMsg{entries: entries, total: total, free: free, freeErr: freeErr}, nil
}

// CacheScreen lists cached artifacts: what is taking up space, when it
// was fetched and last used.
type CacheScreen struct {
	keys  KeyMap
	cache *cache.Cache

	loading    bool
	entries    []cache.Entry
	total      int64
	free       int64
	freeErr    error
	sortIdx    int
	table      table.Model
	refreshing bool
}

// NewCacheScreen returns the cache manager, which loads its listing on
// Init.
func NewCacheScreen(c *cache.Cache) *CacheScreen {
	return &CacheScreen{
		keys:    DefaultKeyMap(),
		cache:   c,
		loading: true,
		table:   table.New(table.WithFocused(true)),
	}
}

func (s *CacheScreen) sortBy() cache.SortBy { return cacheSortCycle[s.sortIdx] }

// Init implements Screen.
func (s *CacheScreen) Init() tea.Cmd { return s.load() }

func (s *CacheScreen) load() tea.Cmd {
	return cacheCmd(func() (cacheLoadedMsg, error) { return cacheDataFor(s.cache, s.sortBy()) })
}

// Title implements Screen.
func (s *CacheScreen) Title() string {
	if s.loading {
		return "cache — loading…"
	}
	return fmt.Sprintf("cache — %d entries, %s", len(s.entries), humanSize(s.total))
}

// KeyBindings implements Screen.
func (s *CacheScreen) KeyBindings() []key.Binding {
	return []key.Binding{cacheSortBinding, cacheDeleteBinding, cacheRefreshBinding, s.keys.Back}
}

var (
	cacheSortBinding    = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort"))
	cacheDeleteBinding  = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
	cacheRefreshBinding = key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh package"))
)

// HandleBack implements backHandler only so the cache screen can be
// reached from Games and returned from with the ordinary pop; it always
// defers.
func (s *CacheScreen) HandleBack() (Screen, tea.Cmd, bool) { return s, nil, false }

// Update implements Screen.
func (s *CacheScreen) Update(msg tea.Msg, env Env) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.resize(env)
		return s, nil

	case cacheResultMsg:
		s.loading = false
		s.refreshing = false
		if msg.err != nil {
			return s, ReportError(msg.err)
		}
		s.entries, s.total, s.free, s.freeErr = msg.data.entries, msg.data.total, msg.data.free, msg.data.freeErr
		s.resize(env)
		return s, nil

	case tea.KeyPressMsg:
		return s.handleKey(msg, env)
	}

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
}

func (s *CacheScreen) handleKey(msg tea.KeyPressMsg, env Env) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, cacheSortBinding):
		s.sortIdx = (s.sortIdx + 1) % len(cacheSortCycle)
		return s, s.load()

	case key.Matches(msg, cacheDeleteBinding):
		return s.confirmDelete()

	case key.Matches(msg, cacheRefreshBinding):
		return s.startRefresh()
	}

	var cmd tea.Cmd
	s.table, cmd = s.table.Update(msg)
	return s, cmd
}

// selected returns the highlighted entry.
func (s *CacheScreen) selected() (cache.Entry, bool) {
	i := s.table.Cursor()
	if i < 0 || i >= len(s.entries) {
		return cache.Entry{}, false
	}
	return s.entries[i], true
}

func (s *CacheScreen) confirmDelete() (Screen, tea.Cmd) {
	entry, ok := s.selected()
	if !ok {
		return s, nil
	}
	id := entry.ID
	action := cacheCmd(func() (cacheLoadedMsg, error) {
		if err := s.cache.Delete(id); err != nil {
			return cacheLoadedMsg{}, err
		}
		return cacheDataFor(s.cache, s.sortBy())
	})
	return s, Confirm(
		"Delete "+entry.Name+"?",
		"Removes it from disk. It will be downloaded again if something needs it.",
		action,
	)
}

// startRefresh re-fetches a cached package, picking up any upstream
// change even within the same day. Only package entries carry enough of
// their own metadata (package.json, written by internal/artifacts) to do
// this without the live catalog, so it is the one kind §6.1 names
// ("R refresh a package").
func (s *CacheScreen) startRefresh() (Screen, tea.Cmd) {
	entry, ok := s.selected()
	if !ok || entry.Kind != cache.KindPackage || s.refreshing {
		return s, nil
	}
	s.refreshing = true

	dir := filepath.Join(s.cache.Root, entry.Path)
	return s, cacheCmd(func() (cacheLoadedMsg, error) {
		meta, err := artifacts.ReadPackageMeta(dir)
		if err != nil {
			return cacheLoadedMsg{}, fmt.Errorf("read %s: %w", entry.Name, err)
		}
		pkg := catalog.Package{
			ID: meta.ID, Name: meta.Name, DownloadURL: meta.SourceURL,
			EffectFiles: meta.EffectFiles, DenyEffectFiles: meta.DenyEffectFiles,
		}
		if _, err := s.cache.EnsurePackage(context.Background(), pkg, nil); err != nil {
			return cacheLoadedMsg{}, fmt.Errorf("refresh %s: %w", entry.Name, err)
		}
		return cacheDataFor(s.cache, s.sortBy())
	})
}

func cacheColumns(width int) []table.Column {
	const (
		kindW = 12
		verW  = 14
		sizeW = 10
		dlW   = 11
		useW  = 11
	)
	nameW := width - kindW - verW - sizeW - dlW - useW - 12
	if nameW < 12 {
		nameW = 12
	}
	return []table.Column{
		{Title: "Name", Width: nameW},
		{Title: "Kind", Width: kindW},
		{Title: "Version", Width: verW},
		{Title: "Size", Width: sizeW},
		{Title: "Downloaded", Width: dlW},
		{Title: "Last used", Width: useW},
	}
}

func (s *CacheScreen) resize(env Env) {
	s.table.SetColumns(cacheColumns(env.Width))
	s.table.SetWidth(env.Width)

	h := env.Height - 4
	if h < 3 {
		h = 3
	}
	s.table.SetHeight(h)

	// s.entries already comes back from cache.Cache.List sorted the way
	// the current mode asks for; there is nothing left to sort here.
	rows := make([]table.Row, 0, len(s.entries))
	for _, e := range s.entries {
		rows = append(rows, table.Row{
			e.Name, string(e.Kind), e.Version,
			humanSize(e.Size), dateOnly(e.DownloadedAt), dateOnly(e.LastUsedAt),
		})
	}
	s.table.SetRows(rows)

	switch {
	case len(rows) == 0:
	case s.table.Cursor() < 0:
		s.table.SetCursor(0)
	case s.table.Cursor() >= len(rows):
		s.table.SetCursor(len(rows) - 1)
	}
}

// View implements Screen.
func (s *CacheScreen) View(env Env) string {
	if s.loading {
		return env.Styles.Faint.Render("loading cache index…")
	}
	if len(s.entries) == 0 {
		return env.Styles.Faint.Render("Cache is empty.")
	}

	header := fmt.Sprintf("sort: %s   total: %s", s.sortBy(), humanSize(s.total))
	if s.freeErr == nil {
		header += fmt.Sprintf("   free: %s", humanSize(s.free))
	}
	if s.refreshing {
		header += "   refreshing…"
	}

	return env.Styles.Faint.Render(header) + "\n\n" + s.table.View()
}

// humanSize formats a byte count for display.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// dateOnly renders a timestamp as a plain date, or "—" for the zero value
// (an entry from before last-used tracking existed, or one never used).
func dateOnly(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02")
}
