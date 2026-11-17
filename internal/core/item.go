package core

// Item is one row in a list of installable things: an effect package, an
// add-on, or a piece of custom content.
type Item struct {
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
	// needs, or what selected it. NoteWarn marks it as a warning rather
	// than an aside.
	Note     string
	NoteWarn bool
	// Header marks a non-selectable separator row, used to group custom
	// content under its own heading.
	Header bool
}
