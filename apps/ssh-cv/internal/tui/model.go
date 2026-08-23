// Package tui renders the CV as a terminal user interface.
//
// The shape is an index and a page: a list of everything the CV holds, and one
// screen per entry. That replaces a tabbed version where three tabs each held
// every section of their category stacked end to end, so reading about one
// role meant scrolling past two others, a skills list and a set of bordered
// pills. A CV is a document with parts, and the fastest way to read a part is
// to open it.
//
// One Bubble Tea model owns both views. They share a viewport, a language and
// a window; splitting them into two models would cost more coordination than
// the separation is worth.
package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tonil/apps/ssh-cv/internal/authz"
	"github.com/riptone/tonil/apps/ssh-cv/internal/cv"
)

// Config is everything a session needs to render.
type Config struct {
	// Doc is the CV. Its first language is the one the session opens in.
	Doc *cv.Document
	// Grant is what the connecting key resolved to. Nothing here is gated -
	// this CV is as public as the website - but a recognised key is worth
	// naming in the footer, because it is how you tell which of your own
	// machines you are on.
	Grant authz.Grant
	// Width and Height come from the SSH PTY request.
	Width  int
	Height int
	// Fingerprint of the connecting key, shown when there is no label to show
	// instead. Empty for a session that offered no key.
	Fingerprint string
	// Renderer is the session's own lipgloss renderer, from
	// bubbletea.MakeRenderer. It decides the colour profile, and it has to
	// come from the session: lipgloss's default renderer is bound to the
	// server's stdout, which is a pipe under systemd, which resolves to no
	// colour at all. Nil is fine for a local run - see newStyles.
	Renderer *lipgloss.Renderer
}

type keymap struct {
	quit     key.Binding
	back     key.Binding
	open     key.Binding
	up       key.Binding
	down     key.Binding
	pageUp   key.Binding
	pageDown key.Binding
	top      key.Binding
	bottom   key.Binding
	next     key.Binding
	prev     key.Binding
	lang     key.Binding
}

func newKeymap() keymap {
	return keymap{
		quit:     key.NewBinding(key.WithKeys("q", "ctrl+c", "esc")),
		back:     key.NewBinding(key.WithKeys("esc", "backspace")),
		open:     key.NewBinding(key.WithKeys("enter")),
		up:       key.NewBinding(key.WithKeys("up", "k")),
		down:     key.NewBinding(key.WithKeys("down", "j")),
		pageUp:   key.NewBinding(key.WithKeys("pgup", "b")),
		pageDown: key.NewBinding(key.WithKeys("pgdown", " ", "f")),
		top:      key.NewBinding(key.WithKeys("home", "g")),
		bottom:   key.NewBinding(key.WithKeys("end", "G")),
		// Forward and back through the CV. These were tab and shift+tab,
		// which nobody reaches for in a document - the arrows are what the
		// hand does, and h/l are not free because l switches language.
		next: key.NewBinding(key.WithKeys("right")),
		prev: key.NewBinding(key.WithKeys("left")),
		lang: key.NewBinding(key.WithKeys("l")),
	}
}

// Model is the Bubble Tea model for one SSH session.
type Model struct {
	cfg    Config
	styles styles
	keys   keymap
	labels labels

	// geoMax is what the terminal allows; geo is the card actually drawn,
	// shrunk to the page it is holding. baseBody is the index's own height,
	// which every page is at least as tall as - see resize.
	geoMax   geometry
	geo      geometry
	baseBody int

	view  viewport.Model
	items []item
	// cursor is the selected index row; open is the item being read, or -1
	// while the index is showing.
	cursor int
	open   int
	lang   int

	ready   bool
	quitted bool
}

// New builds a model for one session.
func New(cfg Config) Model {
	if cfg.Doc == nil || len(cfg.Doc.Langs) == 0 {
		// Guarded rather than trusted: this runs inside a session handler, so
		// a nil here would take somebody's connection down with a stack
		// trace instead of showing them an empty CV.
		cfg.Doc = &cv.Document{
			Langs:  []string{"en"},
			ByLang: map[string]cv.Content{"en": {}},
		}
	}

	m := Model{
		cfg:    cfg,
		styles: newStyles(cfg.Renderer),
		keys:   newKeymap(),
		labels: labelsFor(cfg.Doc.Langs[0], cfg.Doc.Content(cfg.Doc.Langs[0]).Labels),
		open:   -1,
	}
	m.items = buildItems(m.content(), m.labels)
	m.resize(cfg.Width, cfg.Height)
	return m
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) content() cv.Content {
	return m.cfg.Doc.Content(m.cfg.Doc.Langs[m.lang])
}

// reading reports whether a page is open, which is the one piece of state
// every key binding and both footers branch on.
func (m Model) reading() bool {
	return m.open >= 0 && m.open < len(m.items)
}

