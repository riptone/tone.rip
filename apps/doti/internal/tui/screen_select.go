package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
)

// appComponent is app.Component under a shorter name, because the copy-before-
// write below reads badly at full length.
type appComponent = app.Component

// The component selector: what an Install or an Adopt will act on.

func (m Model) selectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.screen = ScreenMenu
		return m, nil
	case key.Matches(msg, m.keys.Up):
		m.itemAt = max(m.itemAt-1, 0)
	case key.Matches(msg, m.keys.Down):
		m.itemAt = min(m.itemAt+1, len(m.items)-1)
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
		return m.begin(menu[m.menuAt].action, m.Chosen())
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

	rows := make([]string, 0, len(m.rows)+2)
	rows = append(rows,
		s.faint.Render("space toggles · a all · n none · enter confirm"),
		s.pad(g.Inner))

	for _, r := range m.rows {
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
		rows = append(rows, s.ends(g.Inner, marker+box+s.pad(1)+label, status))
	}

	selected := 0
	for _, item := range m.items {
		if item.Selected {
			selected++
		}
	}

	return s.chrome.Render(g, pane{
		Name: m.name(),
		Rows: rows,
		Hints: []hint{
			{Text: "space toggle", Keep: 3},
			{Text: "enter confirm", Keep: 2},
			{Text: "esc back", Keep: 1},
		},
		Status: fmt.Sprintf("%d of %d", selected, len(m.items)),
	})
}
