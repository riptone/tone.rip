package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/packages/gotui"
)

// The styles for this program's *content* - menu rows, checkboxes, a run's log.
//
// The frame around them and the palette they draw from are gotui's, shared with
// apps/ssh-cv. What is worth repeating here is the one rule the body has to
// obey: every style carries the background, because an inner style's reset ends
// the background an outer one started - so a nested foreground punches a hole
// in the black for the rest of that line. Every gap goes through pad.

// styles is the whole vocabulary. Nothing builds a style inline.
type styles struct {
	// chrome is the frame: the black surface, the card, the rule, the
	// scrollbar, the footer, and the primitives that must not be
	// reimplemented (pad, buttons, ends, centre).
	chrome gotui.Chrome

	group  lipgloss.Style
	cursor lipgloss.Style
	rowOn  lipgloss.Style
	rowOff lipgloss.Style
	rowKey lipgloss.Style

	// check is a ticked box: the same green as the zoom button, under its own
	// name because "chosen" and "a window control" are different jobs that
	// happen to share a hue. Green rather than the accent so "chosen" and
	// "where I am" never have to be told apart by position.
	check lipgloss.Style
	// done is something the machine already has - present, but not a thing
	// this run will do.
	done  lipgloss.Style
	body  lipgloss.Style
	faint lipgloss.Style
	// warn is an answer to a key that was just pressed and did not do what the
	// reader expected - the accent, so it reads as the same register as a
	// MarkWarn line rather than as a new colour to learn.
	warn lipgloss.Style

	// The report marks, one style per Mark, so a line in the window and the
	// same line on stdout carry the same colour.
	marks map[app.Mark]lipgloss.Style
	// phase is a run's section headings: "packages", "configs", "secrets".
	phase lipgloss.Style
}

func newStyles(r *lipgloss.Renderer) styles {
	if r == nil {
		r = lipgloss.DefaultRenderer()
	}
	chrome := gotui.NewChrome(r, spec)
	base := chrome.Base

	return styles{
		chrome: chrome,

		group:  base.Foreground(gotui.Text).Bold(true),
		cursor: base.Foreground(gotui.Accent).Bold(true),
		rowOn:  base.Foreground(gotui.Text).Bold(true),
		rowOff: base.Foreground(gotui.Muted),
		rowKey: base.Foreground(gotui.Faint),

		check: base.Foreground(gotui.Zoom).Bold(true),
		done:  base.Foreground(gotui.Faint),
		body:  base.Foreground(gotui.Text),
		faint: base.Foreground(gotui.Faint),
		warn:  base.Foreground(gotui.Accent),

		// The same palette the live reporter writes as raw sequences, so
		// `doti install` and the window's Install do not merely agree about
		// what happened, they agree about how it looked. MarkNone is the one
		// that differs, and only because its glyph is a space: the live
		// reporter leaves it uncoloured where this gives it the body's Muted.
		marks: map[app.Mark]lipgloss.Style{
			app.MarkNone:   base.Foreground(gotui.Muted),
			app.MarkOK:     base.Foreground(gotui.Faint),
			app.MarkChange: base.Foreground(gotui.Zoom),
			app.MarkSkip:   base.Foreground(gotui.Faint),
			app.MarkWarn:   base.Foreground(gotui.Accent),
		},
		phase: base.Foreground(gotui.Text).Bold(true),
	}
}

// pad is n columns of black, and the only way to write a gap.
func (s styles) pad(n int) string { return s.chrome.Pad(n) }

func (s styles) ends(width int, left, right string) string {
	return s.chrome.Ends(width, left, right)
}

// mark is the style for one report mark, defaulting to plain body text.
func (s styles) mark(m app.Mark) lipgloss.Style {
	if style, ok := s.marks[m]; ok {
		return style
	}
	return s.body
}
