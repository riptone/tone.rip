package gotui

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// A spec in the middle of the two the apps use, so neither app's numbers are
// what these assertions are really about.
var testSpec = Spec{
	WidthMax: 70, HeightMax: 30, PadX: 2, Gutter: 2,
	WidthMin: 24, HeightMin: 8, MinBody: 4, CompactBelow: 10,
}

func testChrome() Chrome { return NewChrome(OfflineRenderer(io.Discard), testSpec) }

// ---------------------------------------------------------------- geometry --

func TestGeometryTakesMarginFirstThenItsOwnWidth(t *testing.T) {
	// Plenty of room: the ceiling applies, and there is margin either side.
	g := testSpec.For(200, 60)
	if g.Width != testSpec.WidthMax {
		t.Errorf("width = %d, want the ceiling %d", g.Width, testSpec.WidthMax)
	}
	if g.Height != testSpec.HeightMax {
		t.Errorf("height = %d, want the ceiling %d", g.Height, testSpec.HeightMax)
	}

	// Enough for the card but not for the margin: the card gives up margin.
	g = testSpec.For(72, 60)
	if g.Width != 66 {
		t.Errorf("at 72 columns width = %d, want 66 - three either side", g.Width)
	}

	// Not enough for either: the card takes the whole terminal, and stays a card.
	g = testSpec.For(20, 6)
	if g.Width > 20 || g.Height > 6 {
		t.Errorf("card %dx%d does not fit a 20x6 terminal", g.Width, g.Height)
	}
}

// A terminal that reports no size is not worth guessing about; the assumption
// is the one every terminal claims to be.
func TestGeometryAssumes80x24WhenToldNothing(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {-1, -1}, {0, 24}, {80, 0}} {
		g := testSpec.For(size[0], size[1])
		if g.TermWidth < 1 || g.TermHeight < 1 {
			t.Errorf("For(%d, %d) resolved to %dx%d", size[0], size[1], g.TermWidth, g.TermHeight)
		}
	}
	g := testSpec.For(0, 0)
	if g.TermWidth != 80 || g.TermHeight != 24 {
		t.Errorf("For(0, 0) = %dx%d, want 80x24", g.TermWidth, g.TermHeight)
	}
}

func TestGeometryReservesTheGutterOutOfTheTextWidth(t *testing.T) {
	g := testSpec.For(200, 60)
	if g.Text != g.Inner-testSpec.Gutter {
		t.Errorf("text = %d, inner = %d, gutter = %d", g.Text, g.Inner, testSpec.Gutter)
	}
	// And a spec with no scrollbar spends nothing on one.
	flat := Spec{WidthMax: 70, HeightMax: 30, PadX: 2, WidthMin: 24, HeightMin: 8, MinBody: 4}
	if fg := flat.For(200, 60); fg.Text != fg.Inner {
		t.Errorf("with no gutter text = %d, inner = %d", fg.Text, fg.Inner)
	}
}

// In a short terminal the frame is already most of the screen, and a row of
// content is worth more than a row of air.
func TestTheSpacersComeOutOfAShortCard(t *testing.T) {
	if g := testSpec.For(80, 60); !g.Spaced {
		t.Error("a tall card should keep its spacers")
	}
	if g := testSpec.For(80, 14); g.Spaced {
		t.Error("a short card should drop its spacers")
	}
	// Dropping them buys rows, which is the whole point.
	tall, short := testSpec.For(80, 60), testSpec.For(80, 14)
	if short.Height-short.Body >= tall.Height-tall.Body {
		t.Errorf("compact spends %d rows on chrome, spaced spends %d",
			short.Height-short.Body, tall.Height-tall.Body)
	}
}

// A window is the size of its contents; one that stays 30 rows tall to show
// seven is a full-screen app pretending to be a window.
func TestFitShrinksToContentButNotBelowTheFloor(t *testing.T) {
	g := testSpec.For(120, 60)
	full := g.Body

	fitted := g.Fit(7, testSpec.MinBody)
	if fitted.Body != 7 {
		t.Errorf("Fit(7) gave %d body rows", fitted.Body)
	}
	if fitted.Height != g.Height-(full-7) {
		t.Errorf("the card kept height %d while its body shrank to %d", fitted.Height, fitted.Body)
	}

	// A window with two lines in it reads as an error message.
	if tiny := g.Fit(1, testSpec.MinBody); tiny.Body != testSpec.MinBody {
		t.Errorf("Fit(1) gave %d body rows, want the floor %d", tiny.Body, testSpec.MinBody)
	}

	// More content than the terminal allows keeps the full height and scrolls.
	if big := g.Fit(500, testSpec.MinBody); big.Body != full {
		t.Errorf("Fit(500) gave %d body rows, want the full %d", big.Body, full)
	}
}

