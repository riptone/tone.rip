package tui

import (
	"slices"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
)

// Folding a list: parents, their children, and a cursor that only ever lands on
// something visible.
//
// The install selector offers every tool, cask, zsh plugin and MCP server the
// manifest declares - thirty-odd rows where there used to be four. Folded by
// default, the group reads exactly as it did when each list was one line, and
// opening one is a keypress. That is the whole point: "pick which packages" and
// "do not make me scroll past twenty-three of them" are both true.

// foldMarks are what a parent row shows: open, and closed.
const (
	foldOpen   = "▾"
	foldClosed = "▸"
)

// foldedByDefault closes every parent.
//
// Closed rather than open, because open is the shape nobody asked for: thirty
// rows where the reader came to tick four groups. It also means the screen looks
// the same as it did before any of this existed, until somebody presses an arrow.
func foldedByDefault(items []app.Component) map[string]bool {
	folded := map[string]bool{}
	for i := range items {
		if isParent(items, i) {
			folded[items[i].Label] = true
		}
	}
	return folded
}

// parentOf is the index of the component label belongs to as a parent, or -1.
func parentOf(items []app.Component, label string) int {
	for i, item := range items {
		if item.Parent == "" && item.Label == label {
			return i
		}
	}
	return -1
}

// childrenOf is the indices of the components folded under one label.
func childrenOf(items []app.Component, label string) []int {
	var out []int
	for i, item := range items {
		if item.Parent == label {
			out = append(out, i)
		}
	}
	return out
}

// isParent reports whether anything folds under this component.
func isParent(items []app.Component, i int) bool {
	if items[i].Parent != "" {
		return false
	}
	return len(childrenOf(items, items[i].Label)) > 0
}

// anyParent reports whether this list folds at all, which is what decides
// whether the fold column is drawn.
//
// Reserved for every top-level row when it is, so the labels line up in one
// column - and not reserved at all on the removal and unlink lists, where two
// blank columns would be two columns of nothing.
func anyParent(items []app.Component) bool {
	for i := range items {
		if isParent(items, i) {
			return true
		}
	}
	return false
}

// tally counts how many of a parent's children are ticked.
func tally(items []app.Component, label string) (selected, total int) {
	for _, i := range childrenOf(items, label) {
		total++
		if items[i].Selected {
			selected++
		}
	}
	return selected, total
}

// fold closes or opens one parent.
//
// The map is copied before the write for the same reason m.items is: Bubble Tea
// keeps every earlier copy of the model, and a map header copied into one of
// them shares the same buckets - so mutating in place would rewrite the past.
func (m Model) fold(label string, closed bool) Model {
	next := make(map[string]bool, len(m.folded)+1)
	for k, v := range m.folded {
		next[k] = v
	}
	next[label] = closed
	m.folded = next
	m.rows = flatten(m.items, m.folded)
	return m
}

// foldAt closes or opens the parent at or above the cursor.
//
// "Or above": pressing left on a child closes the thing it is inside, which is
// what the arrow means in every tree anybody has used. The cursor follows it up
// to the parent, because leaving it on a row that is no longer drawn is how `G`
// used to put it on an invisible item.
func (m Model) foldAt(closed bool) Model {
	if len(m.items) == 0 {
		return m
	}
	at := m.itemAt
	if parent := m.items[at].Parent; parent != "" {
		if closed {
			if i := parentOf(m.items, parent); i >= 0 {
				m = m.fold(parent, true)
				m.itemAt = i
				return m
			}
		}
		// Opening from a child: it is already open, so there is nothing to do
		// and nowhere better to be.
		return m
	}
	if !isParent(m.items, at) {
		return m
	}
	return m.fold(m.items[at].Label, closed)
}

// toggleFoldAt is the one key that always does something, for the reader who
// has not worked out which arrow.
func (m Model) toggleFoldAt() Model {
	if len(m.items) == 0 {
		return m
	}
	label := m.items[m.itemAt].Label
	if parent := m.items[m.itemAt].Parent; parent != "" {
		label = parent
	}
	return m.foldAt(!m.folded[label])
}

// visibleItems is the item indices currently drawn, in order.
func (m Model) visibleItems() []int {
	out := make([]int, 0, len(m.items))
	for _, r := range m.rows {
		if r.item >= 0 {
			out = append(out, r.item)
		}
	}
	return out
}

