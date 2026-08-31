package tui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/packages/gotui"
)

func TestHOpensTheHelpAndEscComesBack(t *testing.T) {
	m := model()
	help := tap(m, "h")
	if help.screen != ScreenHelp {
		t.Fatalf("h left us on screen %v", help.screen)
	}
	if !strings.Contains(plain(help), "Moving around") {
		t.Errorf("the help is empty:\n%s", plain(help))
	}
	if back := tap(help, "esc"); back.screen != ScreenMenu {
		t.Errorf("esc left us on screen %v", back.screen)
	}
	// h is a toggle as well as a door.
	if back := tap(help, "h"); back.screen != ScreenMenu {
		t.Errorf("h from the help left us on screen %v", back.screen)
	}
	// And ? is the other conventional spelling.
	if tap(m, "?").screen != ScreenHelp {
		t.Error("? does not open the help")
	}
}

// A detour, not a one-way trip: it returns to whatever asked for it.
func TestHelpReturnsToWhereItWasAskedFor(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func() Model
		want Screen
	}{
		{"from the menu", func() Model { return model() }, ScreenMenu},
		{"from the selector", func() Model { return tap(model(), "enter") }, ScreenSelect},
		{"from a finished run", func() Model {
			return send(tap(model(), "5", "enter"), finishedMsg{}, streamDoneMsg{})
		}, ScreenRun},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from := tc.open()
			if from.screen != tc.want {
				t.Fatalf("the fixture is on screen %v, want %v", from.screen, tc.want)
			}
			back := tap(tap(from, "h"), "esc")
			if back.screen != tc.want {
				t.Errorf("came back to screen %v, want %v", back.screen, tc.want)
			}
		})
	}
}

// Not while something is running: h would take the log off screen mid-install,
// and the log is the reason the window exists.
func TestHelpDoesNotInterruptARunningOperation(t *testing.T) {
	running := tap(model(), "5", "enter")
	if running.settledForTest() {
		t.Fatal("the fixture is not running")
	}
	if got := tap(running, "h").screen; got != ScreenRun {
		t.Errorf("h during a run left us on screen %v", got)
	}
}

// Built from the same tables the program acts on. A help screen that describes
// a key the program no longer has is worse than no help screen.
func TestTheHelpNamesEveryOperationAndItsDescription(t *testing.T) {
	body := allOf(tap(model(), "h"))
	for _, entry := range menu {
		if !strings.Contains(body, entry.label) {
			t.Errorf("the help does not name %q", entry.label)
		}
		if !strings.Contains(body, entry.desc) {
			t.Errorf("the help does not carry %q's description", entry.label)
		}
	}
	for _, key := range []string{"ctrl+c", "space", "--term", "-n", "--tools"} {
		if !strings.Contains(body, key) {
			t.Errorf("the help does not mention %q", key)
		}
	}
}

// Prose in 58 columns is a column of fragments, which is why this screen and the
// run log get the bigger card.
func TestTheHelpAndTheRunScreenAreWiderThanTheMenu(t *testing.T) {
	const width, height = 120, 40
	m := New(Config{
		Components: components(),
		Width:      width,
		Height:     height,
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Run:        noWork,
	})

	// In columns, not bytes: a box-drawing character is three bytes and one
	// column, so len() reports every card as three times its width.
	cardWidth := func(view string) int {
		for _, line := range strings.Split(view, "\n") {
			if stripped := ansi.Strip(line); strings.Contains(stripped, "╭") {
				return ansi.StringWidth(strings.TrimSpace(stripped))
			}
		}
		return 0
	}

	menuWidth := cardWidth(m.View())
	helpWidth := cardWidth(tap(m, "h").View())
	runWidth := cardWidth(tap(m, "5", "enter").View())

	if menuWidth != spec.WidthMax {
		t.Errorf("the menu card is %d columns, want %d", menuWidth, spec.WidthMax)
	}
	for name, got := range map[string]int{"help": helpWidth, "run": runWidth} {
		if got != wideSpec.WidthMax {
			t.Errorf("the %s card is %d columns, want %d", name, got, wideSpec.WidthMax)
		}
		if got <= menuWidth {
			t.Errorf("the %s card (%d) is no wider than the menu (%d)", name, got, menuWidth)
		}
	}
}

// The wide card is still a card: Spec.For gives up margin before width and never
// exceeds the terminal.
func TestTheWideCardStillFitsANarrowTerminal(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {40, 14}, {30, 12}} {
		m := New(Config{
			Components: components(),
			Width:      size[0],
			Height:     size[1],
			Renderer:   gotui.OfflineRenderer(io.Discard),
			Run:        noWork,
		})
		for name, view := range map[string]string{
			"help": tap(m, "h").View(),
			"run":  tap(m, "5", "enter").View(),
		} {
			if !strings.Contains(view, "╭") {
				t.Errorf("%dx%d %s lost the frame", size[0], size[1], name)
			}
			for i, row := range strings.Split(view, "\n") {
				if got := ansi.StringWidth(row); got != size[0] {
					t.Errorf("%dx%d %s row %d is %d columns", size[0], size[1], name, i, got)
				}
				if n := gotui.Unpainted(row); n > 0 {
					t.Errorf("%dx%d %s row %d leaves %d cells unpainted", size[0], size[1], name, i, n)
				}
			}
		}
	}
}

// A resize is the only thing that changes the help's content, so it is the only
// thing that has to re-wrap it.
func TestAResizeRewrapsTheHelp(t *testing.T) {
	m := tap(model(), "h")
	wide := m.help.view.TotalLineCount()

	next, _ := m.Update(tea.WindowSizeMsg{Width: 46, Height: 20})
	narrow := next.(Model)
	if narrow.help.view.TotalLineCount() <= wide {
		t.Errorf("a narrower card produced %d lines, was %d - it should wrap into more",
			narrow.help.view.TotalLineCount(), wide)
	}
	if !strings.Contains(plain(narrow), "Moving around") {
		t.Errorf("the help lost its content on resize:\n%s", plain(narrow))
	}
}

// Long enough to scroll, so the scrollbar and the hint have to be there.
func TestTheHelpScrolls(t *testing.T) {
	m := tap(model(), "h")
	if m.help.view.TotalLineCount() <= m.help.view.Height {
		t.Skip("the help fits this card, so there is nothing to scroll")
	}
	if !strings.Contains(m.View(), "┃") {
		t.Error("no scrollbar on a help screen that does not fit")
	}
	if !strings.Contains(plain(m), "↑/↓ scroll") {
		t.Errorf("no scroll hint:\n%s", plain(m))
	}

	scrolled := tap(m, "G")
	if !scrolled.help.view.AtBottom() {
		t.Error("G did not reach the bottom")
	}
	if !tap(scrolled, "g").help.view.AtTop() {
		t.Error("g did not return to the top")
	}
}

// allOf is the whole help text rather than the visible page, for asserting
// content that is below the fold.
func allOf(m Model) string {
	return ansi.Strip(strings.Join(m.helpRows(m.helpGeometry().Text), "\n"))
}

// settledForTest exposes the run state to the tests in this file.
func (m Model) settledForTest() bool { return m.run.settled() }

var _ = app.MarkOK
