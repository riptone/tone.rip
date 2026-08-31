// Package gotui is the terminal window apps/ssh-cv and apps/doti both draw.
//
// It owns the frame and nothing inside it. The palette, the surface, the card,
// the geometry, the scrollbar, the footer's drop order, the colour-profile
// policy and the request that makes the emulator itself go black all live here;
// each program keeps only the styles for its own content - its headings, its
// rows, its cursor. That line is what separates "the two look like one
// application" from "the two are one application".
//
//	palette.go  the colours, and the surface every style derives from
//	frame.go    the card: spec, geometry, pane, render, title bar
//	scroll.go   the scrollbar, and the body rows it hangs off
//	footer.go   key hints and a status competing for one row
//	render.go   which colours are allowed, and asking the terminal to go black
//
// It exists because both programs draw the same card - three buttons, a dim
// name, a rule, a body, a line of key hints - and the rules that make it
// survive the trip are not obvious enough to reimplement twice:
//
//   - Everything is painted, and the black is #000000. A near-black reads as
//     grey next to a terminal that is actually black. The cost is that a
//     translucent terminal cannot show through, which is a trade taken
//     deliberately: black everywhere beat transparency somewhere.
//   - Nothing resolves against the reader's terminal. Fixed hex, never
//     AdaptiveColor - which picks from the *detected* background and so gave
//     different people different colours, and gave nobody any when detection
//     failed.
//   - A raw space is a hole in the black. An inner style's reset ends the
//     background an outer one started, so `strings.Repeat(" ", n)` shows the
//     reader's terminal through the window for the rest of that line. Every
//     gap goes through Pad.
//   - The hex has to survive quantisation. ssh forwards TERM and little else,
//     so most sessions are 256 colours, and termenv's quantiser is coarse:
//     greys below #303030 collapse onto one index and a tinted dark can land
//     on a cube colour. The border was once #2a2b31 and arrived navy.
//
// The last one has tests here rather than in either app, because it is a
// property of the values and both programs inherit it.
package gotui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The palette. The trailing comment on each is the 256-colour index it
// quantises to.
var (
	// Black behind every cell. Not "near black".
	Black = lipgloss.Color("#000000") // 16

	Text  = lipgloss.Color("#ffffff") // 231 - content, headings, the row under the cursor
	Muted = lipgloss.Color("#b4b4b4") // 250 - rows that are not
	Faint = lipgloss.Color("#8a8a8a") // 102 - dates, key hints, asides
	Rule  = lipgloss.Color("#3a3a3a") // 59  - the border and the rules

	// Accent is the cursor, and the only place the site's orange appears.
	// One thing on screen has to be findable instantly.
	Accent = lipgloss.Color("#ff5c00") // 202

	// The window buttons. macOS keeps these three in both appearances.
	Close    = lipgloss.Color("#ff5f57") // 203
	Minimise = lipgloss.Color("#febc2e") // 214
	Zoom     = lipgloss.Color("#28c840") // 41
)

// Surface is a base style carrying the black background, plus the drawing
// primitives that depend on it.
//
// Every style an app builds should derive from Base, so the background is
// never lost to an inner style's reset.
type Surface struct {
	Base lipgloss.Style
}

// NewSurface builds a surface against one renderer.
//
// The renderer matters more than it looks. lipgloss's default one is bound to
// the *process's* stdout, which under systemd is a pipe and in a test is not a
// terminal - so its profile resolves to Ascii and every colour is silently
// stripped. Callers that render anywhere other than a real terminal must pass
// a renderer with an explicit profile.
func NewSurface(r *lipgloss.Renderer) Surface {
	if r == nil {
		r = lipgloss.DefaultRenderer()
	}
	return Surface{Base: r.NewStyle().Background(Black)}
}

// Pad is n columns of black, and the only way to write a gap.
func (s Surface) Pad(n int) string {
	if n <= 0 {
		return ""
	}
	return s.Base.Render(strings.Repeat(" ", n))
}

// Fill pads a rendered row out to width with black.
//
// The one rule for anything that feeds a bubbles/viewport: the viewport pads
// short lines itself, with plain spaces that carry no background, which shows
// as a ragged hole down the right of every short line. Padding first means it
// never has to.
func (s Surface) Fill(row string, width int) string {
	if pad := width - lipgloss.Width(row); pad > 0 {
		return row + s.Pad(pad)
	}
	return row
}