// moveItem steps the cursor over the visible items, wrapping at both ends.
//
// Over the *visible* ones: stepping by one through m.items would walk into a
// folded child and stop the cursor somewhere the reader cannot see it.
func (m Model) moveItem(delta int) Model {
	visible := m.visibleItems()
	if len(visible) == 0 {
		return m
	}
	at := slices.Index(visible, m.itemAt)
	if at < 0 {
		// The cursor's row was folded away by something other than a fold key.
		at = 0
	}
	m.itemAt = visible[step(at, delta, len(visible))]
	return m
}

// jumpItem puts the cursor on the first or last visible item.
func (m Model) jumpItem(last bool) Model {
	visible := m.visibleItems()
	if len(visible) == 0 {
		return m
	}
	if last {
		m.itemAt = visible[len(visible)-1]
		return m
	}
	m.itemAt = visible[0]
	return m
}

// toggleAt ticks one component, and keeps a parent and its children agreeing.
//
// Two rules, and they are the ones a tri-state list needs:
//
//   - A parent carries its children. Partly ticked or not at all, it fills;
//     fully ticked, it clears. So space on a half-open group is "yes, all of
//     it", which is what the hand means.
//   - A child updates its parent, which is the OR of them. A parent left
//     unticked with a ticked child underneath would report a skipped phase and
//     then install something.
func (m Model) toggleAt(at int) Model {
	items := append([]app.Component(nil), m.items...)
	if isParent(items, at) {
		label := items[at].Label
		selected, total := tally(items, label)
		on := selected < total
		items[at].Selected = on
		for _, i := range childrenOf(items, label) {
			items[i].Selected = on
		}
		m.items = items
		return m
	}

	items[at].Selected = !items[at].Selected
	if parent := items[at].Parent; parent != "" {
		if i := parentOf(items, parent); i >= 0 {
			selected, _ := tally(items, parent)
			items[i].Selected = selected > 0
		}
	}
	m.items = items
	return m
}

// box is the checkbox for one row: ticked, empty, or partly - which only a
// parent can be.
func (s styles) box(items []app.Component, at int) string {
	brackets := func(inner string) string {
		return s.rowKey.Render("[") + inner + s.rowKey.Render("]")
	}
	if isParent(items, at) {
		selected, total := tally(items, items[at].Label)
		switch {
		case selected == 0:
			return s.rowKey.Render("[ ]")
		case selected < total:
			// Partly, and said with a glyph rather than by leaving it ticked:
			// "some of these" is the state a reader most needs to see, because
			// it is the one they did not ask for directly.
			return brackets(s.check.Render("~"))
		default:
			return brackets(s.check.Render("x"))
		}
	}
	if items[at].Selected {
		return brackets(s.check.Render("x"))
	}
	return s.rowKey.Render("[ ]")
}

// rowLead is everything on a row before its label: the cursor marker, the
// checkbox, and - for a parent - the fold state.
//
// Three shapes, one column: a parent's box sits leftmost with its fold mark
// between the box and the label, a child's box is pushed right by two so it
// reads as being inside, and a top-level row with nothing under it reserves the
// fold mark's width. All three end at the same column, so every label in the
// list starts in the same place - without the reservation, a list that mixes
// them reads as though the plain rows were mis-indented.
func (m Model) rowLead(at int, folds bool) string {
	s := m.styles
	marker := s.pad(2)
	if at == m.itemAt {
		marker = s.cursor.Render("› ")
	}
	if m.items[at].Parent != "" {
		return marker + s.pad(2) + s.box(m.items, at) + s.pad(1)
	}
	lead := marker + s.box(m.items, at) + s.pad(1)
	switch {
	case !folds:
		return lead
	case !isParent(m.items, at):
		return lead + s.pad(2)
	case m.folded[m.items[at].Label]:
		return lead + s.rowKey.Render(foldClosed) + s.pad(1)
	default:
		return lead + s.rowKey.Render(foldOpen) + s.pad(1)
	}
}

// missingOnly drops what the machine already has, and any group left with
// nothing under it.
//
// A parent's own Done is ignored on purpose: it is a summary of its children,
// and the children are the truth. So a group keeps its row exactly when
// something under it is still missing - and a childless top-level row is judged
// on its own Done, because there is nothing else to judge it on.
func missingOnly(items []app.Component) []app.Component {
	alive := map[string]bool{}
	for _, item := range items {
		if item.Parent != "" && !item.Done {
			alive[item.Parent] = true
		}
	}
	out := make([]app.Component, 0, len(items))
	for i, item := range items {
		switch {
		case item.Parent != "":
			if !item.Done {
				out = append(out, item)
			}
		case isParent(items, i):
			if alive[item.Label] {
				out = append(out, item)
			}
		case !item.Done:
			out = append(out, item)
		}
	}
	return out
}
