package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Action is one operation the window can run.
type Action string

const (
	ActionNone    Action = ""
	ActionInstall Action = "install"
	ActionUnlink  Action = "unlink"
	ActionAdopt   Action = "adopt"
	ActionPreview Action = "preview"
	ActionCheck   Action = "check"
	ActionSync    Action = "sync"
	ActionUpdate  Action = "update"
	// ActionSelfUpdate replaces the binary. Not in the menu - it is offered in
	// the footer, and only when the check found something.
	ActionSelfUpdate Action = "self-update"
)

// op is one entry in the menu.
type op struct {
	action Action
	label  string
	desc   string
	// selects means the operation opens the component selector first. The rest
	// act on everything or on nothing, so a selector would be a keypress that
	// never changes the outcome.
	selects bool
}

// The same operations the shell installer's menu offered, in the same order.
// Changing what the tool does should not silently change what people's hands
// already know.
var menu = []op{
	{action: ActionInstall, label: "Install", desc: "packages and configs", selects: true},
	{action: ActionUnlink, label: "Uninstall", desc: "remove symlinks (packages stay)"},
	{action: ActionAdopt, label: "Adopt", desc: "install only what is missing", selects: true},
	{action: ActionPreview, label: "Preview", desc: "show what would change"},
	{action: ActionCheck, label: "Health check", desc: "verify symlinks and tools"},
	{action: ActionSync, label: "Sync", desc: "git pull, then re-link"},
	{action: ActionUpdate, label: "Update", desc: "upgrade installed packages"},
}

// labelFor is an action's name, for the run screen's title.
func labelFor(action Action) string {
	for _, entry := range menu {
		if entry.action == action {
			return entry.label
		}
	}
	if action == ActionSelfUpdate {
		return "Self-update"
	}
	return string(action)
}

func (m Model) menuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.menuAt = max(m.menuAt-1, 0)
	case key.Matches(msg, m.keys.Down):
		m.menuAt = min(m.menuAt+1, len(menu)-1)
	case key.Matches(msg, m.keys.Top):
		m.menuAt = 0
	case key.Matches(msg, m.keys.Bottom):
		m.menuAt = len(menu) - 1
	case key.Matches(msg, m.keys.Update) && m.update != "":
		return m.begin(ActionSelfUpdate, nil)
	case key.Matches(msg, m.keys.Open):
		chosen := menu[m.menuAt]
		if chosen.selects {
			m.screen = ScreenSelect
			m.itemAt = 0
			return m, nil
		}
		return m.begin(chosen.action, nil)
	}
	if n := digit(msg.String()); n >= 1 && n <= len(menu) {
		m.menuAt = n - 1
	}
	return m, nil
}

func digit(key string) int {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return 0
	}
	return int(key[0] - '0')
}

func (m Model) viewMenu() string {
	// heading + blank + one row per entry.
	g := fit(geometryFor(m.width, m.height), len(menu)+2)
	s := m.styles

	rows := make([]string, 0, len(menu)+2)
	rows = append(rows, s.group.Render("What would you like to do?"), s.pad(g.Inner))
	for i, entry := range menu {
		marker, label := s.pad(2), s.rowOff.Render(entry.label)
		if i == m.menuAt {
			marker, label = s.cursor.Render("› "), s.rowOn.Render(entry.label)
		}
		left := marker + s.rowKey.Render(fmt.Sprintf("%d  ", i+1)) + label
		rows = append(rows, s.ends(g.Inner, left, s.faint.Render(entry.desc)))
	}

	return s.chrome.Render(g, pane{
		Name:   m.name(),
		Rows:   rows,
		Hints:  m.menuHints(),
		Status: "menu",
	})
}

// menuHints are the key legends, each with the rank it fights to stay at.
//
// "q quit" outlives everything that is not a way out, and the update offer
// outranks the operations: a reader who cannot see it will never find it, and
// it is only ever there when there is genuinely a newer release.
func (m Model) menuHints() []hint {
	hints := []hint{
		{Text: "↑/↓ move", Keep: 3},
		{Text: "enter open", Keep: 2},
		{Text: "q quit", Keep: 1},
	}
	if m.update != "" {
		hints = append(hints, hint{Text: "u update to " + m.update, Keep: 0})
	}
	return hints
}
