package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The window.
//
// The CV is drawn as a small window - three buttons, a dim name, a rule, a
// body, a line of key hints - centred in whatever terminal it was given. Two
// reasons, one aesthetic and one structural.
//
// The aesthetic one: a CV is a document, and a document wants a page. Text
// that runs to the far edge of a 200-column terminal is unreadable in the same
// way a website with no max-width is unreadable, and the site this belongs to
// has spent a lot of care on exactly that measure.
//
// The structural one: a fixed page means one layout to reason about instead of
// a continuum of them. Everything below is measured from the geometry resolved
// once per resize, so a section cannot quietly render two columns wider than
// the frame it sits in.
//
// The frame is never dropped, however small the terminal gets. An earlier
// version took the border off below 40x14 and let the document fill the
// screen, which stopped looking like the same application at exactly the
// moment it was hardest to read; the card takes the whole terminal instead,
// margins first and then its own width.
const (
	cardWidthMax  = 78
	cardHeightMax = 34
	cardPadX      = 2

	// The scrollbar column, plus a space between it and the text. Reserved
	// whether or not the section scrolls, so opening a long page does not
	// reflow a short one.
	gutter = 2

	// Rows the card spends on itself: two borders, the title bar, the rule
	// under it, a blank above and below the body, and the footer.
	cardChrome = 7
	// The same without the two blanks. In a short terminal the frame is
	// already most of the screen, and a row of CV is worth more than a row of
	// air - so under compactBelow rows of body, the spacers come out.
	cardChromeCompact = 5
	compactBelow      = 10

	// What the card asks for before it starts giving up margin.
	cardWidthMin  = 24
	cardHeightMin = 8

	// A card never collapses below this many body rows, however little it
	// holds: a window with two lines in it reads as an error message.
	cardMinBody = 4
)

// geometry is the resolved size of one render, computed on resize and read
// everywhere else.
type geometry struct {
	// termWidth and termHeight are the terminal the card is centred in.
	termWidth  int
	termHeight int
	// width and height are the card's own size, borders included.
	width  int
	height int
	// inner is the usable width inside the borders and padding.
	inner int
	// text is what a line of text may occupy: inner, less the gutter.
	text int
	// body is how many rows the section body gets.
	body int
	// spaced is false in a terminal too short to spend two rows on air.
	spaced bool
}

// geometryFor sizes the card for a terminal.
//
// Margin goes first: three columns either side and a row top and bottom, until
// there is not enough terminal to spend on them, at which point the card takes
// everything. What it does not do is stop being a card.
func geometryFor(width, height int) geometry {
	// A PTY request with no size is not worth guessing about; assume the
	// terminal every terminal claims to be.
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}

	cardWidth := min(min(cardWidthMax, max(width-6, cardWidthMin)), width)
	cardHeight := min(min(cardHeightMax, max(height-2, cardHeightMin)), height)

	chrome, spaced := cardChrome, true
	if cardHeight-cardChrome < compactBelow {
		chrome, spaced = cardChromeCompact, false
	}

	inner := max(cardWidth-2-2*cardPadX, 1)
	return geometry{
		termWidth:  width,
		termHeight: height,
		width:      cardWidth,
		height:     cardHeight,
		inner:      inner,
		text:       max(inner-gutter, 1),
		body:       max(cardHeight-chrome, 1),
		spaced:     spaced,
	}
}

// fit shrinks the card to what it is holding.
//
// A window on a desktop is the size of its contents; one that stays 34 rows
// tall to show nine rows of index is a full-screen app pretending to be a
// window. Anything taller than the terminal allows keeps the full height and
// scrolls instead, which is what the scrollbar and the line counter are for.
func (g geometry) fit(lines int) geometry {
	body := min(max(lines, cardMinBody), g.body)
	g.height -= g.body - body
	g.body = body
	return g
}

// lineCount is how many rows a rendered block occupies. An empty block is one
// row, because that is what rendering it produces.
func lineCount(block string) int {
	return strings.Count(block, "\n") + 1
}

// pane is everything the frame needs to draw one screen.
type pane struct {
	// name is the right-aligned text on the title bar.
	name string
	// rows is the body, already exactly geometry.body rows tall.
	rows []string
	// hints are the key bindings, in display order - see footerRow for how
	// they are dropped, which is a different order.
	hints []hint
	// status is the right of the footer: a line counter, or the label of the
	// key that opened the session. Plain text, like the hints - the frame
	// styles both, so neither has to know what a footer looks like.
	status string
}

// render draws the card and centres it in the terminal.
func (s styles) render(g geometry, p pane) string {
	rows := make([]string, 0, len(p.rows)+5)
	rows = append(rows,
		s.titleBar(g, p.name),
		s.rule.Render(strings.Repeat("─", max(g.inner, 1))),
	)
	if g.spaced {
		rows = append(rows, "")
	}
	rows = append(rows, p.rows...)
	if g.spaced {
		rows = append(rows, "")
	}
	rows = append(rows, footerRow(g, s, p.hints, p.status))

	card := s.card.
		Width(g.width - 2).
		Height(g.height - 2).
		Render(strings.Join(rows, "\n"))
	return s.centre(g, card)
}