// ------------------------------------------------------------------- render --

func TestRenderFillsTheTerminalExactly(t *testing.T) {
	c := testChrome()
	for _, size := range [][2]int{{80, 24}, {200, 60}, {40, 12}, {24, 8}} {
		g := c.Geometry(size[0], size[1])
		view := c.Render(g, Pane{
			Name:   "test",
			Rows:   []string{c.Base.Foreground(Text).Render("content")},
			Hints:  []Hint{{Text: "q quit", Keep: 1}},
			Status: "ok",
		})
		lines := strings.Split(view, "\n")
		if len(lines) != size[1] {
			t.Errorf("%dx%d rendered %d rows", size[0], size[1], len(lines))
		}
		for i, line := range lines {
			if got := ansi.StringWidth(line); got != size[0] {
				t.Errorf("%dx%d row %d is %d columns", size[0], size[1], i, got)
			}
		}
	}
}

// More rows than the body has is the card growing past its own frame, which is
// the most visible way a TUI can look broken.
func TestRenderCutsRowsItHasNoRoomFor(t *testing.T) {
	c := testChrome()
	g := c.Geometry(80, 24)
	rows := make([]string, g.Body+20)
	for i := range rows {
		rows[i] = c.Base.Foreground(Text).Render("row")
	}
	view := c.Render(g, Pane{Name: "test", Rows: rows})
	if got := len(strings.Split(view, "\n")); got != 24 {
		t.Errorf("rendered %d rows into a 24-row terminal", got)
	}
}

func TestTitleBarKeepsTheButtonsAndTruncatesTheName(t *testing.T) {
	c := testChrome()
	g := c.Geometry(40, 20)
	bar := ansi.Strip(c.TitleBar(g, strings.Repeat("a very long window name ", 5)))
	if !strings.Contains(bar, "●") {
		t.Error("the buttons were dropped to fit the name")
	}
	if !strings.Contains(bar, "…") {
		t.Errorf("the name was not truncated: %q", bar)
	}
	if got := ansi.StringWidth(c.TitleBar(g, "short")); got != g.Inner {
		t.Errorf("the title bar is %d columns, want %d", got, g.Inner)
	}
}

