package app

import (
	"fmt"
	"strings"
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

// fitBlocks returns the [start, end) run of variable-height blocks that
// fits in height, grown outward from focus so the focused block is always
// shown whole — the rows-of-different-sizes counterpart to writeWindow's
// fixed-height rows. Blocks are assumed to be separated by one blank line.
func fitBlocks(blocks []string, focus, height int) (start, end int) {
	if len(blocks) == 0 {
		return 0, 0
	}
	switch {
	case focus < 0:
		focus = 0
	case focus >= len(blocks):
		focus = len(blocks) - 1
	}

	used := countLines(blocks[focus])
	start, end = focus, focus+1
	for {
		grew := false
		if end < len(blocks) {
			if n := countLines(blocks[end]) + 1; used+n <= height {
				used, end, grew = used+n, end+1, true
			}
		}
		if start > 0 {
			if n := countLines(blocks[start-1]) + 1; used+n <= height {
				used, start, grew = used+n, start-1, true
			}
		}
		if !grew {
			return start, end
		}
	}
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
