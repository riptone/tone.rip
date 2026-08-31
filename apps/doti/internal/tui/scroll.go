package tui

// Moving a cursor over a list, and keeping it on screen.
//
// The menu and the selector built every row and handed them all to the frame,
// which draws as many as the body has and drops the rest. On a short terminal,
// or with a long component list, that meant `G` moved the cursor onto an item
// nobody could see - and space then toggled it. Thirty components in a 24-row
// terminal showed twelve.

// window returns the rows that fit, scrolled so the one at cursor is inside it,
// and the offset it starts at.
//
// The offset is what the scrollbar is drawn from, so the two cannot disagree
// about where in the list you are. Scrolled by the smallest amount that works:
// a list that fits does not move at all, and stepping off the bottom edge
// advances by one row rather than jumping half a page.
func window(rows []string, cursor, height int) ([]string, int) {
	if height <= 0 || len(rows) <= height {
		return rows, 0
	}
	// Clamped rather than trusted: a cursor past the end would slice out of
	// range, and the caller computes it from a different list.
	cursor = min(max(cursor, 0), len(rows)-1)

	offset := 0
	if cursor >= height {
		offset = cursor - height + 1
	}
	if last := len(rows) - height; offset > last {
		offset = last
	}
	return rows[offset : offset+height], offset
}

// step moves a cursor by delta, wrapping at both ends.
//
// Up from the first row is the last one, and down from the last is the first -
// the same arithmetic apps/ssh-cv uses, because a list you can only leave by
// pressing the other arrow eight times is a list that ignores you. Home and
// end clamp instead: those mean "the first" and "the last", and wrapping them
// would mean the opposite.
//
// An empty list does not move, which is also what stops the modulo dividing by
// zero - the removal selector is empty on a machine with nothing to remove.
func step(cursor, delta, length int) int {
	if length <= 0 {
		return 0
	}
	return (cursor + delta%length + length) % length
}
