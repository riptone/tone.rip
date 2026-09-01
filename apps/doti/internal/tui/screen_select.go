package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/packages/gotui"
)

// appComponent is app.Component under a shorter name, because the copy-before-
// write below reads badly at full length.
type appComponent = app.Component

// The component selector: what an Install, an Adopt or a removal will act on.

// openSelector fills the list for one operation and shows it.
//
// Which list, and how it starts, is the operation's own business: an install
// offers everything on the machine with all of it ticked, a removal offers only
// what it is willing to delete with none of it ticked, and an unlink offers the
// stow packages alone.
func (m Model) openSelector(entry op) Model {
	source := m.components
	if entry.removes {
		source = m.removable
	}
	items := make([]appComponent, 0, len(source))
	for _, item := range source {
		if !entry.offers(item.Kind) {
			continue
		}
		item.Selected = !entry.removes
		items = append(items, item)
	}
	if entry.onlyMissing {
		items = missingOnly(items)
	}

	m.items = items
	m.folded = foldedByDefault(items)
	if entry.onlyMissing {
		// Open, because a list of what is left is short by construction and
		// folding it would hide the one thing the reader came for.
		m.folded = map[string]bool{}
	}
	m.rows = flatten(items, m.folded)
	m.itemAt = 0
	m.notice = ""
	m.screen = ScreenSelect
	return m
}

// offers reports whether this operation's selector lists a kind of component.
func (e op) offers(kind app.Kind) bool {
	return len(e.kinds) == 0 || slices.Contains(e.kinds, kind)
}

func (m Model) selectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.screen = ScreenMenu
		m.notice = ""
		return m, nil
	case key.Matches(msg, m.keys.Up):
		m = m.moveItem(-1)
	case key.Matches(msg, m.keys.Down):
		m = m.moveItem(1)
	case key.Matches(msg, m.keys.Top):
		m = m.jumpItem(false)
	case key.Matches(msg, m.keys.Bottom):
		m = m.jumpItem(true)
	case key.Matches(msg, m.keys.Unfold):
		m = m.foldAt(false)
	case key.Matches(msg, m.keys.Fold):
		m = m.foldAt(true)
	case key.Matches(msg, m.keys.FoldToggle):
		m = m.toggleFoldAt()
	case key.Matches(msg, m.keys.Toggle):
		if len(m.items) > 0 {
			m = m.toggleAt(m.itemAt)
		}
	case key.Matches(msg, m.keys.All):
		m = m.setAll(true)
	case key.Matches(msg, m.keys.None):
		m = m.setAll(false)
	case key.Matches(msg, m.keys.Open):
		chosen := m.Chosen()
		// Nothing ticked means nothing, on every screen that has ticks.
		//
		// It used to mean *everything* for all but a removal, and by accident:
		// an empty selection reaches internal/app as an empty Include, and an
		// empty Include is how the command line says "no narrowing". So untick
		// every box on an Install, press enter, and it installed the lot. Said
		// out loud rather than silently ignored - a keypress that does nothing
		// and explains nothing reads as a hang.
		if len(chosen) == 0 {
			// Two different empties. An empty *list* cannot be ticked, so
			// telling the reader to press `a` would be advice that does
			// nothing - and a keypress that does nothing and explains nothing
			// is what this branch exists to prevent.
			//
			// Both short enough to sit on one line of the narrowest card this
			// program draws: at 64 columns the body is 58 columns wide.
			if len(m.items) == 0 {
				m.notice = "there is nothing here to do: esc to go back"
			} else {
				m.notice = "nothing is ticked: press a for all, or esc to go back"
			}
			return m, nil
		}
		m.notice = ""
		return m.begin(menu[m.menuAt].action, chosen)
	}
	// Any key that could have changed the ticks has now been handled, so the
	// complaint about there being none is stale.
	m.notice = ""
	return m, nil
}

// setAll is `a` and `n`: every box, parents included, so a parent never
// disagrees with what is under it.
func (m Model) setAll(on bool) Model {
	items := append([]appComponent(nil), m.items...)
	for i := range items {
		items[i].Selected = on
	}
	m.items = items
	return m
}

