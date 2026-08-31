package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/packages/gotui"
)

// The palette.
//
// The values and the primitives that depend on them live in packages/gotui,
// shared with apps/ssh-cv, which draws the same card. They started duplicated
// here on purpose - an abstraction cannot be factored correctly out of one
// example - and moved once this UI existed to factor them against.
//
// The rules they obey, and why:
//
//   - Everything is painted, and the black is #000000. A near-black reads as
//     grey next to a terminal that is actually black.
//   - Nothing resolves against the reader's terminal. Fixed hex, never
//     AdaptiveColor, so the same screen looks the same for everybody.
//   - Every style carries the background. An inner style's reset ends the
//     background an outer one started, so a nested foreground punches a hole
//     in the black for the rest of that line. Every gap goes through pad.
//   - The hex survives 256-colour quantisation. Greys below #303030 collapse
//     onto one index and tinted darks can land on a cube colour - the comment
//     numbers are where each value actually lands.
var (
	colBlack = gotui.Black

	colText  = gotui.Text  // the row under the cursor
	colMuted = gotui.Muted // rows that are not
	colFaint = gotui.Faint // key hints, asides, status
	colRule  = gotui.Rule  // the border and the rules

	// The cursor, and the only place the site's accent appears.
	colAccent = gotui.Accent

	// A selected checkbox: the same green as the zoom button, under its own
	// name because "chosen" and "a window control" are different jobs that
	// happen to share a hue. Green rather than the accent so "chosen" and
	// "where I am" never have to be told apart by position.
	colOn = gotui.Zoom
	// Something already installed or already linked - present, but not a
	// thing this run will do.
	colDone = gotui.Faint

	colClose    = gotui.Close
	colMinimise = gotui.Minimise
	colZoom     = gotui.Zoom
)

// styles is the whole vocabulary. Nothing builds a style inline.
type styles struct {
	// chrome is the shared surface: the black base and the primitives that
	// must not be reimplemented (pad, buttons, ends, centre).
	chrome gotui.Surface

	card    lipgloss.Style
	surface lipgloss.Style
	button  lipgloss.Style
	winName lipgloss.Style
	rule    lipgloss.Style
	footer  lipgloss.Style

	group  lipgloss.Style
	cursor lipgloss.Style
	rowOn  lipgloss.Style
	rowOff lipgloss.Style
	rowKey lipgloss.Style

	check lipgloss.Style
	done  lipgloss.Style
	body  lipgloss.Style
	faint lipgloss.Style
}

func newStyles(r *lipgloss.Renderer) styles {
	if r == nil {
		r = lipgloss.DefaultRenderer()
	}
	chrome := gotui.NewSurface(r)
	base := chrome.Base

	return styles{
		chrome: chrome,
		card: base.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colRule).
			BorderBackground(colBlack).
			Padding(0, cardPadX),
		surface: base,
		button:  base,
		winName: base.Foreground(colFaint),
		rule:    base.Foreground(colRule),
		footer:  base.Foreground(colFaint),

		group:  base.Foreground(colText).Bold(true),
		cursor: base.Foreground(colAccent).Bold(true),
		rowOn:  base.Foreground(colText).Bold(true),
		rowOff: base.Foreground(colMuted),
		rowKey: base.Foreground(colFaint),

		check: base.Foreground(colOn).Bold(true),
		done:  base.Foreground(colDone),
		body:  base.Foreground(colText),
		faint: base.Foreground(colFaint),
	}
}

// pad is n columns of black, and the only way to write a gap.
func (s styles) pad(n int) string { return s.chrome.Pad(n) }

// buttons renders the three window buttons.
func (s styles) buttons() string { return s.chrome.Buttons() }
