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
	WidthMax:  64,
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

// wideSpec is the same card, bigger, for the screens that hold something long.
//
// A menu is seven short rows and looks abandoned in a wide frame. A run's log
// is the opposite: `git pull` explains a missing upstream in a paragraph, and
// 58 columns turn that into a column of fragments - which is what it did. The
// help text has the same problem for the same reason.
//
// Derived from spec rather than written out, so the padding and the gutter
// cannot drift from the ones the Chrome's card was built with.
var wideSpec = func() gotui.Spec {
	wide := spec
	wide.WidthMax, wide.HeightMax = 96, 40
	return wide
}()

func geometryFor(width, height int) geometry { return spec.For(width, height) }

// wideGeometryFor sizes the bigger card. Still a card: Spec.For gives up margin
// before width and never exceeds the terminal, so a 60-column window gets a
// 54-column frame rather than a broken one.
func wideGeometryFor(width, height int) geometry { return wideSpec.For(width, height) }

// fit shrinks the card to what it is holding, never below the spec's floor.
func fit(g geometry, lines int) geometry { return g.Fit(lines, spec.MinBody) }
