package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/packages/gotui"
)

// The styles for this program's *content* - the index rows, a section body.
//
// The frame around them, and the palette they draw from, are gotui's: shared
// with apps/doti, which draws the same card. The reasoning for the palette
// lives there; what is worth repeating here is only the part a section body
// has to obey.
//
// **Every style carries the background.** An inner style's reset ends the
// background an outer one started - lipgloss cannot know the outer style
// wanted it back - so a nested foreground punches a hole in the black for the
// rest of that line. Deriving every style from one base that sets the
// background, and running every run of spaces through pad, is what keeps it
// solid. window_test's "no holes" check is what notices when a stray
// strings.Repeat(" ", n) goes in.

// styles is the whole vocabulary. Anything that renders text uses one of
// these; nothing builds a style inline, which is what keeps nine pages looking
// like one document.
type styles struct {
	// chrome is the frame: the black surface, the card, the rule, the
	// scrollbar, the footer, and the primitives that must not be
	// reimplemented (pad, buttons, ends, centre).
	chrome gotui.Chrome

	// The index.
	group  lipgloss.Style
	cursor lipgloss.Style
	rowOn  lipgloss.Style
	rowOff lipgloss.Style
	rowKey lipgloss.Style

	// A section body.
	title   lipgloss.Style
	meta    lipgloss.Style
	heading lipgloss.Style
	term    lipgloss.Style
	body    lipgloss.Style
	faint   lipgloss.Style
}

// newStyles builds the vocabulary against one renderer.
//
// The renderer is the reason colour works at all. lipgloss's default one is
// bound to *this process's* stdout, which for a daemon is a pipe - not a
// terminal, so the profile resolves to Ascii and every colour is silently
// stripped for every session. The renderer that matters belongs to the SSH
// session; nil here means "not a session", which is the preview and the tests.
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

		title:   base.Foreground(gotui.Text).Bold(true),
		meta:    base.Foreground(gotui.Faint),
		heading: base.Foreground(gotui.Text).Bold(true).Underline(true),
		term:    base.Foreground(gotui.Text).Bold(true),
		body:    base.Foreground(gotui.Text),
		faint:   base.Foreground(gotui.Faint),
	}
}

// pad is n columns of black, and the only way to write a gap.
func (s styles) pad(n int) string { return s.chrome.Pad(n) }
