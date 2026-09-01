package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/riptone/tone.rip/packages/gotui"
)

// The help screen: what the keys do, and what each operation does.
//
// Built from the same tables the program acts on - the menu entries and the
// keymap - rather than written out beside them, because a help screen that
// describes a key the program no longer has is worse than no help screen. The
// menu's own descriptions appear here verbatim for the same reason.

// helpState is the help screen's scroll position.
type helpState struct {
	view viewport.Model
	// from is the screen to return to, so h is a detour rather than a
	// one-way trip out of whatever you were doing.
	from Screen
}

// helpGeometry is the wide card: this is prose, and prose in 58 columns is a
// column of fragments.
func (m Model) helpGeometry() geometry { return wideGeometryFor(m.width, m.height) }

// openHelp moves to the help screen, remembering where from.
func (m Model) openHelp() Model {
	g := m.helpGeometry()
	view := viewport.New(g.Text, g.Body)
	view.KeyMap.Up = m.keys.Up
	view.KeyMap.Down = m.keys.Down
	view.KeyMap.PageUp = m.keys.PageUp
	view.KeyMap.PageDown = m.keys.PageDown

	m.help = helpState{view: view, from: m.screen}
	m.screen = ScreenHelp
	return m.reflowHelp()
}

// reflowHelp re-renders the help at the current width.
func (m Model) reflowHelp() Model {
	g := m.helpGeometry()
	m.help.view.Width, m.help.view.Height = g.Text, g.Body
	m.help.view.SetContent(strings.Join(m.helpRows(g.Text), "\n"))
	return m
}

func (m Model) helpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Help), key.Matches(msg, m.keys.Back):
		// Back where you came from, including the middle of a finished run.
		m.screen = m.help.from
		return m, nil
	case key.Matches(msg, m.keys.Top):
		m.help.view.GotoTop()
		return m, nil
	case key.Matches(msg, m.keys.Bottom):
		m.help.view.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.help.view, cmd = m.help.view.Update(msg)
	return m, cmd
}

func (m Model) viewHelp() string {
	g := m.helpGeometry()
	s := m.styles

	hints := []hint{{Text: "esc back", Keep: 1}}
	if m.help.view.TotalLineCount() > m.help.view.Height {
		hints = append([]hint{{Text: "↑/↓ scroll", Keep: 2}}, hints...)
	}

	return s.chrome.Render(g, pane{
		Name:   m.name() + " · help",
		Rows:   s.chrome.BodyRows(g, m.help.view.View(), m.help.view.YOffset, m.help.view.TotalLineCount()),
		Hints:  hints,
		Status: "help",
	})
}

// helpRows is the whole text, wrapped and padded for one width.
//
// Padded because it goes into a viewport, which pads short lines itself with
// plain spaces that carry no background - see gotui.Surface.Fill.
func (m Model) helpRows(width int) []string {
	s := m.styles
	var rows []string

	heading := func(text string) {
		if len(rows) > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, s.group.Render(text))
	}
	// entry lays out a key or a name on the left and its meaning on the right,
	// in the column the menu already uses.
	entry := func(left, right string) {
		const gutter = 18
		// Measured in columns, not bytes: "↑ ↓ / k j" is nine columns and
		// fifteen bytes, and padding by the second put every arrow row one
		// column further left than the one above it.
		pad := max(gutter-lipgloss.Width(left), 1)
		body := gotui.Truncate(right, max(width-gutter-2, 8))
		rows = append(rows, s.pad(2)+s.rowKey.Render(left)+s.pad(pad)+s.body.Render(body))
	}
	paragraph := func(text string) {
		for _, line := range strings.Split(wrap(text, max(width-2, 8)), "\n") {
			rows = append(rows, s.pad(2)+s.faint.Render(line))
		}
	}

	heading("Moving around")
	entry("↑ ↓ / k j", "move, or scroll a log")
	entry("g / G", "first line / last line")
	entry("f / b", "page down / page up")
	entry("enter", "open, or confirm")
	entry("esc", "back, or quit from the menu")
	entry("h", "this screen")
	entry("q", "quit")

	heading("Choosing what to act on")
	entry("space / x", "tick or untick the component under the cursor")
	entry("a / n", "tick all / tick none")
	entry("→ / l", "open a group: the tools, casks, plugins or MCP servers in it")
	entry("←", "close it again")
	entry("tab", "open or close, whichever applies")
	paragraph("Install, Preview and Unlink open this list; Adopt opens it with " +
		"only what the machine is missing, and Remove packages with nothing " +
		"ticked - the ticking is the confirmation. A group's box shows ~ when " +
		"only some of what is inside it is ticked; space on one of those ticks " +
		"all of it.")

	heading("While something is running")
	entry("ctrl+c", "stop it: the footer says interrupted, in amber")
	entry("↑ ↓", "scroll the log, which stops it following the newest line")
	entry("G", "back to the newest line, and follow it again")
	entry("enter", "return here once it says done or failed")

	heading("What the operations do")
	for i, op := range menu {
		entry(string(rune('1'+i))+"  "+op.label, op.desc)
	}

	heading("When there is a newer release")
	entry("u", "install it, from the footer offer")
	entry("r", "relaunch, once it has replaced this binary")
	paragraph("The offer only appears when the check found something newer, " +
		"and never for a build made from a working copy.")

	heading("From a shell")
	entry("doti", "this window")
	entry("doti install", "the same thing, straight to the run screen")
	entry("--term", "print lines instead of drawing a window")
	entry("-n", "report what would change and write nothing")
	entry("--repo DIR", "act on a checkout other than ~/dotfiles")
	entry("--only PKG", "one config package")
	entry("--tools LIST", "narrow install or removal to these tools")

	for i, row := range rows {
		rows[i] = s.chrome.Fill(row, width)
	}
	return rows
}

// wrap breaks text at word boundaries, hard-breaking anything longer than the
// limit - the same choice the run log makes, and for the same reason: a path is
// one word.
func wrap(text string, limit int) string {
	return ansi.Wrap(text, limit, " /-_")
}