// scrollable reports whether the open page is longer than the body it is
// rendered into.
func (m Model) scrollable() bool {
	return m.view.TotalLineCount() > m.view.Height
}

func (m *Model) resize(width, height int) {
	m.geoMax = geometryFor(width, height)
	m.geo = m.geoMax
	if !m.ready {
		m.view = viewport.New(m.geoMax.text, m.geoMax.body)
		m.ready = true
	}
	// The index sets the session's resting height, so that stepping between
	// two short pages does not make the window twitch - it only grows for a
	// page that genuinely needs the room.
	index, _ := m.renderIndex(m.geoMax.text)
	m.baseBody = min(lineCount(index), m.geoMax.body)
	m.refresh()
}

// refresh re-renders the current view into the viewport and resizes the card
// around it, so View stays a pure read of state that is already settled.
func (m *Model) refresh() {
	if !m.ready {
		return
	}

	body, cursorLine := "", -1
	if m.reading() {
		body = m.renderSection(m.items[m.open], m.geoMax.text)
	} else {
		body, cursorLine = m.renderIndex(m.geoMax.text)
	}

	m.geo = m.geoMax.fit(max(lineCount(body), m.baseBody))
	m.view.Width = m.geo.text
	m.view.Height = m.geo.body
	m.view.SetContent(body)
	// Re-clamping the offset is the whole fix for a real bug: scroll down in a
	// short terminal, then make it taller, and the viewport keeps the offset
	// it had while rendering a taller window - so the page ends halfway up the
	// card with a screenful of nothing under it, until a keypress happens to
	// clamp it. SetYOffset clamps to what the content and the new height allow.
	m.view.SetYOffset(m.view.YOffset)
	if cursorLine >= 0 {
		m.showLine(cursorLine)
	}
}

// showLine scrolls the index by the smallest amount that puts a line on
// screen, so a long list in a short terminal follows the cursor instead of
// hiding it.
func (m *Model) showLine(line int) {
	if line < m.view.YOffset {
		m.view.SetYOffset(line)
		return
	}
	if bottom := m.view.YOffset + m.view.Height - 1; line > bottom {
		m.view.SetYOffset(line - m.view.Height + 1)
	}
}

func (m *Model) moveTo(index int) {
	if len(m.items) == 0 {
		return
	}
	m.cursor = min(max(index, 0), len(m.items)-1)
	m.refresh()
}

// move walks the index, wrapping at both ends: with nine rows, wrapping is
// what makes ↑ from the top useful rather than a key that does nothing.
func (m *Model) move(delta int) {
	if len(m.items) == 0 {
		return
	}
	m.cursor = (m.cursor + delta + len(m.items)) % len(m.items)
	m.refresh()
}

func (m *Model) openItem(index int) {
	if index < 0 || index >= len(m.items) {
		return
	}
	m.cursor, m.open = index, index
	m.refresh()
	m.view.GotoTop()
}

func (m *Model) closePage() {
	m.open = -1
	m.refresh()
}

// step is → and ←: the next entry in the index, or - while reading - the next
// page outright, so the whole CV can be read through without returning to the
// list between sections.
func (m *Model) step(delta int) {
	reading := m.reading()
	m.move(delta)
	if reading {
		m.openItem(m.cursor)
	}
}

