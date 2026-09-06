package app

import "testing"

func TestCursorListClampsAtBothEnds(t *testing.T) {
	c := newCursorList(3, 1)
	if c.Cursor() != 1 {
		t.Fatalf("Cursor() = %d, want 1", c.Cursor())
	}

	c.up()
	c.up()
	c.up() // one past the top
	if c.Cursor() != 0 {
		t.Errorf("Cursor() = %d, want 0 (clamped, not wrapped)", c.Cursor())
	}

	c.down()
	c.down()
	c.down() // one past the bottom
	if c.Cursor() != 2 {
		t.Errorf("Cursor() = %d, want 2 (clamped, not wrapped)", c.Cursor())
	}
}

func TestCursorListEmpty(t *testing.T) {
	c := newCursorList(0, 0)
	if got := c.Cursor(); got != -1 {
		t.Errorf("Cursor() on an empty list = %d, want -1", got)
	}
	c.down() // must not panic
	if got := c.Cursor(); got != -1 {
		t.Errorf("Cursor() after down() on an empty list = %d, want -1", got)
	}
}

// Shrinking the list (a filter narrowing results) must not strand the
// cursor past the new end — this is the exact bug that hit bubbles/table
// in step 5.
func TestCursorListSetCountClamps(t *testing.T) {
	c := newCursorList(5, 4)
	c.setCount(2)
	if got := c.Cursor(); got != 1 {
		t.Errorf("Cursor() = %d after shrinking to 2 items, want 1", got)
	}
}

func TestMultiSelectPreselectsRequired(t *testing.T) {
	items := []selectItem{
		{ID: "standard", Name: "Standard", Required: true},
		{ID: "sweetfx", Name: "SweetFX"},
	}
	m := newMultiSelect(items)

	if !m.isSelected(0) {
		t.Error("a Required item must start selected")
	}
	if m.isSelected(1) {
		t.Error("a non-required item must not start selected")
	}
}

func TestMultiSelectRequiredCannotBeToggledOff(t *testing.T) {
	m := newMultiSelect([]selectItem{{ID: "standard", Required: true}})
	m.toggle()
	if !m.isSelected(0) {
		t.Error("toggling a Required item must be a no-op")
	}
}

func TestMultiSelectToggle(t *testing.T) {
	m := newMultiSelect([]selectItem{{ID: "a"}, {ID: "b"}})
	m.toggle() // cursor starts at 0
	if !m.isSelected(0) {
		t.Fatal("toggle should select item 0")
	}
	m.toggle()
	if m.isSelected(0) {
		t.Fatal("a second toggle should deselect it")
	}
}

func TestMultiSelectDisabledCannotBeToggled(t *testing.T) {
	m := newMultiSelect([]selectItem{{ID: "manual", Disabled: true, DisabledNote: "see repo"}})
	m.toggle()
	if m.isSelected(0) {
		t.Error("a disabled item must never become selected")
	}
}

// The cursor must skip header separator rows entirely: it should never be
// possible to land on one, since space there would silently do nothing.
func TestMultiSelectCursorSkipsHeaders(t *testing.T) {
	items := []selectItem{
		{ID: "a", Name: "A"},
		{Header: true, Name: "── Custom ──"},
		{ID: "b", Name: "B"},
	}
	m := newMultiSelect(items)
	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}

	m.down()
	if m.cursor != 2 {
		t.Errorf("down() from 0 landed on %d, want 2 (skipping the header)", m.cursor)
	}

	m.up()
	if m.cursor != 0 {
		t.Errorf("up() from 2 landed on %d, want 0 (skipping the header)", m.cursor)
	}
}

// The cursor may still rest on a disabled row (so its explanation is
// readable), but toggling it must do nothing.
func TestMultiSelectCursorCanRestOnDisabledRow(t *testing.T) {
	items := []selectItem{
		{ID: "a", Name: "A"},
		{ID: "manual", Name: "Manual only", Disabled: true},
	}
	m := newMultiSelect(items)
	m.down()
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	m.toggle()
	if m.isSelected(1) {
		t.Error("a disabled row must not be selectable even while under the cursor")
	}
}

func TestMultiSelectSelectedIDsExcludesHeaders(t *testing.T) {
	items := []selectItem{
		{ID: "standard", Required: true},
		{Header: true, Name: "── Custom ──"},
		{ID: "mine", Name: "Mine"},
	}
	m := newMultiSelect(items)
	m.down() // to the header... skipped, lands on "mine"
	m.toggle()

	got := m.selectedIDs()
	want := []string{"standard", "mine"}
	if len(got) != len(want) {
		t.Fatalf("selectedIDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("selectedIDs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMultiSelectUpDownClampAtEnds(t *testing.T) {
	m := newMultiSelect([]selectItem{{ID: "a"}, {ID: "b"}})
	m.up() // already at 0
	if m.cursor != 0 {
		t.Errorf("cursor = %d after up() at the top, want 0", m.cursor)
	}
	m.down()
	m.down() // already at the bottom
	if m.cursor != 1 {
		t.Errorf("cursor = %d after down() past the bottom, want 1", m.cursor)
	}
}

// An all-header or empty list must not panic when navigated.
func TestMultiSelectAllHeaders(t *testing.T) {
	m := newMultiSelect([]selectItem{{Header: true, Name: "only a header"}})
	m.up()
	m.down()
	m.toggle()
	if len(m.selectedIDs()) != 0 {
		t.Error("nothing should be selectable in an all-header list")
	}
}

// setItems is how the wizard swaps between its curated shortlist and the
// full catalog: the rows change, the selections do not, and the cursor
// stays on the row it was on when that row is still there.
func TestSetItemsKeepsSelectionsAndFollowsTheCursor(t *testing.T) {
	full := []selectItem{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
		{ID: "c", Name: "C"},
	}
	m := newMultiSelect(full)
	m.down() // b
	m.toggle()
	m.down() // c
	m.toggle()

	m.setItems([]selectItem{full[0], full[2]}) // b hidden, cursor was on c
	if m.items[m.cursor].ID != "c" {
		t.Errorf("cursor on %q, want c — the row it was on is still shown", m.items[m.cursor].ID)
	}
	if !m.selected["b"] {
		t.Error("hiding a row must not unselect it")
	}

	m.setItems([]selectItem{full[0]}) // the cursor's own row is gone now
	if m.items[m.cursor].ID != "a" {
		t.Errorf("cursor on %q, want a — the first row it can rest on", m.items[m.cursor].ID)
	}
	if got := m.selectedIDs(); len(got) != 0 {
		t.Errorf("selectedIDs() = %v; only visible rows are reported", got)
	}
}