// Buttons renders the three window buttons.
//
// Decoration, and the only place in either program where colour is not doing
// work - a terminal window drawn inside a terminal window is a joke that
// stops being funny if it is subtle.
func (s Surface) Buttons() string {
	const dot = "●"
	return s.Base.Foreground(Close).Render(dot) + s.Pad(1) +
		s.Base.Foreground(Minimise).Render(dot) + s.Pad(1) +
		s.Base.Foreground(Zoom).Render(dot)
}

// Ends puts left at the start of a row and right at its end.
//
// When they will not both fit, right is dropped rather than wrapped or
// truncated: the left of these rows is the buttons or the cursor, and losing
// the thing that says where you are would be worse than losing a counter.
func (s Surface) Ends(width int, left, right string) string {
	if right == "" {
		return left
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + s.Pad(gap) + right
}

// Centre puts a rendered card in the middle of the terminal.
//
// Done by hand rather than with lipgloss.Place, which renders through the
// *default* renderer - the same trap NewSurface warns about, and one that
// used to leave the margin around the card unpainted for exactly the sessions
// that needed it painted.
func (s Surface) Centre(termWidth, termHeight, cardWidth int, card string) string {
	left := max((termWidth-cardWidth)/2, 0)
	right := max(termWidth-cardWidth-left, 0)
	lines := strings.Split(card, "\n")
	top := max((termHeight-len(lines))/2, 0)

	blank := s.Pad(termWidth)
	out := make([]string, 0, termHeight)
	for range top {
		out = append(out, blank)
	}
	for _, row := range lines {
		out = append(out, s.Pad(left)+row+s.Pad(right))
	}
	for len(out) < termHeight {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

// Truncate shortens text to limit columns, ending with an ellipsis.
//
// Measured in columns rather than runes: a CJK glyph is two columns wide, and
// cutting by rune count overflows the card by however many wide characters
// the string happened to contain.
func Truncate(text string, limit int) string {
	if limit < 1 {
		return ""
	}
	if lipgloss.Width(text) <= limit {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > limit {
		runes = runes[:len(runes)-1]
	}
	return strings.TrimRight(string(runes), " ") + "…"
}

// Raw SGR sequences for the one place a program writes escape codes by hand
// instead of rendering a style: a reporter that interleaves lines with cursor
// control, where a lipgloss style would end the background mid-line.
//
// Sourced from the palette above rather than written out, because they were
// written out - apps/doti's plain reporter carried "\x1b[38;2;255;92;0m" and
// two more like it, which are these values spelled differently and free to
// drift from them.
const (
	// SGRReset ends every sequence below.
	SGRReset = "\x1b[0m"
	// SGRBold is the phase headings.
	SGRBold = "\x1b[1m"
)

// FG is the raw SGR foreground sequence for one palette colour.
//
// Truecolor, like the styles: a terminal with fewer colours clamps it to the
// nearest it has, which is the same thing termenv's quantiser would do and one
// fewer thing to keep in step. An unparseable colour returns the empty string,
// so a typo is colourless rather than a stray escape in the middle of a line.
func FG(c lipgloss.Color) string {
	r, g, b, ok := rgb(string(c))
	if !ok {
		return ""
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

// rgb parses the "#rrggbb" the palette is written in.
func rgb(hex string) (r, g, b int, ok bool) {
	if len(hex) != 7 || hex[0] != '#' {
		return 0, 0, 0, false
	}
	var v [3]int
	for i := range v {
		n, err := strconv.ParseUint(hex[1+i*2:3+i*2], 16, 8)
		if err != nil {
			return 0, 0, 0, false
		}
		v[i] = int(n)
	}
	return v[0], v[1], v[2], true
}

// SpinnerFrames is what a slow step animates with, in the window and in the
// plain reporter both.
//
// Braille: one cell wide in every font that has them, so the line does not
// change width as it turns. Shared because the two renderings of the same run
// spinning differently is the kind of detail that makes two programs look like
// three.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
