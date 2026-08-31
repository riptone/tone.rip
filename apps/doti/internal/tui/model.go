package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Screen is which view is on top.
type Screen int

const (
	// ScreenMenu is the list of operations.
	ScreenMenu Screen = iota
	// ScreenSelect is the per-component toggle list reached from Install.
	ScreenSelect
)

// Action is what the user chose, read by main once the program exits.
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
)

type menuEntry struct {
	action Action
	label  string
	desc   string
}

// The same operations the shell installer's menu offered, in the same order.
// Changing what the tool does should not silently change what people's hands
// already know.
var menu = []menuEntry{
	{ActionInstall, "Install", "packages and configs"},
	{ActionUnlink, "Uninstall", "remove symlinks (packages stay)"},
	{ActionAdopt, "Adopt", "install only what is missing"},
	{ActionPreview, "Preview", "show what would change"},
	{ActionCheck, "Health check", "verify symlinks and tools"},
	{ActionSync, "Sync", "git pull, then re-link"},
	{ActionUpdate, "Update", "upgrade installed packages"},
}

// Item is one toggleable component on the select screen.
type Item struct {
	// Group is the heading it sits under: "Packages", "Configs", "Secrets".
	Group string
	Label string
	// Status is the machine's current state for this item - "installed",
	// "linked", "not linked". Shown dim on the right.
	Status string
	// Done means the machine already has it. It stays selectable, because
	// re-linking is how drift gets repaired.
	Done bool
	// Selected is the checkbox. Defaults on.
	Selected bool
}

// Config builds a model.
type Config struct {
	Items []Item
	// Version is shown in the title bar, like ssh-cv shows the language.
	Version  string
	Width    int
	Height   int
	Renderer *lipgloss.Renderer
}

// Model is the whole UI.
type Model struct {
	styles  styles
	version string

	screen Screen
	menuAt int

	items   []Item
	itemAt  int
	rows    []row
	width   int
	height  int
	action  Action
	confirm bool
	quit    bool
}

// row is one rendered line of the select screen: either a group heading or
// an item. Flattened once so the cursor moves over items only.
type row struct {
	heading string
	item    int
}

// New builds the model.
func New(cfg Config) Model {
	m := Model{
		styles:  newStyles(cfg.Renderer),
		version: cfg.Version,
		items:   append([]Item(nil), cfg.Items...),
		width:   cfg.Width,
		height:  cfg.Height,
	}
	m.rows = flatten(m.items)
	return m
}

func flatten(items []Item) []row {
	var rows []row
	group := ""
	for i, item := range items {
		if item.Group != group {
			group = item.Group
			rows = append(rows, row{heading: group, item: -1})
		}
		rows = append(rows, row{item: i})
	}
	return rows
}

// Action is what the user chose; empty when they quit.
func (m Model) Action() Action { return m.action }

// Chosen is the labels left ticked, valid once Action is Install.
func (m Model) Chosen() []string {
	var out []string
	for _, item := range m.items {
		if item.Selected {
			out = append(out, item.Label)
		}
	}
	return out
}

func (m Model) Init() tea.Cmd { return nil }

// Update handles one message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quit = true
		return m, tea.Quit
	}

	if m.screen == ScreenMenu {
		return m.menuKey(msg)
	}
	return m.selectKey(msg)
}

func (m Model) menuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.menuAt = max(m.menuAt-1, 0)
	case "down", "j":
		m.menuAt = min(m.menuAt+1, len(menu)-1)
	case "enter", "right", "l":
		chosen := menu[m.menuAt].action
		// Install is the only operation that asks what to include; the rest
		// act on everything or on nothing, so a selector would be a keypress
		// that never changes the outcome.
		if chosen == ActionInstall || chosen == ActionAdopt {
			m.action = chosen
			m.screen = ScreenSelect
			return m, nil
		}
		m.action = chosen
		return m, tea.Quit
	}
	if n := digit(msg.String()); n >= 1 && n <= len(menu) {
		m.menuAt = n - 1
	}
	return m, nil
}

func (m Model) selectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "left", "h":
		m.screen = ScreenMenu
		m.action = ActionNone
		return m, nil
	case "up", "k":
		m.itemAt = max(m.itemAt-1, 0)
	case "down", "j":
		m.itemAt = min(m.itemAt+1, len(m.items)-1)
	case " ", "x":
		if len(m.items) > 0 {
			m.items[m.itemAt].Selected = !m.items[m.itemAt].Selected
		}
	case "a":
		m.setAll(true)
	case "n":
		m.setAll(false)
	case "enter":
		m.confirm = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) setAll(on bool) {
	for i := range m.items {
		m.items[i].Selected = on
	}
}

func digit(key string) int {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return 0
	}
	return int(key[0] - '0')
}

// View renders the current screen.
func (m Model) View() string {
	if m.quit {
		return ""
	}
	if m.screen == ScreenMenu {
		return m.viewMenu()
	}
	return m.viewSelect()
}

func (m Model) name() string {
	if m.version == "" {
		return "doti"
	}
	return "doti · " + m.version
}

func (m Model) viewMenu() string {
	// heading + blank + one row per entry.
	g := geometryFor(m.width, m.height, len(menu)+2)
	s := m.styles

	rows := make([]string, 0, len(menu)+1)
	rows = append(rows, s.group.Render("What would you like to do?"), s.pad(g.inner))
	for i, entry := range menu {
		marker, label := s.pad(2), s.rowOff.Render(entry.label)
		if i == m.menuAt {
			marker, label = s.cursor.Render("› ")+"", s.rowOn.Render(entry.label)
		}
		left := marker + s.rowKey.Render(fmt.Sprintf("%d  ", i+1)) + label
		right := s.faint.Render(entry.desc)
		rows = append(rows, s.ends(g.inner, left, right))
	}

	return s.render(g, pane{
		name:   m.name(),
		rows:   rows,
		hints:  "↑/↓ move · enter open · q quit",
		status: "menu",
	})
}

func (m Model) viewSelect() string {
	// hint line + blank + one row per group heading and item.
	g := geometryFor(m.width, m.height, len(m.rows)+2)
	s := m.styles

	rows := make([]string, 0, len(m.rows)+1)
	rows = append(rows,
		s.faint.Render("space toggles · a all · n none · enter confirm"),
		s.pad(g.inner))

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
		left := marker + box + s.pad(1) + label
		rows = append(rows, s.ends(g.inner, left, status))
	}

	selected := 0
	for _, item := range m.items {
		if item.Selected {
			selected++
		}
	}

	return s.render(g, pane{
		name:   m.name(),
		rows:   rows,
		hints:  "space toggle · enter confirm · esc back",
		status: fmt.Sprintf("%d of %d", selected, len(m.items)),
	})
}

// Confirmed reports whether the user pressed enter on the select screen
// rather than backing out or quitting.
func (m Model) Confirmed() bool { return m.confirm }
