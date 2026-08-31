package gotui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The window both programs draw: three buttons, a dim name, a rule, a body,
// a line of key hints, centred in whatever terminal it was given.
//
// Two reasons it is a card at all, one aesthetic and one structural. The
// aesthetic one: a document wants a page, and text running to the far edge of a
// 200-column terminal is unreadable the same way a website with no max-width
// is. The structural one: a fixed page is one layout to reason about instead of
// a continuum of them - everything is measured from a geometry resolved once
// per resize, so no screen can quietly render two columns wider than the frame
// it sits in.
//
// The frame is never dropped, however small the terminal gets. An earlier
// version took the border off below 40x14 and let the content fill the screen,
// which stopped looking like the same application at exactly the moment it was
// hardest to read. The card takes the whole terminal instead: margin first,
// then its own width.

// Spec is the one thing the two programs disagree about - how big the card is
// allowed to get - plus the floors under it.
type Spec struct {
	// WidthMax and HeightMax are the card's ceiling. A CV is a document and
	// wants a wide page; a menu is a list and looks abandoned in one.
	WidthMax  int
	HeightMax int
	// PadX is the horizontal padding inside the border.
	PadX int
	// Gutter is the scrollbar column plus a space between it and the text.
	// Reserved whether or not the current screen scrolls, so opening a long
	// page does not reflow a short one. Zero for a program with nothing to
	// scroll.
	Gutter int
	// WidthMin and HeightMin are what the card asks for before it starts
	// giving up margin.
	WidthMin  int
	HeightMin int
	// MinBody is the fewest body rows a card will collapse to, however little
	// it holds: a window with two lines in it reads as an error message.
	MinBody int
	// CompactBelow is the body height under which the two blank spacer rows
	// come out. In a short terminal the frame is already most of the screen,
	// and a row of content is worth more than a row of air. Zero keeps them
	// always.
	CompactBelow int
}

// Rows the card spends on itself: two borders, the title bar, the rule under
// it, a blank above and below the body, and the footer.
const (
	chromeRows        = 7
	chromeRowsCompact = 5
)

// Geometry is the resolved size of one render: computed on resize, read
// everywhere else.
type Geometry struct {
	// TermWidth and TermHeight are the terminal the card is centred in.
	TermWidth  int
	TermHeight int
	// Width and Height are the card's own size, borders included.
	Width  int
	Height int
	// Inner is the usable width inside the borders and padding.
	Inner int
	// Text is what a line of text may occupy: Inner, less the gutter.
	Text int
	// Body is how many rows the content gets.
	Body int
	// Spaced is false in a terminal too short to spend two rows on air.
	Spaced bool
}

// For sizes the card for a terminal.
//
// Margin goes first: three columns either side and a row top and bottom, until
// there is not enough terminal to spend on them, at which point the card takes
// everything.
func (sp Spec) For(width, height int) Geometry {
	// A terminal that reports no size is not worth guessing about; assume the
	// one every terminal claims to be.
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}

	cardWidth := min(min(sp.WidthMax, max(width-6, sp.WidthMin)), width)
	cardHeight := min(min(sp.HeightMax, max(height-2, sp.HeightMin)), height)

	chrome, spaced := chromeRows, true
	if cardHeight-chromeRows < sp.CompactBelow {
		chrome, spaced = chromeRowsCompact, false
	}

	inner := max(cardWidth-2-2*sp.PadX, 1)
	return Geometry{
		TermWidth:  width,
		TermHeight: height,
		Width:      cardWidth,
		Height:     cardHeight,
		Inner:      inner,
		Text:       max(inner-sp.Gutter, 1),
		Body:       max(cardHeight-chrome, 1),
		Spaced:     spaced,
	}
}

// Fit shrinks the card to what it is holding.
//
// A window on a desktop is the size of its contents; one that stays 34 rows
// tall to show nine rows of menu is a full-screen app pretending to be a
// window. Anything taller than the terminal allows keeps the full height and
// scrolls instead, which is what the scrollbar and the counter are for.
func (g Geometry) Fit(lines, minBody int) Geometry {
	body := min(max(lines, minBody), g.Body)
	g.Height -= g.Body - body
	g.Body = body
	return g
}

