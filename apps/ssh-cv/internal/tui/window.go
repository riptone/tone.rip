package tui

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/riptone/tone.rip/packages/gotui"
)

// How big this program's card is allowed to get, and nothing else.
//
// The frame itself - the geometry arithmetic, the render, the title bar, the
// scrollbar, the footer's drop order, the clamp under all of it - is gotui's,
// shared with apps/doti. What is left here is the one thing the two programs
// genuinely disagree about: a CV is a document and wants a wide page, where a
// menu looks abandoned in one.
var spec = gotui.Spec{
	WidthMax:  78,
	HeightMax: 34,
	PadX:      2,
	// The scrollbar column plus a space between it and the text, reserved
	// whether or not the current section scrolls - so opening a long page does
	// not reflow a short one.
	Gutter:    2,
	WidthMin:  24,
	HeightMin: 8,
	// A card never collapses below this many body rows, however little it
	// holds: a window with two lines in it reads as an error message.
	MinBody: 4,
	// Under this many body rows the two blank spacer rows come out. In a short
	// terminal the frame is already most of the screen, and a row of CV is
	// worth more than a row of air.
	CompactBelow: 10,
}

type (
	geometry = gotui.Geometry
	pane     = gotui.Pane
	hint     = gotui.Hint
)

func geometryFor(width, height int) geometry { return spec.For(width, height) }

// fit shrinks the card to what it is holding, never below the spec's floor.
func fit(g geometry, lines int) geometry { return g.Fit(lines, spec.MinBody) }

func lineCount(block string) int { return gotui.LineCount(block) }

func clamp(view string, width, height int) string { return gotui.Clamp(view, width, height) }

// stripANSI is how a row is asked whether it has any text left in it, rather
// than any bytes.
func stripANSI(line string) string { return ansi.Strip(line) }

func (s styles) render(g geometry, p pane) string { return s.chrome.Render(g, p) }

// ends puts left at the left of a row and right at its right, and drops right
// entirely when there is no room for both - a status overlapping the hints
// beside it is worse than no status.
func (s styles) ends(width int, left, right string) string {
	return s.chrome.Ends(width, left, right)
}

func (s styles) bodyRows(g geometry, view string, offset, total int) []string {
	return s.chrome.BodyRows(g, view, offset, total)
}

func (s styles) scrollbar(rows, total, offset int) []string {
	return s.chrome.Scrollbar(rows, total, offset)
}
