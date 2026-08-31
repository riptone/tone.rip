package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/packages/gotui"
)

// The palette, and the rules that make it survive the trip to a terminal that
// is not this one.
//
// The values and the primitives that depend on them live in
// packages/gotui, shared with apps/doti, which draws the same card. The
// reasoning below is why they are what they are; the tests that hold them
// to it live beside them.
//
// **Everything is painted, and everything is black.** Every cell the session
// occupies carries an explicit `#000000` background - the card, the space
// around it, the border cells. That is a deliberate trade: it costs the
// reader's terminal transparency, because a translucent terminal blends what
// is behind the window with its *default* background and an explicit `48;…` is
// an opaque rectangle over the top of that. What it buys is black on every
// terminal, with no dependence on the terminal agreeing to anything. main.go
// additionally asks the emulator to make its own default colours black and
// white for the session (OSC 11 and 10), which is the only way the terminal's
// own chrome - its tab bar, its status line - goes black with us; where that is
// ignored, the painting above still holds inside the session.
//
// **Nothing resolves against the reader's terminal.** These were
// lipgloss.AdaptiveColor pairs, which pick a value from the *detected*
// background, so the same CV came out in different colours for different people
// and in no colours at all when the detection failed.
//
// **Every style carries the background.** An inner style's reset ends the
// background an outer one started - lipgloss cannot know the outer style
// wanted it back - so a nested foreground punches a hole in the black for the
// rest of that line. Deriving every style from one base that sets the
// background, and running every run of spaces through pad, is what keeps it
// solid. window_test's "no holes" check is what notices when a stray
// strings.Repeat(" ", n) goes in.
//
// **The hex has to survive quantisation.** ssh forwards TERM and little else,
// so most sessions resolve to 256 colours rather than truecolor, and termenv's
// quantiser is coarse: every grey below #303030 collapses onto one index, and a
// dark grey with any tint in it can land on a *cube* colour instead - the
// border was once #2a2b31 and arrived as navy. theme_test asserts both.
var (
	colBlack = gotui.Black

	colText  = gotui.Text  // content, headings, the cursor row
	colMuted = gotui.Muted // rows not under the cursor
	colFaint = gotui.Faint // dates, key hints, asides
	colRule  = gotui.Rule  // the border and the rules

	// The scrollbar's two parts are the rule and the text colours by another
	// name. They keep their own identifiers because the scrollbar is this
	// program's alone - apps/doti has no document to scroll - and naming
	// them for their job is what makes the palette test's "these two must
	// stay distinguishable" pairs readable.
	colTrack = gotui.Rule
	colBar   = gotui.Text

	// The cursor, and the only place the site's signature accent appears.
	colAccent = gotui.Accent

	colClose    = gotui.Close
	colMinimise = gotui.Minimise
	colZoom     = gotui.Zoom
)

// styles is the whole vocabulary. Anything that renders text uses one of
// these; nothing builds a style inline, which is what keeps nine pages looking
// like one document.
type styles struct {
	// chrome is the shared surface: the black base, and the primitives
	// that must not be reimplemented (pad, buttons, ends, centre).
	chrome gotui.Surface

	// Window chrome.
	card    lipgloss.Style
	surface lipgloss.Style
	button  lipgloss.Style
	winName lipgloss.Style
	rule    lipgloss.Style
	track   lipgloss.Style
	bar     lipgloss.Style
	footer  lipgloss.Style

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
		track:   base.Foreground(colTrack),
		bar:     base.Foreground(colBar),
		footer:  base.Foreground(colFaint),

		group:  base.Foreground(colText).Bold(true),
		cursor: base.Foreground(colAccent).Bold(true),
		rowOn:  base.Foreground(colText).Bold(true),
		rowOff: base.Foreground(colMuted),
		rowKey: base.Foreground(colFaint),

		title:   base.Foreground(colText).Bold(true),
		meta:    base.Foreground(colFaint),
		heading: base.Foreground(colText).Bold(true).Underline(true),
		term:    base.Foreground(colText).Bold(true),
		body:    base.Foreground(colText),
		faint:   base.Foreground(colFaint),
	}
}

// pad is n columns of black, and the only way to write a gap.
func (s styles) pad(n int) string { return s.chrome.Pad(n) }

// buttons renders the three window buttons.
func (s styles) buttons() string { return s.chrome.Buttons() }