// Pane is everything the frame needs to draw one screen.
type Pane struct {
	// Name is the right-aligned text on the title bar.
	Name string
	// Rows is the body. Shorter than Geometry.Body is padded; longer is cut,
	// because a card that grows past its own frame is the most visible way a
	// TUI can look broken.
	Rows []string
	// Hints are the key bindings, in display order - FooterRow drops them in a
	// different one.
	Hints []Hint
	// Status is the right of the footer: a counter, a state, a label. Plain
	// text, like the hints - the frame styles both, so neither has to know
	// what a footer looks like.
	Status string
	// StatusColour overrides the faint the status is drawn in.
	//
	// For the one thing a status says that is worth finding without reading:
	// how a run ended. "done" and "failed" in the same grey are two words that
	// have to be told apart by spelling. Nil keeps the faint.
	StatusColour lipgloss.TerminalColor
}

// Chrome is the frame's own styles and the primitives that depend on them.
//
// It holds only what the frame draws. Everything a program puts *inside* the
// body - its headings, its rows, its cursor - stays in that program, which is
// the line between "the two look like one application" and "the two are one
// application".
type Chrome struct {
	Surface

	Spec Spec

	Card    lipgloss.Style
	Rule    lipgloss.Style
	WinName lipgloss.Style
	Footer  lipgloss.Style
	// Track and Bar are the scrollbar's two parts: the rule and text colours
	// under names that say what they are for.
	Track lipgloss.Style
	Bar   lipgloss.Style
}

// NewChrome builds the frame against one renderer.
//
// The renderer is the reason colour works at all - see LocalRenderer and
// ClampProfile for what happens when it is left to the default.
func NewChrome(r *lipgloss.Renderer, spec Spec) Chrome {
	surface := NewSurface(r)
	base := surface.Base
	return Chrome{
		Surface: surface,
		Spec:    spec,
		Card: base.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Rule).
			BorderBackground(Black).
			Padding(0, spec.PadX),
		Rule:    base.Foreground(Rule),
		WinName: base.Foreground(Faint),
		Footer:  base.Foreground(Faint),
		Track:   base.Foreground(Rule),
		Bar:     base.Foreground(Text),
	}
}

// Geometry resolves this chrome's spec for a terminal size.
func (c Chrome) Geometry(width, height int) Geometry { return c.Spec.For(width, height) }

// Render draws the card and centres it in the terminal.
func (c Chrome) Render(g Geometry, p Pane) string {
	rows := make([]string, 0, len(p.Rows)+5)
	rows = append(rows,
		c.TitleBar(g, p.Name),
		c.Rule.Render(strings.Repeat("─", max(g.Inner, 1))),
	)
	if g.Spaced {
		rows = append(rows, "")
	}
	for i := range g.Body {
		if i < len(p.Rows) {
			row := p.Rows[i]
			rows = append(rows, row+c.Pad(g.Inner-lipgloss.Width(row)))
			continue
		}
		rows = append(rows, c.Pad(g.Inner))
	}
	if g.Spaced {
		rows = append(rows, "")
	}
	rows = append(rows, c.FooterRow(g, p))

	card := c.Card.
		Width(g.Width - 2).
		Height(g.Height - 2).
		Render(strings.Join(rows, "\n"))
	return c.Centre(g.TermWidth, g.TermHeight, g.Width, card)
}

// TitleBar is the buttons, then the window's name at the far right.
func (c Chrome) TitleBar(g Geometry, name string) string {
	buttons := c.Buttons()
	room := max(g.Inner-lipgloss.Width(buttons)-2, 4)
	return c.Ends(g.Inner, buttons, c.WinName.Render(Truncate(name, room)))
}

// LineCount is how many rows a rendered block occupies. An empty block is one
// row, because that is what rendering it produces.
func LineCount(block string) int { return strings.Count(block, "\n") + 1 }

// Clamp keeps a render inside the terminal it is going to, in both directions.
//
// A frame one row too tall scrolls its own title away and leaves a second
// footer behind, which is the most visible way a TUI can look broken; one
// column too wide wraps every line into the next. This is a backstop under the
// arithmetic rather than a substitute for it - and it truncates with ansi so a
// cut never lands in the middle of an escape sequence.
func Clamp(view string, width, height int) string {
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