func TestLineCount(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"", 1}, {"one", 1}, {"one\ntwo", 2}, {"one\ntwo\n", 3},
	} {
		if got := LineCount(tc.in); got != tc.want {
			t.Errorf("LineCount(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// The backstop under the arithmetic, and it truncates with ansi so a cut never
// lands in the middle of an escape sequence.
func TestClamp(t *testing.T) {
	c := testChrome()
	styled := c.Base.Foreground(Accent).Render("abcdefghij")

	got := Clamp(styled+"\n"+styled+"\n"+styled, 5, 2)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("clamped to %d rows, want 2", len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != 5 {
			t.Errorf("row %d is %d columns, want 5", i, w)
		}
	}
	// The colour survived, which is what truncating with ansi buys.
	if !strings.Contains(lines[0], "\x1b[") {
		t.Errorf("the cut stripped the styling: %q", lines[0])
	}
	if Clamp("x", 0, 5) != "" || Clamp("x", 5, 0) != "" {
		t.Error("a zero dimension should clamp to nothing")
	}
}

func TestFillPadsShortRowsAndLeavesLongOnesAlone(t *testing.T) {
	s := NewSurface(OfflineRenderer(io.Discard))
	row := s.Base.Foreground(Text).Render("abc")
	if got := lipgloss.Width(s.Fill(row, 10)); got != 10 {
		t.Errorf("filled to %d columns, want 10", got)
	}
	if got := s.Fill(row, 2); got != row {
		t.Errorf("Fill shortened a row: %q", got)
	}
	if got := Unpainted(s.Fill(row, 10)); got != 0 {
		t.Errorf("the padding is unpainted in %d cells", got)
	}
}

// ---------------------------------------------------------------- scrollbar --

// One that is always there tells you nothing; one that appears only when there
// is more to read is the whole signal.
func TestScrollbarOnlyAppearsWhenThereIsMoreToRead(t *testing.T) {
	c := testChrome()
	for _, glyph := range c.Scrollbar(10, 8, 0) {
		if strings.Contains(glyph, "│") || strings.Contains(glyph, "┃") {
			t.Fatal("content that fits should have no scrollbar at all")
		}
	}

	top := c.Scrollbar(10, 40, 0)
	if len(top) != 10 {
		t.Fatalf("Scrollbar returned %d glyphs for 10 rows", len(top))
	}
	for i, glyph := range top {
		if !strings.Contains(glyph, "│") && !strings.Contains(glyph, "┃") {
			t.Errorf("row %d of a long document has no scrollbar", i)
		}
	}
	if !strings.Contains(top[0], "┃") {
		t.Error("at the top the thumb should be at the top")
	}
	if bottom := c.Scrollbar(10, 40, 30); !strings.Contains(bottom[9], "┃") {
		t.Error("at the bottom the thumb should be at the bottom")
	}
}

func TestScrollbarSurvivesDegenerateSizes(t *testing.T) {
	c := testChrome()
	if got := c.Scrollbar(0, 40, 0); len(got) != 0 {
		t.Errorf("zero rows gave %d glyphs", len(got))
	}
	if got := c.Scrollbar(-3, 40, 0); len(got) != 0 {
		t.Errorf("negative rows gave %d glyphs", len(got))
	}
	// An offset past the end must not index outside the bar.
	for _, glyph := range c.Scrollbar(5, 10, 1000) {
		if glyph == "" {
			t.Error("an out-of-range offset produced an empty glyph")
		}
	}
}

// The padding is the point: a viewport pads short lines with plain spaces that
// carry no background, and those read as a ragged hole down the right.
func TestBodyRowsPadsAndHangsTheScrollbar(t *testing.T) {
	c := testChrome()
	g := c.Geometry(80, 24)
	view := strings.Join([]string{
		c.Base.Foreground(Text).Render("first"),
		"",
		c.Base.Foreground(Text).Render("third"),
	}, "\n")

	rows := c.BodyRows(g, view, 0, 100)
	if len(rows) != g.Body {
		t.Fatalf("BodyRows returned %d rows, want the body's %d", len(rows), g.Body)
	}
	for i, row := range rows {
		if got := lipgloss.Width(row); got != g.Inner {
			t.Errorf("row %d is %d columns, want %d", i, got, g.Inner)
		}
		if n := Unpainted(row); n > 0 {
			t.Errorf("row %d leaves %d cells unpainted:\n%q", i, n, row)
		}
	}
	// A blank line still gets its scrollbar glyph.
	if !strings.Contains(rows[1], "│") && !strings.Contains(rows[1], "┃") {
		t.Errorf("the blank row lost its scrollbar: %q", rows[1])
	}
}

// ------------------------------------------------------------------- footer --

// Dropped by rank rather than by position: a reader who cannot page around is
// inconvenienced, and one who cannot see how to leave is trapped.
func TestFooterDropsByRankNotByPosition(t *testing.T) {
	c := testChrome()
	hints := []Hint{
		{Text: "↑/↓ scroll", Keep: 0},
		{Text: "←/→ section", Keep: 3},
		{Text: "esc back", Keep: 2},
		{Text: "q quit", Keep: 1},
	}

	wide := ansi.Strip(c.FooterRow(c.Geometry(200, 60), hints, "3 of 9"))
	for _, want := range []string{"scroll", "section", "back", "quit", "3 of 9"} {
		if !strings.Contains(wide, want) {
			t.Errorf("a wide footer dropped %q: %q", want, wide)
		}
	}

	narrow := ansi.Strip(c.FooterRow(c.Geometry(46, 20), hints, "3 of 9"))
	if !strings.Contains(narrow, "quit") {
		t.Errorf("the way out was dropped: %q", narrow)
	}
	if strings.Contains(narrow, "section") {
		t.Errorf("the lowest-ranked hint survived: %q", narrow)
	}
}

// It outlives a low-ranked hint and is outlived by everything that says which
// key to press.
func TestTheStatusIsDroppedAtItsOwnRank(t *testing.T) {
	c := testChrome()
	hints := []Hint{
		{Text: "q quit", Keep: 1},
		{Text: "l lang", Keep: 5},
	}
	got := ansi.Strip(c.FooterRow(c.Geometry(34, 20), hints, "counter"))
	if strings.Contains(got, "lang") {
		t.Errorf("a hint ranked below the status survived it: %q", got)
	}
	if !strings.Contains(got, "quit") {
		t.Errorf("the way out was dropped: %q", got)
	}
}

// What it must never do is show the arithmetic and no way out.
func TestAnImpossiblyNarrowFooterStillSaysHowToLeave(t *testing.T) {
	c := testChrome()
	got := ansi.Strip(c.FooterRow(c.Geometry(24, 8), []Hint{
		{Text: "↑/↓ move", Keep: 3},
		{Text: "q quit", Keep: 1},
	}, "999 of 999"))
	if !strings.Contains(got, "quit") {
		t.Errorf("no way out in %q", got)
	}
}

func TestFooterWithNothingToSayIsEmpty(t *testing.T) {
	c := testChrome()
	if got := ansi.Strip(c.FooterRow(c.Geometry(80, 24), nil, "")); strings.TrimSpace(got) != "" {
		t.Errorf("an empty footer rendered %q", got)
	}
}

// --------------------------------------------------------------------- keys --

// One definition of "down", so a program adds bindings beside these rather than
// redefining them. Asserted as key lists rather than through key.Matches,
// which would pull bubbletea into this package for a test - the fact worth
// pinning is which keys are bound, and bubbles owns the matching.
func TestNavIsTheSharedVocabulary(t *testing.T) {
	nav := NewNav()
	for _, tc := range []struct {
		name string
		got  []string
		want []string
	}{
		{"up", nav.Up.Keys(), []string{"up", "k"}},
		{"down", nav.Down.Keys(), []string{"down", "j"}},
		{"page up", nav.PageUp.Keys(), []string{"pgup", "b"}},
		{"page down", nav.PageDown.Keys(), []string{"pgdown", " ", "f"}},
		{"top", nav.Top.Keys(), []string{"home", "g"}},
		{"bottom", nav.Bottom.Keys(), []string{"end", "G"}},
		{"open", nav.Open.Keys(), []string{"enter"}},
		{"back", nav.Back.Keys(), []string{"esc", "backspace"}},
		{"quit", nav.Quit.Keys(), []string{"q", "ctrl+c"}},
	} {
		if strings.Join(tc.got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s is bound to %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// Every binding carries its aliases, which is the whole reason for using
// bubbles/key over a switch on msg.String(): the vim key and the arrow are one
// fact in one place, so a program cannot gain one and miss the other.
func TestEveryNavBindingHasItsAliases(t *testing.T) {
	nav := NewNav()
	for name, binding := range map[string][]string{
		"up": nav.Up.Keys(), "down": nav.Down.Keys(),
		"top": nav.Top.Keys(), "bottom": nav.Bottom.Keys(),
	} {
		if len(binding) < 2 {
			t.Errorf("%s has only %v - the vim alias is the point", name, binding)
		}
	}
}

// The floor is what a local terminal gets too, and TERM under-reports there for
// its own reasons - a multiplexer claiming `screen` is the common one.
func TestLocalRendererAppliesTheFloor(t *testing.T) {
	t.Setenv("TERM", "screen")
	// os.Stdout under `go test` is not a terminal, so detection alone resolves
	// to Ascii and everything would be stripped. The floor is what saves it.
	if got := LocalRenderer(os.Stdout).ColorProfile(); got != termenv.ANSI256 {
		t.Errorf("profile = %v, want ANSI256 - the floor did not apply", got)
	}

	t.Setenv("TERM", "dumb")
	if got := LocalRenderer(os.Stdout).ColorProfile(); got != termenv.Ascii {
		t.Errorf("profile = %v for a dumb terminal, want Ascii", got)
	}
}
