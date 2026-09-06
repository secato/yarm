package app

// This file holds small, pure cursor-state helpers shared by the wizard's
// single- and multi-select steps. They hold no Bubble Tea state of their
// own and do no I/O, so the wizard's step logic is testable as plain Go
// without driving a program.

// cursorList tracks a moveable cursor over a fixed-size list, clamping at
// both ends rather than wrapping — wrapping would let a fast scroller
// overshoot past the item they meant to land on without noticing.
type cursorList struct {
	cursor int
	count  int
}

// newCursorList returns a cursor over count items, positioned at start
// (clamped into range).
func newCursorList(count, start int) cursorList {
	c := cursorList{count: count}
	c.setCursor(start)
	return c
}

func (c *cursorList) setCount(count int) {
	c.count = count
	c.setCursor(c.cursor)
}

func (c *cursorList) setCursor(n int) {
	switch {
	case c.count <= 0:
		c.cursor = 0
	case n < 0:
		c.cursor = 0
	case n >= c.count:
		c.cursor = c.count - 1
	default:
		c.cursor = n
	}
}

func (c *cursorList) up()   { c.setCursor(c.cursor - 1) }
func (c *cursorList) down() { c.setCursor(c.cursor + 1) }

// Cursor returns the current position, or -1 when there is nothing to
// point at.
func (c cursorList) Cursor() int {
	if c.count <= 0 {
		return -1
	}
	return c.cursor
}

// selectItem is one row in a wizard multi-select list: an effect package,
// an add-on, or a piece of custom content.
type selectItem struct {
	ID          string
	Name        string
	Description string
	// Required marks an item that cannot be unchecked (Standard effects,
	// which other packages assume is present).
	Required bool
	// Disabled marks an item with nothing installable behind it (a
	// manual-only add-on); DisabledNote explains why, typically a
	// repository URL.
	Disabled     bool
	DisabledNote string
	// Cached marks an item already present in the local cache, so
	// selecting it will not trigger a download.
	Cached bool
	// Note is a short suffix shown after the name — what this row still
	// needs, or what selected it. NoteWarn styles it as a warning rather
	// than an aside.
	Note     string
	NoteWarn bool
	// Header marks a non-selectable separator row, e.g. "── Custom ──",
	// used to group custom content under its own heading.
	Header bool
}

// multiSelect is the cursor and selection state behind a checkbox list.
// Required items are selected on construction and cannot be toggled off;
// header and disabled rows are skipped by cursor movement entirely so the
// cursor can never land somewhere pressing space would do nothing.
type multiSelect struct {
	items    []selectItem
	selected map[string]bool
	cursor   int
}

// newMultiSelect returns a multiSelect with every Required item
// preselected and the cursor on the first movable row.
func newMultiSelect(items []selectItem) multiSelect {
	m := multiSelect{items: items, selected: map[string]bool{}}
	for _, it := range items {
		if it.Required {
			m.selected[it.ID] = true
		}
	}
	for i, it := range items {
		if !it.Header {
			m.cursor = i
			break
		}
	}
	return m
}

// setItems replaces the visible rows, keeping every selection — including
// selections whose row is no longer shown, since the wizard hides rows
// (the curated shortlist) rather than unchecking them. The cursor follows
// the row it was on when that row survives, and otherwise falls back to
// the first row it can rest on.
func (m *multiSelect) setItems(items []selectItem) {
	var on string
	if m.cursor >= 0 && m.cursor < len(m.items) {
		on = m.items[m.cursor].ID
	}
	m.items = items
	m.cursor = 0
	for i, it := range items {
		if !it.Header {
			m.cursor = i
			break
		}
	}
	if on == "" {
		return
	}
	for i, it := range items {
		if it.ID == on && !it.Header {
			m.cursor = i
			return
		}
	}
}

// canToggle reports whether the row at i responds to space: not a header,
// not disabled, and not required (required rows are permanently on).
func (m multiSelect) canToggle(i int) bool {
	if i < 0 || i >= len(m.items) {
		return false
	}
	it := m.items[i]
	return !it.Header && !it.Disabled && !it.Required
}

// movable reports whether the cursor may rest on row i: any real row,
// selectable or not (so a disabled add-on is still visible/readable),
// except header separators.
func (m multiSelect) movable(i int) bool {
	return i >= 0 && i < len(m.items) && !m.items[i].Header
}

func (m *multiSelect) up() {
	for i := m.cursor - 1; i >= 0; i-- {
		if m.movable(i) {
			m.cursor = i
			return
		}
	}
}

func (m *multiSelect) down() {
	for i := m.cursor + 1; i < len(m.items); i++ {
		if m.movable(i) {
			m.cursor = i
			return
		}
	}
}

// toggle flips the selection of the row under the cursor, if it can be
// toggled at all.
func (m *multiSelect) toggle() {
	if !m.canToggle(m.cursor) {
		return
	}
	id := m.items[m.cursor].ID
	m.selected[id] = !m.selected[id]
}

// isSelected reports the checkbox state of the row at i.
func (m multiSelect) isSelected(i int) bool {
	if i < 0 || i >= len(m.items) {
		return false
	}
	return m.selected[m.items[i].ID]
}

// selectedIDs returns every checked item's ID, in list order, excluding
// header rows.
func (m multiSelect) selectedIDs() []string {
	var out []string
	for _, it := range m.items {
		if !it.Header && m.selected[it.ID] {
			out = append(out, it.ID)
		}
	}
	return out
}