func (m Model) viewSelect() string {
	// hint line + blank + one row per group heading and item.
	g := fit(geometryFor(m.width, m.height), len(m.rows)+2)
	s := m.styles

	entry := menu[m.menuAt]
	lead := s.faint.Render("space toggles · a all · n none · enter confirm")
	switch {
	// The notice outranks the rest, because it is the answer to a key that was
	// just pressed and the others are standing instructions.
	case m.notice != "":
		lead = s.warn.Render(m.notice)
	case entry.removes:
		// Said in the body rather than the footer, because it is the sentence
		// that stops somebody pressing enter out of habit.
		lead = s.faint.Render("nothing is ticked: tick what to uninstall")
	case entry.onlyMissing:
		// Why the list is shorter than the Install one, said once so nobody has
		// to wonder whether something went missing.
		lead = s.faint.Render("only what the machine is missing")
	}

	rows := make([]string, 0, len(m.rows)+3)
	rows = append(rows, lead, s.pad(g.Text))
	if len(m.items) == 0 {
		switch {
		case entry.removes:
			rows = append(rows,
				s.body.Render("Nothing this repository installed is still present."))
		case entry.onlyMissing:
			rows = append(rows,
				s.body.Render("Nothing to adopt: the machine already has all of it."))
		}
	}
	// Where the cursor's row lands once the lead and the blank are in front of
	// it, so the window below can keep it on screen.
	cursorRow := len(rows)

	// Whether the fold column exists at all. Reserved for every top-level row
	// when the list folds, so the labels sit in one column - and not reserved on
	// the removal and unlink lists, where it would be two columns of nothing.
	folds := anyParent(m.items)

	for _, r := range m.rows {
		if r.item == m.itemAt {
			cursorRow = len(rows)
		}
		if r.item < 0 {
			rows = append(rows, s.group.Render(r.heading))
			continue
		}
		item := m.items[r.item]

		label := s.rowOff.Render(item.Label)
		if r.item == m.itemAt {
			label = s.rowOn.Render(item.Label)
		}
		status := s.faint.Render(item.Status)
		if item.Done {
			status = s.done.Render(item.Status)
		}
		rows = append(rows, s.ends(g.Text, m.rowLead(r.item, folds)+label, status))
	}

	// Counted over the leaves: a parent is a summary of its children, and adding
	// it to the total would make "17 of 18" the reading for a list of sixteen
	// tools with everything ticked.
	selected, total := 0, 0
	for i, item := range m.items {
		if isParent(m.items, i) {
			continue
		}
		total++
		if item.Selected {
			selected++
		}
	}

	// space and enter are the two this screen is for, so they outlive the help
	// hint - which was ranked above them and pushed "space toggle" off a
	// 64-column card, on the one screen where nothing else says what space does.
	hints := []hint{
		{Text: "space toggle", Keep: 3},
		{Text: "enter confirm", Keep: 2},
		{Text: "esc back", Keep: 1},
	}
	if folds {
		// Above "h help", on the one screen where folding exists: a reader who
		// cannot see that a group opens will never open it, and the help hint is
		// also on the menu they came from.
		hints = append(hints, hint{Text: "→ open", Keep: 4}, hint{Text: "h help", Keep: 6})
	} else {
		hints = append(hints, hint{Text: "h help", Keep: 4})
	}
	// The count is the confirmation on a removal, so it says so and turns red
	// the moment it is not zero.
	status := fmt.Sprintf("%d of %d", selected, total)
	var colour lipgloss.TerminalColor
	if entry.removes {
		status = fmt.Sprintf("%d to remove", selected)
		if selected > 0 {
			colour = gotui.Close
		}
	}

	// Windowed, and the scrollbar drawn from the same offset - so a list longer
	// than the card scrolls with the cursor instead of losing its tail.
	visible, offset := window(rows, cursorRow, g.Body)

	return s.chrome.Render(g, pane{
		Name:         m.name(),
		Rows:         s.chrome.BodyRows(g, strings.Join(visible, "\n"), offset, len(rows)),
		Hints:        hints,
		Status:       status,
		StatusColour: colour,
	})
}
