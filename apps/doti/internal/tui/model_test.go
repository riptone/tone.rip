package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/packages/gotui"
)

func components() []app.Component {
	return []app.Component{
		{Group: "Packages", Label: "brew packages", Status: "11 of 16 present", Selected: true},
		{Group: "Configs", Label: "zsh", Status: "linked", Done: true, Selected: true},
		{Group: "Configs", Label: "ghostty", Status: "not linked", Selected: true},
		{Group: "Secrets", Label: "mssql-envs", Status: "not rendered", Selected: true},
	}
}

// noWork is a Run that is never actually invoked: the tests below feed the
// window their own events, which is what lets the whole run screen be asserted
// without a machine to install onto.
func noWork(context.Context, Action, []string, RunOptions) error { return nil }

func model() Model {
	return New(Config{
		Components: components(),
		Version:    "v1.0.0",
		Width:      80,
		Height:     26,
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Run:        noWork,
	})
}

// tap is press under a shorter name; `key` is the bubbles package.
func tap(m Model, keys ...string) Model { return press(m, keys...) }

func plain(m Model) string { return ansi.Strip(m.View()) }

// ---------------------------------------------------------------- the menu --

// Every entry has to be on screen. The first version cropped the last one,
// because the row count handed to the geometry was one short of the rows
// actually built - and a menu that silently loses its final option is worse
// than one that does not fit at all.
func TestTheWholeMenuFits(t *testing.T) {
	body := plain(model())
	for _, entry := range menu {
		if !strings.Contains(body, entry.label) {
			t.Errorf("%q is not on screen:\n%s", entry.label, body)
		}
	}
}

func TestTheCardIsDrawnWithItsChrome(t *testing.T) {
	body := plain(model())
	for _, want := range []string{"╭", "╯", "●", "doti · v1.0.0", "menu"} {
		if !strings.Contains(body, want) {
			t.Errorf("the frame is missing %q:\n%s", want, body)
		}
	}
}

func TestArrowsMoveAndNumbersJump(t *testing.T) {
	m := model()
	if m.menuAt != 0 {
		t.Fatalf("menuAt = %d, want 0", m.menuAt)
	}
	if got := tap(m, "down", "down").menuAt; got != 2 {
		t.Errorf("after two downs menuAt = %d, want 2", got)
	}
	if got := tap(m, "5").menuAt; got != 4 {
		t.Errorf("after 5 menuAt = %d, want 4", got)
	}
	// Never past either end.
	if got := tap(m, "up", "up").menuAt; got != 0 {
		t.Errorf("up from the top gave %d", got)
	}
	if got := tap(m, "G", "down").menuAt; got != len(menu)-1 {
		t.Errorf("down from the bottom gave %d", got)
	}
	// A digit past the end of the menu is not a jump to nowhere.
	if got := tap(m, "9").menuAt; got != 0 {
		t.Errorf("9 on a %d-entry menu moved to %d", len(menu), got)
	}
}

// What the hand-rolled key switch did not have, and could not gain without
// writing every alias out again. gotui.Nav is one definition of "down".
func TestVimAndHomeEndKeysWork(t *testing.T) {
	m := model()
	if got := tap(m, "j", "j").menuAt; got != 2 {
		t.Errorf("j is not down: menuAt = %d", got)
	}
	if got := tap(m, "j", "j", "k").menuAt; got != 1 {
		t.Errorf("k is not up: menuAt = %d", got)
	}
	if got := tap(m, "G").menuAt; got != len(menu)-1 {
		t.Errorf("G is not the end: menuAt = %d", got)
	}
	if got := tap(m, "G", "g").menuAt; got != 0 {
		t.Errorf("g is not the top: menuAt = %d", got)
	}
}

// -------------------------------------------------------------- the selector --

// Install and Adopt ask what to include. The rest act on everything, so a
// selector would be a keypress that never changes the outcome.
func TestOnlyInstallAndAdoptOpenTheSelector(t *testing.T) {
	for i, entry := range menu {
		m := model()
		m.menuAt = i
		next := tap(m, "enter")
		if entry.selects {
			if next.screen != ScreenSelect {
				t.Errorf("%s should open the selector, went to screen %v", entry.label, next.screen)
			}
			continue
		}
		if next.screen != ScreenRun {
			t.Errorf("%s should run straight away, went to screen %v", entry.label, next.screen)
		}
	}
}

func TestTheSelectorShowsEveryItemUnderItsGroup(t *testing.T) {
	body := plain(tap(model(), "enter"))
	for _, want := range []string{"Packages", "Configs", "Secrets",
		"brew packages", "zsh", "ghostty", "mssql-envs"} {
		if !strings.Contains(body, want) {
			t.Errorf("the selector is missing %q:\n%s", want, body)
		}
	}
}

func TestSpaceTogglesAndTheCountFollows(t *testing.T) {
	m := tap(model(), "enter")
	if !strings.Contains(plain(m), "4 of 4") {
		t.Fatalf("everything should start ticked:\n%s", plain(m))
	}
	m = tap(m, " ")
	if !strings.Contains(plain(m), "3 of 4") {
		t.Errorf("space did not untick:\n%s", plain(m))
	}
	if got := len(m.Chosen()); got != 3 {
		t.Errorf("Chosen() = %d labels, want 3", got)
	}
}

func TestAllAndNone(t *testing.T) {
	m := tap(model(), "enter", "n")
	if got := len(m.Chosen()); got != 0 {
		t.Errorf("after n, Chosen() = %d, want 0", got)
	}
	if got := len(tap(m, "a").Chosen()); got != 4 {
		t.Errorf("after a, Chosen() = %d, want 4", got)
	}
}

