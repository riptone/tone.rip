package tui

import (
	"io"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/riptone/tonil/apps/ssh-cv/internal/authz"
)

var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// colourModel builds a session that really emits colour.
//
// The default renderer resolves to Ascii under `go test` for the same reason
// it does under systemd - stdout is not a terminal - which is exactly the bug
// these tests exist to catch, so the profile is forced instead of detected.
func colourModel(t *testing.T, width, height int) Model {
	t.Helper()
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.TrueColor)

	m := New(Config{
		Doc:      testDoc(t),
		Grant:    authz.Grant{Label: "laptop"},
		Width:    width,
		Height:   height,
		Renderer: r,
	})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(Model)
}

// The black as lipgloss writes it at TrueColor.
const blackSGR = "48;2;0;0;0"

// holes walks one rendered row and counts the cells painted with no background
// at all - which is to say the cells where the reader's own terminal shows
// through the black.
//
// Two failures look like this. One is a raw run of spaces: an inner style's
// reset ends the background an outer style started, so a gap written as
// strings.Repeat(" ", n) leaves the rest of that line bare, and it looks like
// stripes. The other is the space around the card, which is only black because
// centre paints it.
func holes(row string) int {
	painted := false
	count := 0

	for len(row) > 0 {
		if loc := sgr.FindStringIndex(row); loc != nil && loc[0] == 0 {
			code := row[loc[0]:loc[1]]
			switch {
			case strings.Contains(code, blackSGR):
				painted = true
			case code == "\x1b[0m" || code == "\x1b[m":
				painted = false
			}
			row = row[loc[1]:]
			continue
		}

		_, size := decodeRune(row)
		row = row[size:]
		if !painted {
			count++
		}
	}
	return count
}

func decodeRune(s string) (rune, int) {
	for i, r := range s {
		if i == 0 {
			return r, len(string(r))
		}
	}
	return 0, 1
}

// Every cell of the terminal, not only the ones the card is on: the session is
// meant to be black to the edges.
func TestTheBlackHasNoHoles(t *testing.T) {
	for _, size := range [][2]int{{100, 34}, {80, 24}, {46, 14}, {30, 9}} {
		m := colourModel(t, size[0], size[1])
		for name, view := range map[string]string{
			"index":         m.View(),
			"first role":    press(t, m, "enter").View(),
			"last page":     press(t, m, "G", "enter").View(),
			"in Portuguese": press(t, m, "l", "enter").View(),
		} {
			for i, row := range strings.Split(view, "\n") {
				if n := holes(row); n > 0 {
					t.Errorf("%dx%d %s: row %d leaves %d cells unpainted\n%q",
						size[0], size[1], name, i, n, row)
					break
				}
			}
		}
	}
}

// The card is the whole render: every row exactly as wide as the terminal, and
// exactly as many rows as it has. Anything else and the frame walks.
func TestTheRenderIsExactlyTheTerminal(t *testing.T) {
	for _, size := range [][2]int{{200, 60}, {100, 34}, {80, 24}, {46, 14}, {30, 9}, {20, 8}} {
		view := colourModel(t, size[0], size[1]).View()
		rows := strings.Split(view, "\n")
		if len(rows) != size[1] {
			t.Errorf("%dx%d rendered %d rows, want %d", size[0], size[1], len(rows), size[1])
		}
		for i, row := range rows {
			if width := lipgloss.Width(row); width != size[0] {
				t.Errorf("%dx%d row %d is %d columns wide, want the full %d",
					size[0], size[1], i, width, size[0])
				break
			}
		}
	}
}

// However small it gets, it stays a window. An earlier version took the border
// off below 40x14 and let the document fill the screen, which stopped looking
// like the same application at exactly the point it was hardest to read.
func TestTheFrameSurvivesATinyTerminal(t *testing.T) {
	for _, size := range [][2]int{{100, 34}, {46, 14}, {34, 11}, {30, 9}, {24, 8}} {
		view := colourModel(t, size[0], size[1]).View()
		for _, corner := range []string{"╭", "╮", "╰", "╯"} {
			if !strings.Contains(view, corner) {
				t.Errorf("%dx%d lost the window (no %q)", size[0], size[1], corner)
			}
		}
	}
}

// Growing the terminal after scrolling must not leave a hole.
//
// The viewport keeps its offset across a resize, so a page scrolled to the
// bottom of a short window renders from that offset into a tall one - the text
// ends halfway up the card and the rest is blank, until some keypress happens
// to clamp the offset. This is that bug, and it was reported from a real
// session.
func TestGrowingTheTerminalDoesNotLeaveAGap(t *testing.T) {
	m := press(t, newTestModel(t, 90, 16, authz.Grant{}), "enter", "G")
	if !m.scrollable() {
		t.Skip("the first page fits in a 16-row terminal")
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 44})
	grown := updated.(Model)

	if maxOffset := max(grown.view.TotalLineCount()-grown.view.Height, 0); grown.view.YOffset > maxOffset {
		t.Errorf("after growing, offset %d is past the last line (max %d)",
			grown.view.YOffset, maxOffset)
	}
	// And the last line of the page is on screen, rather than above a gap.
	body := stripANSI(grown.view.View())
	if strings.TrimSpace(body) == "" {
		t.Fatal("the body came back empty")
	}
	if grown.scrollable() && grown.view.YOffset+grown.view.Height < grown.view.TotalLineCount() {
		return // still scrolled mid-document, which is fine
	}
	rows := strings.Split(strings.TrimRight(body, " \n"), "\n")
	if len(rows) > 0 && strings.TrimSpace(rows[len(rows)-1]) == "" {
		t.Error("the page ends in blank rows inside the card")
	}
}

// The three buttons are the one thing in the frame that is unmistakably
// colour, and the reason the bug was visible at all: they were grey.
func TestTheWindowButtonsAreRedYellowGreen(t *testing.T) {
	view := colourModel(t, 100, 34).View()
	for name, code := range map[string]string{
		"close":    "38;2;255;95;87",
		"minimise": "38;2;254;188;46",
		"zoom":     "38;2;40;200;64",
	} {
		if !strings.Contains(view, code) {
			t.Errorf("the %s button is not painted (%s missing)", name, code)
		}
	}
}

// Nothing in the palette may resolve against the reader's terminal: two
// sessions with opposite backgrounds have to produce the same bytes, or the
// CV looks different depending on who opened it.
func TestThePaletteIgnoresTheTerminalBackground(t *testing.T) {
	render := func(dark bool) string {
		r := lipgloss.NewRenderer(io.Discard)
		r.SetColorProfile(termenv.TrueColor)
		r.SetHasDarkBackground(dark)
		m := New(Config{Doc: testDoc(t), Width: 100, Height: 34, Renderer: r})
		updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 34})
		return updated.(Model).View()
	}
	if render(true) != render(false) {
		t.Error("the CV renders differently on a light and a dark terminal")
	}
}