// centre puts the card in the middle of the terminal.
//
// Done by hand rather than with lipgloss.Place, which renders through the
// *default* renderer - the same trap sessionRenderer exists to avoid, and one
// that used to matter here when the surround was painted.
func (s styles) centre(g geometry, card string) string {
	left := max((g.termWidth-g.width)/2, 0)
	right := max(g.termWidth-g.width-left, 0)
	top := max((g.termHeight-g.height)/2, 0)

	blank := s.pad(g.termWidth)
	out := make([]string, 0, g.termHeight)
	for range top {
		out = append(out, blank)
	}
	for _, row := range strings.Split(card, "\n") {
		out = append(out, s.pad(left)+row+s.pad(right))
	}
	for len(out) < g.termHeight {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

// titleBar is the buttons, then the window's name at the far right.
func (s styles) titleBar(g geometry, name string) string {
	buttons := s.buttons()
	room := max(g.inner-lipgloss.Width(buttons)-2, 4)
	return s.ends(g.inner, buttons, s.winName.Render(truncate(name, room)))
}

// bodyRows pads a viewport's output to the full body height and hangs the
// scrollbar off the right edge of every row.
func (s styles) bodyRows(g geometry, view string, offset, total int) []string {
	lines := strings.Split(view, "\n")
	bars := s.scrollbar(g.body, total, offset)
	rows := make([]string, 0, g.body)
	for i := range g.body {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		if strings.TrimSpace(stripANSI(line)) == "" {
			rows = append(rows, s.pad(g.inner-1)+bars[i])
			continue
		}
		rows = append(rows, line+s.pad(g.inner-1-lipgloss.Width(line))+bars[i])
	}
	return rows
}

// scrollbar returns one glyph per body row.
//
// Two parts, like a real one: a thin line the whole height of the body, which
// says the document has a length, and a thicker section on top of it, which
// says where in that length you are. Neither appears when everything fits - a
// scrollbar that is always there tells you nothing, and one that appears only
// when there is more to read is the whole signal.
func (s styles) scrollbar(rows, total, offset int) []string {
	out := make([]string, max(rows, 0))
	for i := range out {
		out[i] = s.pad(1)
	}
	if rows <= 0 || total <= rows {
		return out
	}

	for i := range out {
		out[i] = s.track.Render("│")
	}

	size := max(rows*rows/total, 1)
	span := rows - size
	position := 0
	if scrollable := total - rows; scrollable > 0 && span > 0 {
		position = min(max(offset*span/scrollable, 0), span)
	}
	for i := position; i < position+size && i < rows; i++ {
		out[i] = s.bar.Render("┃")
	}
	return out
}

// statusRank is where the line counter sits in the same order the hints are
// ranked by: it outlives "l lang", because a reader who cannot tell there is
// more to read stops reading, and it is outlived by everything that says which
// key to press.
const statusRank = 4

// hint is one key legend, and how hard it fights to stay on screen.
//
// Display order and drop order are deliberately different. "↑/↓ scroll" reads
// best first, but "q quit" has to outlive "←/→ section": a reader who cannot
// page around is inconvenienced, and a reader who cannot see how to leave is
// trapped in somebody else's CV.
type hint struct {
	text string
	// keep is the drop order - lower survives longer.
	keep int
}

// footerRow lays out the footer: key hints at the left, status at the right,
// and everything competing for one row.
//
// Whatever will not fit is dropped by rank rather than by position, so a
// 46-column terminal keeps "↑/↓ scroll · esc back · q quit" and loses the
// counter, while a 60-column one keeps the counter and loses "l lang". What it
// must never do is show the arithmetic and no way out.
func footerRow(g geometry, s styles, hints []hint, status string) string {
	join := func(kept []hint) string {
		parts := make([]string, 0, len(kept))
		for _, h := range kept {
			parts = append(parts, h.text)
		}
		return strings.Join(parts, " · ")
	}

	kept := append([]hint(nil), hints...)
	showStatus := status != ""
	width := func() int {
		total := lipgloss.Width(join(kept))
		if showStatus {
			total += lipgloss.Width(status) + 2
		}
		return total
	}

	for width() > g.inner {
		worst := -1
		for i, h := range kept {
			if worst < 0 || h.keep > kept[worst].keep {
				worst = i
			}
		}
		if showStatus && (worst < 0 || statusRank > kept[worst].keep) {
			showStatus = false
			continue
		}
		if worst < 0 || len(kept) <= 1 {
			break
		}
		kept = append(kept[:worst], kept[worst+1:]...)
	}

	// Truncated before it is styled: cutting a string that already carries
	// escape sequences cuts one of them in half.
	left := s.footer.Render(truncate(join(kept), g.inner))
	if !showStatus {
		return left
	}
	return s.ends(g.inner, left, s.footer.Render(status))
}

// ends puts left at the left of a row and right at its right, and drops right
// entirely when there is no room for both - a status overlapping the hints
// beside it is worse than no status.
func (s styles) ends(width int, left, right string) string {
	if right == "" {
		return left
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + s.pad(gap) + right
}

// stripANSI is how a row is asked whether it has any text left in it, rather
// than any bytes: the viewport's own padding is spaces, and a styled empty
// line is escape sequences around nothing.
func stripANSI(line string) string {
	return ansi.Strip(line)
}

// clamp keeps a render inside the terminal it is going to, in both directions.
//
// A frame one row too tall scrolls its own title away and leaves a second
// footer behind, which is the most visible way a TUI can look broken; one
// column too wide wraps every line into the next. This is a backstop under the
// arithmetic rather than a substitute for it - and it truncates with ansi so a
// cut never lands in the middle of an escape sequence.
func clamp(view string, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "")
	}
	return strings.Join(lines, "\n")
}
