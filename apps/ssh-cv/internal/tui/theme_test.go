package tui

import (
	"io"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The palette is written in truecolor hex, and almost nobody reads it in
// truecolor: ssh forwards TERM and little else, so a session usually resolves
// to 256 colours. termenv's quantiser is coarse there - every grey below
// #303030 lands on the same index as the surface - so a colour chosen for how
// it looks here can arrive as a colour that is not there at all.
//
// This is the check that caught it: the border was a subtle #2a2b31 and came
// out identical to the background on any 256-colour client, which is to say
// the window had no edge for most of the people reading it.
func TestThePaletteSurvivesTwoFiftySixColours(t *testing.T) {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI256)
	code := func(c lipgloss.TerminalColor) string {
		return r.NewStyle().Foreground(c).Render("x")
	}

	// Pairs that carry meaning by being different. Greys that happen to
	// collapse into each other are fine - the scrollbar's moving part and the
	// body text are both white on purpose - but anything here is load bearing.
	for _, pair := range []struct {
		what       string
		one, other lipgloss.TerminalColor
	}{
		{"a heading against a row that is not selected", colText, colMuted},
		{"a row against the dates beside it", colMuted, colFaint},
		{"the hints against the border", colFaint, colRule},
		{"the border against the body text", colRule, colText},
		{"the scrollbar's line against the part that moves", colTrack, colBar},
		{"the cursor against the row it sits on", colAccent, colText},
		{"the close button against the zoom button", colClose, colZoom},
		{"the close button against the minimise button", colClose, colMinimise},
	} {
		if code(pair.one) == code(pair.other) {
			t.Errorf("at 256 colours there is no difference left in %s (both %q)",
				pair.what, code(pair.one))
		}
	}
}

// The other half of the same problem: a grey has to arrive grey.
//
// termenv quantises into the 6x6x6 cube as well as the greyscale ramp, and a
// dark grey with any tint in it can land on a cube colour instead. The border
// was #2a2b31 - a barely-cool dark grey - and came out as index 17, which is
// navy. Not invisible, but a blue line around a monochrome CV, which is worse
// for being almost right.
func TestTheGreysArriveGrey(t *testing.T) {
	for name, c := range map[string]lipgloss.Color{
		"text":  colText,
		"muted": colMuted,
		"faint": colFaint,
		"rule":  colRule,
		"track": colTrack,
		"bar":   colBar,
	} {
		rgb := termenv.ConvertToRGB(termenv.ANSI256.Color(string(c)))
		spread := max(rgb.R, max(rgb.G, rgb.B)) - min(rgb.R, min(rgb.G, rgb.B))
		// Generous: the cube's own greys are exact, and anything under a
		// tenth of the range reads as neutral. The navy this catches has a
		// spread of about 0.37.
		if spread > 0.1 {
			t.Errorf("%s (%s) quantises to r=%.2f g=%.2f b=%.2f - a tint, not a grey",
				name, c, rgb.R, rgb.G, rgb.B)
		}
	}
}

// The three buttons have to stay red, yellow and green rather than three
// shades of one hue - they are the whole visual quotation.
func TestTheButtonsQuantiseToDistinctHues(t *testing.T) {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI256)
	seen := map[string]string{}
	for name, c := range map[string]lipgloss.TerminalColor{
		"close": colClose, "minimise": colMinimise, "zoom": colZoom, "accent": colAccent,
	} {
		rendered := r.NewStyle().Foreground(c).Render("x")
		if other, clash := seen[rendered]; clash {
			t.Errorf("%s and %s quantise to the same colour", name, other)
		}
		seen[rendered] = name
	}
}
