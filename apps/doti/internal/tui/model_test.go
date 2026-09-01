package tui

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/packages/gotui"
)

// components is what a scan of a machine looks like: kinds, because the
// selectors that offer a subset filter on those, and parents with children,
// because a fixture of flat rows would never exercise the fold.
func components() []app.Component {
	tool := func(label, status string) app.Component {
		return app.Component{Group: "Packages", Kind: app.KindTool,
			Parent: "brew packages", Label: label, Status: status,
			Done: status == "installed", Selected: true}
	}
	return []app.Component{
		{Group: "Packages", Kind: app.KindTools, Label: "brew packages",
			Status: "1 of 2 present", Selected: true},
		tool("jq", "installed"),
		tool("fd", "missing"),
		{Group: "Packages", Kind: app.KindMcps, Label: "mcp servers",
			Status: "1 of 1 present", Done: true, Selected: true},
		{Group: "Packages", Kind: app.KindMcp, Parent: "mcp servers",
			Label: "mcp-sqlite-tools", Status: "installed", Done: true, Selected: true},
		{Group: "Configs", Kind: app.KindStow, Label: "zsh",
			Status: "linked", Done: true, Selected: true},
		{Group: "Configs", Kind: app.KindStow, Label: "ghostty",
			Status: "not linked", Selected: true},
		{Group: "Configs", Kind: app.KindGitLocal, Label: "gitconfig-local",
			Status: "written", Done: true, Selected: true},
		{Group: "Secrets", Kind: app.KindSecret, Label: "mssql-envs",
			Status: "not rendered", Selected: true},
	}
}

// leaves is how many of the fixture's components are things rather than
// summaries of things - which is what the selector counts, and what Chosen
// returns beside the parents that carry them.
func leaves() int {
	var n int
	for _, item := range components() {
		if item.Kind != app.KindTools && item.Kind != app.KindMcps {
			n++
		}
	}
	return n
}

// labelsOf is the labels in a list of refs, for a readable failure.
func labelsOf(refs []app.Ref) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Label)
	}
	return out
}

// chosenLabels is what is ticked, as a set.
func chosenLabels(m Model) map[string]bool {
	set := map[string]bool{}
	for _, ref := range m.Chosen() {
		set[ref.Label] = true
	}
	return set
}

// noWork is a Run that is never actually invoked: the tests below feed the
// window their own events, which is what lets the whole run screen be asserted
// without a machine to install onto.
func noWork(context.Context, Action, []app.Ref, RunOptions) error { return nil }

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
	// Counted from the fixture rather than written down, so adding a component
	// to it does not turn every assertion here into a puzzle. Leaves, not rows:
	// a parent is a summary of its children, and counting it as well would make
	// "3 of 2 tools" the reading for a fully ticked group.
	all := leaves()

	m := tap(model(), "enter")
	if want := fmt.Sprintf("%d of %d", all, all); !strings.Contains(plain(m), want) {
		t.Fatalf("everything should start ticked, want %q:\n%s", want, plain(m))
	}

	// The cursor starts on a folded parent, so untick a leaf instead - the last
	// one, which nothing folds away.
	m = tap(m, "G", " ")
	if want := fmt.Sprintf("%d of %d", all-1, all); !strings.Contains(plain(m), want) {
		t.Errorf("space did not untick, want %q:\n%s", want, plain(m))
	}
	// One fewer leaf, and every parent still ticked - so the refs are the
	// leaves that are left plus the parents that carry them.
	if got := len(m.Chosen()); got != len(components())-1 {
		t.Errorf("Chosen() = %v", labelsOf(m.Chosen()))
	}
}

func TestAllAndNone(t *testing.T) {
	m := tap(model(), "enter", "n")
	if got := len(m.Chosen()); got != 0 {
		t.Errorf("after n, Chosen() = %d, want 0", got)
	}
	if got, want := len(tap(m, "a").Chosen()), len(components()); got != want {
		t.Errorf("after a, Chosen() = %d, want %d", got, want)
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
	if got, want := len(before.Chosen()), len(components()); got != want {
		t.Errorf("the earlier model now has %d ticked; it had %d", got, want)
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

// esc is bound to both Back and Quit, and which one it means depends on whether
// there is anywhere to go back to. The rule apps/ssh-cv follows: esc closes a
// page from inside one, and closes the session from the index.
func TestEscGoesBackWhereItCanAndQuitsWhereItCannot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		open  func() Model
		quits bool
		want  Screen
	}{
		{"the menu has nothing behind it", func() Model { return model() }, true, ScreenMenu},
		{"the selector goes back", func() Model { return tap(model(), "enter") }, false, ScreenMenu},
		{"the help goes back", func() Model { return tap(model(), "h") }, false, ScreenMenu},
		{"a finished run goes back", func() Model {
			return send(tap(model(), "5", "enter"), finishedMsg{}, streamDoneMsg{})
		}, false, ScreenMenu},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after := tap(tc.open(), "esc")
			if after.quit != tc.quits {
				t.Errorf("quit = %v, want %v", after.quit, tc.quits)
			}
			if !tc.quits && after.screen != tc.want {
				t.Errorf("screen = %v, want %v", after.screen, tc.want)
			}
		})
	}
}

// And it must not become a way out of a running install - that is ctrl+c's job,
// which stops the work rather than the program.
func TestEscDoesNotQuitOutOfARunningOperation(t *testing.T) {
	running := tap(model(), "5", "enter")
	after := tap(running, "esc")
	if after.quit {
		t.Error("esc quit the program with an operation still running")
	}
	if after.screen != ScreenRun {
		t.Errorf("esc left a running operation for screen %v", after.screen)
	}
}

// q still quits from everywhere nothing is running, unchanged.
func TestQStillQuitsFromEveryScreen(t *testing.T) {
	for name, open := range map[string]func() Model{
		"menu":     func() Model { return model() },
		"selector": func() Model { return tap(model(), "enter") },
		"help":     func() Model { return tap(model(), "h") },
		"run":      func() Model { return send(tap(model(), "5", "enter"), finishedMsg{}, streamDoneMsg{}) },
	} {
		if !tap(open(), "q").quit {
			t.Errorf("q did not quit from the %s", name)
		}
	}
}

// The two programs agree about it, which is the point of matching.
func TestEscIsBoundToBothBackAndQuit(t *testing.T) {
	k := newKeymap()
	if !slices.Contains(k.Quit.Keys(), "esc") {
		t.Errorf("Quit is bound to %v, want esc among them", k.Quit.Keys())
	}
	if !slices.Contains(k.Back.Keys(), "esc") {
		t.Errorf("Back is bound to %v, want esc among them", k.Back.Keys())
	}
	// ctrl+c is Stop here, deliberately not Quit.
	if slices.Contains(k.Quit.Keys(), "ctrl+c") {
		t.Errorf("ctrl+c is bound to Quit as well as Stop: %v", k.Quit.Keys())
	}
	if !slices.Contains(k.Stop.Keys(), "ctrl+c") {
		t.Errorf("Stop is bound to %v", k.Stop.Keys())
	}
}
