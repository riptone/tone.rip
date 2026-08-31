package gotui

import (
	"io"
	"strings"
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
	// collapse into each other are fine; anything here is load bearing.
	for _, pair := range []struct {
		what       string
		one, other lipgloss.TerminalColor
	}{
		{"a heading against a row that is not selected", Text, Muted},
		{"a row against the dates beside it", Muted, Faint},
		{"the hints against the border", Faint, Rule},
		{"the border against the body text", Rule, Text},
		{"the cursor against the row it sits on", Accent, Text},
		{"the close button against the zoom button", Close, Zoom},
		{"the close button against the minimise button", Close, Minimise},
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
// navy. Not invisible, but a blue line around a monochrome document, which is
// worse for being almost right.
func TestTheGreysArriveGrey(t *testing.T) {
	for name, c := range map[string]lipgloss.Color{
		"text": Text, "muted": Muted, "faint": Faint, "rule": Rule,
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
		"close": Close, "minimise": Minimise, "zoom": Zoom, "accent": Accent,
	} {
		rendered := r.NewStyle().Foreground(c).Render("x")
		if other, clash := seen[rendered]; clash {
			t.Errorf("%s and %s quantise to the same colour", name, other)
		}
		seen[rendered] = name
	}
}

func surface(t *testing.T) (Surface, *lipgloss.Renderer) {
	t.Helper()
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.TrueColor)
	return NewSurface(r), r
}

// The invariant the whole package exists for: a gap is painted, so the
// reader's terminal never shows through the window.
func TestPadPaintsItsGap(t *testing.T) {
	s, _ := surface(t)
	got := s.Pad(4)
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("Pad emitted no colour, so the gap is a hole: %q", got)
	}
	if !strings.Contains(got, "    ") {
		t.Fatalf("Pad = %q, want four columns", got)
	}
	if s.Pad(0) != "" || s.Pad(-3) != "" {
		t.Error("a non-positive gap should render nothing")
	}
}

func TestButtonsAreThreeDistinctDots(t *testing.T) {
	s, r := surface(t)
	rendered := s.Buttons()
	if n := strings.Count(rendered, "●"); n != 3 {
		t.Fatalf("want three dots, got %d in %q", n, rendered)
	}
	// Compared against the same style the surface builds, so this asserts
	// the actual escape sequence rather than a guess at its shape.
	for _, c := range []lipgloss.Color{Close, Minimise, Zoom} {
		dot := r.NewStyle().Background(Black).Foreground(c).Render("●")
		if !strings.Contains(rendered, dot) {
			t.Errorf("button colour %s is missing from %q", c, rendered)
		}
	}
}

func TestEndsJustifiesAndDropsTheRightWhenItCannotFit(t *testing.T) {
	s, _ := surface(t)
	wide := stripANSI(s.Ends(20, "ab", "yz"))
	if wide != "ab"+strings.Repeat(" ", 16)+"yz" {
		t.Fatalf("Ends = %q", wide)
	}
	// The left of these rows is the buttons or the cursor; losing the thing
	// that says where you are would be worse than losing a counter.
	tight := stripANSI(s.Ends(4, "abc", "xyz"))
	if tight != "abc" {
		t.Fatalf("a row too tight should keep the left, got %q", tight)
	}
	if got := stripANSI(s.Ends(10, "abc", "")); got != "abc" {
		t.Fatalf("no right means no padding, got %q", got)
	}
}

func TestCentrePaintsEveryRowIncludingTheMargin(t *testing.T) {
	s, _ := surface(t)
	// cardWidth must match the rendered card's own width; Centre trusts the
	// caller for it, as both windows pass their resolved geometry.
	out := s.Centre(20, 5, 5, "card1\ncard2")
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Fatalf("want 5 rows, got %d", len(lines))
	}
	for i, line := range lines {
		if !strings.Contains(line, "\x1b[") {
			t.Errorf("row %d is unpainted: %q", i, line)
		}
		if w := lipgloss.Width(line); w != 20 {
			t.Errorf("row %d is %d columns, want 20", i, w)
		}
	}
}

// Measured in columns, not runes: a CJK glyph is two columns wide, and
// cutting by rune count overflows the card by however many wide characters
// the string happened to contain.
func TestTruncateCountsColumnsNotRunes(t *testing.T) {
	if got := Truncate("hello", 10); got != "hello" {
		t.Fatalf("short text should pass through, got %q", got)
	}
	if got := Truncate("abcdefghij", 5); lipgloss.Width(got) > 5 {
		t.Fatalf("%q is %d columns, want <= 5", got, lipgloss.Width(got))
	}
	wide := Truncate("日本語のテキスト", 5)
	if lipgloss.Width(wide) > 5 {
		t.Fatalf("%q is %d columns, want <= 5", wide, lipgloss.Width(wide))
	}
	if Truncate("anything", 0) != "" {
		t.Error("a zero limit should render nothing")
	}
}

func stripANSI(in string) string {
	var out strings.Builder
	skip := false
	for _, r := range in {
		switch {
		case r == '\x1b':
			skip = true
		case skip && r == 'm':
			skip = false
		case !skip:
			out.WriteRune(r)
		}
	}
	return out.String()
}
