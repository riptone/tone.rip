package tui

import (
	"fmt"
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
// offers everything on the machine with all of it ticked, and a removal offers
// only what it is willing to delete with none of it ticked.
func (m Model) openSelector(entry op) Model {
	source := m.components
	if entry.removes {
		source = m.removable
	}
	items := make([]appComponent, len(source))
	copy(items, source)
	for i := range items {
		items[i].Selected = !entry.removes
	}

	m.items = items
	m.rows = flatten(items)
	m.itemAt = 0
	m.screen = ScreenSelect
	return m
}

func (m Model) selectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.screen = ScreenMenu
		return m, nil
	case key.Matches(msg, m.keys.Up):
		m.itemAt = step(m.itemAt, -1, len(m.items))
	case key.Matches(msg, m.keys.Down):
		m.itemAt = step(m.itemAt, 1, len(m.items))
	case key.Matches(msg, m.keys.Top):
		m.itemAt = 0
	case key.Matches(msg, m.keys.Bottom):
		m.itemAt = max(len(m.items)-1, 0)
	case key.Matches(msg, m.keys.Toggle):
		if len(m.items) > 0 {
			// Copied before the write: the slice is shared with every earlier
			// copy of this model, and Bubble Tea keeps those.
			items := append([]appComponent(nil), m.items...)
			items[m.itemAt].Selected = !items[m.itemAt].Selected
			m.items = items
		}
	case key.Matches(msg, m.keys.All):
		m = m.setAll(true)
	case key.Matches(msg, m.keys.None):
		m = m.setAll(false)
	case key.Matches(msg, m.keys.Open):
		chosen := m.Chosen()
		// A removal with nothing ticked is the no-op the empty list is for, and
		// running it would report that at length. Say it here instead.
		if menu[m.menuAt].removes && len(chosen) == 0 {
			return m, nil
		}
		return m.begin(menu[m.menuAt].action, chosen)
	}
	return m, nil
}

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
	lead := "space toggles · a all · n none · enter confirm"
	if entry.removes {
		// Said in the body rather than the footer, because it is the sentence
		// that stops somebody pressing enter out of habit.
		lead = "nothing is ticked: tick what to uninstall"
	}

	rows := make([]string, 0, len(m.rows)+3)
	rows = append(rows, s.faint.Render(lead), s.pad(g.Text))
	if entry.removes && len(m.items) == 0 {
		rows = append(rows,
			s.body.Render("Nothing this repository installed is still present."))
	}
	// Where the cursor's row lands once the lead and the blank are in front of
	// it, so the window below can keep it on screen.
	cursorRow := len(rows)

	for _, r := range m.rows {
		if r.item == m.itemAt {
			cursorRow = len(rows)
		}
		if r.item < 0 {
			rows = append(rows, s.group.Render(r.heading))
			continue
		}
		item := m.items[r.item]

		box := s.rowKey.Render("[ ]")
		if item.Selected {
			box = s.rowKey.Render("[") + s.check.Render("x") + s.rowKey.Render("]")
		}

		marker, label := s.pad(2), s.rowOff.Render(item.Label)
		if r.item == m.itemAt {
			marker, label = s.cursor.Render("› "), s.rowOn.Render(item.Label)
		}

		status := s.faint.Render(item.Status)
		if item.Done {
			status = s.done.Render(item.Status)
		}
		rows = append(rows, s.ends(g.Text, marker+box+s.pad(1)+label, status))
	}

	selected := 0
	for _, item := range m.items {
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
		{Text: "h help", Keep: 4},
		{Text: "esc back", Keep: 1},
	}
	// The count is the confirmation on a removal, so it says so and turns red
	// the moment it is not zero.
	status := fmt.Sprintf("%d of %d", selected, len(m.items))
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