// Bubble Tea keeps earlier copies of the model, and a slice header copied into
// one of them shares its backing array. Toggling in place mutated the past.
func TestTogglingDoesNotReachBackwards(t *testing.T) {
	before := tap(model(), "enter")
	after := tap(before, " ")
	if len(before.Chosen()) == len(after.Chosen()) {
		t.Fatal("the toggle did nothing, so this proves nothing")
	}
	if got := len(before.Chosen()); got != 4 {
		t.Errorf("the earlier model now has %d ticked; it had 4", got)
	}
}

func TestEscapeGoesBackToTheMenu(t *testing.T) {
	m := tap(model(), "enter")
	if m.screen != ScreenSelect {
		t.Fatal("not on the selector")
	}
	if got := tap(m, "esc").screen; got != ScreenMenu {
		t.Errorf("esc left us on screen %v, want the menu", got)
	}
}

// ------------------------------------------------------------------ layout --

func TestNarrowTerminalsStillGetACard(t *testing.T) {
	for _, size := range []struct{ w, h int }{{30, 12}, {40, 14}, {200, 60}} {
		m := model()
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		body := next.(Model).View()
		if !strings.Contains(body, "╭") {
			t.Errorf("%dx%d lost the frame", size.w, size.h)
		}
		for i, line := range strings.Split(body, "\n") {
			if got := lipgloss.Width(line); got > size.w {
				t.Errorf("%dx%d line %d is %d columns wide", size.w, size.h, i, got)
			}
		}
	}
}

// The frames used for documentation are rendered from the real model, so this
// also proves the capture path keeps working - including the run screen, which
// the old menu could not draw because it had quit by that point.
func TestFramesRenderWithColour(t *testing.T) {
	frames := Frames(components(), "v1.0.0", 80, 26)
	if len(frames) < 6 {
		t.Fatalf("captured %d frames, want the menu, the offer, the selector and a run", len(frames))
	}
	names := map[string]bool{}
	for _, frame := range frames {
		names[frame.Name] = true
		if !strings.Contains(frame.Body, "\x1b[") {
			t.Errorf("frame %q has no colour in it", frame.Name)
		}
	}
	for _, want := range []string{"menu", "menu-update", "select", "run", "run-done"} {
		if !names[want] {
			t.Errorf("no frame named %q", want)
		}
	}
}

// ----------------------------------------------------------------- painting --

// Every cell the window occupies is black, including the space around the card.
// apps/ssh-cv has had this check since the bug that produced it; apps/doti did
// not, and gotui.Unpainted is what lets both inherit it rather than one of them
// having it.
func TestTheWindowLeavesNoHolesInTheBlack(t *testing.T) {
	for _, size := range [][2]int{{80, 26}, {40, 14}, {120, 40}, {30, 12}} {
		m := New(Config{
			Components: components(),
			Version:    "v1.0.0",
			Width:      size[0],
			Height:     size[1],
			Renderer:   gotui.OfflineRenderer(io.Discard),
			Run:        noWork,
		})
		screens := map[string]string{
			"menu":     m.View(),
			"offer":    send(m, updateFoundMsg("v0.2.0")).View(),
			"selector": tap(m, "enter").View(),
			"run":      tap(m, "5", "enter").View(),
		}
		long := tap(m, "5", "enter")
		for i := range 60 {
			long = send(long, line(app.MarkOK, fmt.Sprintf("line %d", i)))
		}
		screens["run scrolled"] = long.View()
		screens["run done"] = send(long, finishedMsg{}, streamDoneMsg{}).View()

		for name, view := range screens {
			for i, row := range strings.Split(view, "\n") {
				if n := gotui.Unpainted(row); n > 0 {
					t.Errorf("%dx%d %s: row %d leaves %d cells unpainted\n%q",
						size[0], size[1], name, i, n, row)
				}
			}
		}
	}
}

// No row may be wider or narrower than the terminal: one column too wide wraps
// every line into the next, and a short row is a stripe of the reader's theme.
func TestEveryRowIsExactlyTheTerminalWidth(t *testing.T) {
	for _, size := range [][2]int{{80, 26}, {40, 14}, {120, 40}, {64, 20}} {
		m := New(Config{
			Components: components(),
			Version:    "v1.0.0",
			Width:      size[0],
			Height:     size[1],
			Renderer:   gotui.OfflineRenderer(io.Discard),
			Run:        noWork,
		})
		run := tap(m, "5", "enter")
		for i := range 40 {
			run = send(run, line(app.MarkOK, strings.Repeat("wide ", i%12+1)))
		}
		for name, view := range map[string]string{
			"menu":     m.View(),
			"selector": tap(m, "enter").View(),
			"run":      run.View(),
		} {
			for i, row := range strings.Split(view, "\n") {
				if got := ansi.StringWidth(row); got != size[0] {
					t.Errorf("%dx%d %s: row %d is %d columns, want %d",
						size[0], size[1], name, i, got, size[0])
				}
			}
		}
	}
}

// Bubble Tea draws once before it knows the terminal's size, and both fields
// are zero for that frame. Clamping to max(0, 1) crushed the whole card into a
// single cell - which on a real terminal is a visible flash of nothing.
func TestTheFirstFrameIsACardRatherThanOneCell(t *testing.T) {
	m := New(Config{
		Components: components(),
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Run:        noWork,
		// No Width, no Height: the state Bubble Tea starts in.
	})
	body := plain(m)
	if lines := strings.Count(body, "\n"); lines < 10 {
		t.Fatalf("the first frame is %d rows:\n%q", lines+1, body)
	}
	if !strings.Contains(body, "What would you like to do?") {
		t.Errorf("the first frame is not the menu:\n%s", body)
	}
	if got := ansi.StringWidth(strings.Split(body, "\n")[0]); got < 40 {
		t.Errorf("the first frame is %d columns wide", got)
	}
}
