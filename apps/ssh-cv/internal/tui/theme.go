package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The palette, and the rules that make it survive the trip to a terminal that
// is not this one.
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
	// Black. Not "near black" - the reader asked for the real thing, and a
	// #0f0f12 that reads as black beside a white page reads as grey beside a
	// terminal that is actually black.
	colBlack = lipgloss.Color("#000000") // 16

	colText  = lipgloss.Color("#ffffff") // 231 - content, headings, the cursor row
	colMuted = lipgloss.Color("#b4b4b4") // 250 - rows not under the cursor
	colFaint = lipgloss.Color("#8a8a8a") // 102 - dates, key hints, asides
	colRule  = lipgloss.Color("#3a3a3a") // 59  - the border and the rules
	colTrack = lipgloss.Color("#3a3a3a") // 59  - the scrollbar's own line
	colBar   = lipgloss.Color("#ffffff") // 231 - the part of it that moves

	// The cursor, and the only place the site's signature accent appears. One
	// thing in the session has to be findable instantly; everything else is
	// white or grey on purpose.
	colAccent = lipgloss.Color("#ff5c00") // 202

	// The window buttons. macOS keeps these three in both appearances, and so
	// does every terminal recording of it.
	colClose    = lipgloss.Color("#ff5f57") // 203
	colMinimise = lipgloss.Color("#febc2e") // 214
	colZoom     = lipgloss.Color("#28c840") // 41
)

// styles is the whole vocabulary. Anything that renders text uses one of
// these; nothing builds a style inline, which is what keeps nine pages looking
// like one document.
type styles struct {
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
	base := r.NewStyle().Background(colBlack)

	return styles{
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
//
// Raw spaces would show the terminal's own background through the window for
// the rest of that line - see the note on the palette above.
func (s styles) pad(n int) string {
	if n <= 0 {
		return ""
	}
	return s.surface.Render(strings.Repeat(" ", n))
}

// buttons renders the three window buttons. Decoration, and the only place in
// the session where colour is not doing work - a terminal window drawn inside
// a terminal window is a joke that stops being funny if it is subtle.
func (s styles) buttons() string {
	const dot = "●"
	return s.button.Foreground(colClose).Render(dot) + s.pad(1) +
		s.button.Foreground(colMinimise).Render(dot) + s.pad(1) +
		s.button.Foreground(colZoom).Render(dot)
}
