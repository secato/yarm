package app

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// Small rendering helpers shared by more than one screen. They deal only
// in strings and an Env, so a screen's own View stays about what it is
// showing rather than about fitting it on a terminal.

// countLines counts the rendered rows in a block that ends every row with
// a newline.
func countLines(s string) int { return strings.Count(s, "\n") }

// clipTail shortens s to width, marking the cut at the end. The sibling
// truncate() keeps a string's tail instead, because the distinctive part
// of a filesystem path is its last few segments; for a sentence it is the
// first few words.
func clipTail(s string, width int) string {
	if width <= 1 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	return string(runes[:width-1]) + "…"
}

// clipLines keeps the first height lines of a block, replacing the last
// one with a count of what was dropped — a panel that ran out of room must
// not look like it showed everything.
func clipLines(s string, height int) string {
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if height < 1 || len(lines) <= height {
		return s
	}
	kept := lines[:height-1]
	return strings.Join(kept, "\n") + fmt.Sprintf("\n… %d more line(s)", len(lines)-len(kept))
}

// writeWindow renders the slice of count rows that fits in height around
// cursor, indenting its own markers to match the caller's rows and saying
// how many rows it hid at either end — a truncated list must never look
// like a complete one.
func writeWindow(b *strings.Builder, env Env, count, cursor, height int, indent string, row func(i int)) {
	// The "more above/below" markers are rows of their own; leaving room
	// for them keeps a windowed list inside the height it was given.
	if count > height {
		height -= 2
	}
	if height < 1 {
		height = 1
	}
	start, end := scrollWindow(count, cursor, height)
	if start > 0 {
		b.WriteString(env.Styles.Faint.Render(fmt.Sprintf("%s↑ %d more above", indent, start)))
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		row(i)
	}
	if end < count {
		b.WriteString(env.Styles.Faint.Render(fmt.Sprintf("%s↓ %d more below", indent, count-end)))
		b.WriteString("\n")
	}
}

// rule draws a full-width horizontal separator: the line between one block
// and the next, so stacked sections read as sections rather than one run.
// Box-drawing is safe here — a terminal that cannot draw it cannot draw
// the panels either.
func rule(env Env) string {
	return env.Styles.Faint.Render(strings.Repeat("─", env.Width))
}

// splitRow fits a row's text and the note that trails it into width,
// clipping each so that together they fit — clipping them independently
// against the full width is how a row ends up wider than the terminal. The
// note gives way first, down to nothing, because the row's own name is
// what the reader is looking for; the text keeps at least half the width
// so a long note cannot squeeze it away either.
func splitRow(text, note string, width int) (string, string) {
	if note == "" {
		return clipTail(text, width), ""
	}
	room := width - lipgloss.Width(note)
	if half := width / 2; room < half {
		room = half
	}
	text = clipTail(text, room)
	room = width - lipgloss.Width(text)
	if room <= 2 {
		// Not enough left for even a marked-off fragment of the note.
		return text, ""
	}
	return text, clipTail(note, room)
}
