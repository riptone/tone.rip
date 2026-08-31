package tui

import (
	"fmt"
	"strings"

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
	// ActionRemove uninstalls tools. The only operation here that deletes
	// software, which is why its selector starts with nothing ticked.
	ActionRemove Action = "uninstall"
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
	// removes means the operation deletes software.
	//
	// Its selector starts with nothing ticked, so the safe action - press enter
	// without thinking - is the one that does nothing. Every other selector
	// starts with everything ticked, because re-running a step is how drift
	// gets repaired and the common case is "yes, all of it".
	removes bool
}

// The same operations the shell installer's menu offered, in the same order.
// Changing what the tool does should not silently change what people's hands
// already know.
var menu = []op{
	{action: ActionInstall, label: "Install", desc: "packages and configs", selects: true},
	// "Unlink" rather than "Uninstall", which is now a different operation:
	// this one leaves the software and takes away the symlinks.
	{action: ActionUnlink, label: "Unlink", desc: "remove symlinks (packages stay)"},
	{action: ActionAdopt, label: "Adopt", desc: "install only what is missing", selects: true},
	{action: ActionPreview, label: "Preview", desc: "show what would change"},
	{action: ActionCheck, label: "Health check", desc: "verify symlinks and tools"},
	{action: ActionSync, label: "Sync", desc: "git pull, then re-link"},
	{action: ActionUpdate, label: "Update", desc: "upgrade installed packages"},
	// Appended rather than slotted in, so the number keys 1-7 still mean what
	// anybody's hands already know.
	{action: ActionRemove, label: "Remove packages", desc: "uninstall tools you pick",
		selects: true, removes: true},
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
		m.menuAt = step(m.menuAt, -1, len(menu))
	case key.Matches(msg, m.keys.Down):
		m.menuAt = step(m.menuAt, 1, len(menu))
	case key.Matches(msg, m.keys.Top):
		m.menuAt = 0
	case key.Matches(msg, m.keys.Bottom):
		m.menuAt = len(menu) - 1
	case key.Matches(msg, m.keys.Update) && m.update != "":
		return m.begin(ActionSelfUpdate, nil)
	// The binary on disk is not the one running, and restarting is the only
	// thing left to do about that - from here as well as from the run screen
	// somebody may have already left.
	case key.Matches(msg, m.keys.Restart) && m.replaced != "":
		m.restart = true
		return m, tea.Quit
	case key.Matches(msg, m.keys.Open):
		chosen := menu[m.menuAt]
		if chosen.selects {
			return m.openSelector(chosen), nil
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
	rows = append(rows, s.group.Render("What would you like to do?"), s.pad(g.Text))
	cursorRow := len(rows)
	for i, entry := range menu {
		marker, label := s.pad(2), s.rowOff.Render(entry.label)
		if i == m.menuAt {
			marker, label = s.cursor.Render("› "), s.rowOn.Render(entry.label)
			cursorRow = len(rows)
		}
		left := marker + s.rowKey.Render(fmt.Sprintf("%d  ", i+1)) + label
		rows = append(rows, s.ends(g.Text, left, s.faint.Render(entry.desc)))
	}
	// Eight entries fit any card this program draws on a normal terminal, and
	// do not fit a twelve-row one - where the frame silently dropped the last
	// three and the cursor could still reach them.
	visible, offset := window(rows, cursorRow, g.Body)

	return s.chrome.Render(g, pane{
		Name:   m.name(),
		Rows:   s.chrome.BodyRows(g, strings.Join(visible, "\n"), offset, len(rows)),
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
	// Ranked by what a reader is stuck without, not by reading order. The way
	// out survives longest, then the way to act; "h help" goes first, because a
	// terminal too narrow to show it is one where the space is better spent.
	hints := []hint{
		{Text: "↑/↓ move", Keep: 3},
		{Text: "enter open", Keep: 2},
		{Text: "h help", Keep: 4},
		{Text: "q quit", Keep: 1},
	}
	switch {
	case m.replaced != "":
		hints = append(hints, hint{Text: "r restart to run " + m.replaced, Keep: 0})
	case m.update != "":
		hints = append(hints, hint{Text: "u update to " + m.update, Keep: 0})
	}
	return hints
}