// setLang switches language, keeping the reader where they were: toggling
// while reading a role re-renders that role, it does not throw you back to
// the index.
func (m *Model) setLang(index int) {
	m.lang = index % len(m.cfg.Doc.Langs)
	m.labels = labelsFor(m.cfg.Doc.Langs[m.lang], m.content().Labels)
	m.items = buildItems(m.content(), m.labels)

	// A language whose CV omits a section has a shorter index, so both
	// positions have to be re-checked rather than assumed.
	if len(m.items) == 0 {
		m.cursor, m.open = 0, -1
	} else {
		m.cursor = min(m.cursor, len(m.items)-1)
		if m.open >= len(m.items) {
			m.open = len(m.items) - 1
		}
	}
	m.refresh()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Keys that arrive in one read are one message, and its String() is the
	// whole run - so a held-down j over a laggy link becomes "jjj", matches
	// no binding, and the CV appears to ignore the keyboard. Found by driving
	// a real session over SSH, where the coalescing is the normal case rather
	// than the exception; each rune is handled as the keypress it was.
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 {
		for _, r := range msg.Runes {
			updated, cmd := m.handleKey(tea.KeyMsg{
				Type: tea.KeyRunes, Runes: []rune{r}, Alt: msg.Alt,
			})
			m = updated.(Model)
			if cmd != nil {
				// Something in the run ended the session; the rest of it is
				// typing into a closed window.
				return m, cmd
			}
		}
		return m, nil
	}

	switch {
	// Checked before quit, because esc is bound to both: inside a page it
	// means "back to the index", and losing the session because you wanted to
	// close a page would be rude. On the index it is the way out.
	case m.reading() && key.Matches(msg, m.keys.back):
		m.closePage()

	case key.Matches(msg, m.keys.quit):
		m.quitted = true
		return m, tea.Quit

	case key.Matches(msg, m.keys.lang):
		m.setLang(m.lang + 1)

	// → is "forward": into the highlighted row from the index, on to the next
	// page from inside one. ← is its opposite, and does nothing at the top
	// level, where there is nothing behind the index to go back to.
	case key.Matches(msg, m.keys.next):
		if m.reading() {
			m.step(1)
		} else {
			m.openItem(m.cursor)
		}

	case m.reading() && key.Matches(msg, m.keys.prev):
		m.step(-1)

	case !m.reading() && key.Matches(msg, m.keys.open):
		m.openItem(m.cursor)

	case key.Matches(msg, m.keys.up):
		if m.reading() {
			m.view.ScrollUp(1)
		} else {
			m.move(-1)
		}

	case key.Matches(msg, m.keys.down):
		if m.reading() {
			m.view.ScrollDown(1)
		} else {
			m.move(1)
		}

	case key.Matches(msg, m.keys.top):
		if m.reading() {
			m.view.GotoTop()
		} else {
			m.moveTo(0)
		}

	case key.Matches(msg, m.keys.bottom):
		if m.reading() {
			m.view.GotoBottom()
		} else {
			m.moveTo(len(m.items) - 1)
		}

	// Page keys scroll a page and do nothing in the index, where there is no
	// page to turn: space jumping the cursor to the far end of nine rows
	// would be a surprise, not a shortcut.
	case m.reading() && key.Matches(msg, m.keys.pageUp):
		m.view.PageUp()

	case m.reading() && key.Matches(msg, m.keys.pageDown):
		m.view.PageDown()
	}
	return m, nil
}

func (m Model) View() string {
	if m.quitted {
		return ""
	}
	rows := m.styles.bodyRows(m.geo, m.view.View(),
		m.view.YOffset, m.view.TotalLineCount())

	return clamp(m.styles.render(m.geo, pane{
		name:   m.windowName(),
		rows:   rows,
		hints:  m.hints(),
		status: m.status(),
	}), m.geo.termWidth, m.geo.termHeight)
}

// windowName is the title-bar text: whose CV, and which language it is being
// read in. The language belongs here rather than in the footer because it is
// a property of the whole document, not of the view.
func (m Model) windowName() string {
	return "tone — " + m.labels.app + " · " + m.cfg.Doc.Langs[m.lang]
}

// hints are the key bindings in display order, each with how hard it fights
// for its place - the footer drops by that rank, not by position. They say
// only what the current view can actually do: no scroll hint on a page that
// already fits.
func (m Model) hints() []hint {
	l := m.labels
	if m.reading() {
		hints := make([]hint, 0, 5)
		if m.scrollable() {
			hints = append(hints, hint{"↑/↓ " + l.scroll, 0})
		}
		return append(hints,
			hint{"←/→ " + l.section, 3},
			hint{"esc " + l.back, 2},
			hint{"q " + l.quit, 1},
			hint{"l " + l.language, 5},
		)
	}
	return []hint{
		{"↑/↓ " + l.move, 0},
		{"enter " + l.open, 2},
		{"q " + l.quit, 1},
		{"l " + l.language, 5},
	}
}

// status is the right end of the footer: how far through you are when there is
// more than fits, and otherwise - in the index - which key you arrived with.
//
// The counter is shown for the index too, not only for a page. In a short
// terminal the list is the thing most likely to be cut off, and "1-7 of 14"
// beside a scrollbar is how a reader knows to keep pressing ↓ rather than
// assuming the CV is six lines long.
func (m Model) status() string {
	if m.scrollable() {
		total := m.view.TotalLineCount()
		first := m.view.YOffset + 1
		last := min(m.view.YOffset+m.view.Height, total)
		return fmt.Sprintf("%d-%d %s %d", first, last, m.labels.of, total)
	}
	if m.reading() {
		return ""
	}
	if label := m.cfg.Grant.Label; label != "" {
		return label
	}
	if m.cfg.Fingerprint != "" {
		return shortFingerprint(m.cfg.Fingerprint)
	}
	return ""
}

// shortFingerprint trims a SHA256 fingerprint to something that fits a footer
// while staying long enough to recognise your own key.
func shortFingerprint(fingerprint string) string {
	if len(fingerprint) <= 18 {
		return fingerprint
	}
	return fingerprint[:18] + "…"
}
