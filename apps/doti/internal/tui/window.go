package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/packages/gotui"
)

// The window.
//
// The same card apps/ssh-cv draws - three buttons, a dim name, a rule, a
// body, a line of key hints - centred in whatever terminal it was given, and
// for the same reasons: a fixed page is one layout to reason about instead of
// a continuum of them, and the two programs should look like they came from
// the same place.
//
// It is a good deal shorter than ssh-cv's because this UI is a menu rather
// than a document: no scrollbar, no per-section geometry, no footer hints
// dropped by rank. What is left is the frame.
const (
	cardWidthMax  = 64
	cardHeightMax = 24
	cardPadX      = 2

	// Rows the card spends on itself: two borders, the title bar, the rule
	// under it, a blank above and below the body, and the footer.
	cardChrome = 7

	cardWidthMin  = 28
	cardHeightMin = 10
)

// geometry is the resolved size of one render.
type geometry struct {
	termWidth  int
	termHeight int
	width      int
	height     int
	// inner is the usable width inside the borders and padding.
	inner int
	// body is how many rows the content gets.
	body int
}

// geometryFor sizes the card for a terminal. Margin goes first, then the
// card's own width - but it never stops being a card.
func geometryFor(width, height, rows int) geometry {
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}

	cardWidth := min(width-6, cardWidthMax)
	cardWidth = max(cardWidth, min(width, cardWidthMin))

	wanted := rows + cardChrome
	cardHeight := min(min(wanted, height-2), cardHeightMax)
	cardHeight = max(cardHeight, min(height, cardHeightMin))

	return geometry{
		termWidth:  width,
		termHeight: height,
		width:      cardWidth,
		height:     cardHeight,
		inner:      max(cardWidth-2-cardPadX*2, 1),
		body:       max(cardHeight-cardChrome, 1),
	}
}

// pane is one screen's worth of content.
type pane struct {
	name   string
	rows   []string
	hints  string
	status string
}

// render draws the card and centres it in the terminal.
func (s styles) render(g geometry, p pane) string {
	rows := make([]string, 0, len(p.rows)+5)
	rows = append(rows,
		s.titleBar(g, p.name),
		s.rule.Render(strings.Repeat("─", max(g.inner, 1))),
		"",
	)

	for i := range g.body {
		if i < len(p.rows) {
			row := p.rows[i]
			rows = append(rows, row+s.pad(g.inner-lipgloss.Width(row)))
			continue
		}
		rows = append(rows, s.pad(g.inner))
	}

	rows = append(rows, "", s.ends(g.inner,
		s.footer.Render(p.hints), s.footer.Render(p.status)))

	card := s.card.
		Width(g.width - 2).
		Height(g.height - 2).
		Render(strings.Join(rows, "\n"))
	return s.centre(g, card)
}

// centre puts the card in the middle of the terminal, painting the surround
// by hand rather than with lipgloss.Place - which renders through the default
// renderer and would leave the margin unpainted.
func (s styles) centre(g geometry, card string) string {
	return s.chrome.Centre(g.termWidth, g.termHeight, g.width, card)
}

// titleBar is the buttons, then the window's name at the far right.
func (s styles) titleBar(g geometry, name string) string {
	buttons := s.buttons()
	room := max(g.inner-lipgloss.Width(buttons)-2, 4)
	return s.ends(g.inner, buttons, s.winName.Render(truncate(name, room)))
}

// ends puts left at the start of a row and right at its end, padding the gap
// with black so the row has no holes in it.
func (s styles) ends(width int, left, right string) string {
	return s.chrome.Ends(width, left, right)
}

// truncate shortens to width, with an ellipsis when it has to.
func truncate(text string, width int) string {
	return gotui.Truncate(text, width)
}
