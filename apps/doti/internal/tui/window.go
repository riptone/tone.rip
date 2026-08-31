package tui

import "github.com/riptone/tone.rip/packages/gotui"

// How big this program's card is allowed to get, and nothing else.
//
// The frame itself - the geometry arithmetic, the render, the title bar, the
// scrollbar, the footer's drop order, the clamp under all of it - is gotui's,
// shared with apps/ssh-cv. This file used to be 133 lines of the same
// arithmetic with different names.
var spec = gotui.Spec{
	// Narrower than the CV's 78: that is a document and wants a page, this is
	// a list of seven things and looks abandoned in one. Width is a ceiling
	// rather than a size - Fit shrinks the card to what it holds.
	WidthMax: 64,
	// Taller than the menu will ever need, for the one screen that has more
	// than a screenful: a run's log. The menu is unaffected, because Fit
	// takes the height back down to its content.
	HeightMax: 28,
	PadX:      2,
	// The scrollbar column plus a space, reserved on every screen so opening
	// a long run does not reflow the menu behind it.
	Gutter:    2,
	WidthMin:  28,
	HeightMin: 10,
	MinBody:   4,
	// Under this many body rows the two blank spacer rows come out: in a
	// short terminal a row of output is worth more than a row of air.
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
