package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/packages/gotui"
)

// The screen the whole change is for: an operation runs *inside* the window
// instead of the window quitting so the operation can print.

func line(mark app.Mark, text string) eventMsg {
	return eventMsg(app.Record{Kind: "line", Mark: mark, Text: text})
}

func phase(name string) eventMsg { return eventMsg(app.Record{Kind: "phase", Text: name}) }

// running is a model on the run screen, with a job, ready to be fed events.
//
// Entry 5 is Health check: the operations that open the selector need a second
// keypress, and this helper is about the run screen rather than the route to it.
func running(t *testing.T) Model {
	t.Helper()
	m := tap(model(), "5", "enter")
	if m.screen != ScreenRun {
		t.Fatalf("expected the run screen, got %v", m.screen)
	}
	return m
}

// drain runs a command and everything it batched, and returns what they
// produced.
//
// tea.Batch does not execute its children - it returns them as a message for
// the runtime to schedule - so a test that calls the command and expects the
// work to have happened is testing nothing. Run concurrently, like the runtime
// does, because the two halves are a writer and a reader of the same channel
// and neither finishes without the other.
func drain(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	// Nested, because begin batches startJob's batch with the spinner tick -
	// and a BatchMsg holds commands, not messages, so one level of unwrapping
	// runs the inner batch's constructor and nothing inside it.
	var (
		mu   sync.Mutex
		out  []tea.Msg
		wait sync.WaitGroup
	)
	for _, child := range batch {
		if child == nil {
			continue
		}
		wait.Add(1)
		go func(c tea.Cmd) {
			defer wait.Done()
			produced := c()
			if nested, ok := produced.(tea.BatchMsg); ok {
				for _, inner := range nested {
					if inner == nil {
						continue
					}
					wait.Add(1)
					go func(c tea.Cmd) {
						defer wait.Done()
						produced := c()
						mu.Lock()
						out = append(out, produced)
						mu.Unlock()
					}(inner)
				}
				return
			}
			mu.Lock()
			out = append(out, produced)
			mu.Unlock()
		}(child)
	}
	done := make(chan struct{})
	go func() { wait.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the batched commands did not finish")
	}
	return out
}

func TestChoosingAnOperationOpensTheRunScreenRatherThanQuitting(t *testing.T) {
	m := tap(model(), "5", "enter") // Health check: no selector
	if m.screen != ScreenRun {
		t.Fatalf("screen = %v, want the run screen", m.screen)
	}
	if m.quit {
		t.Error("the window quit; the whole point is that it does not")
	}
	if m.run.action != ActionCheck {
		t.Errorf("action = %q, want check", m.run.action)
	}
}

// The bug the selector had: the ticked components were collected and then
// nobody asked for them.
func TestConfirmingTheSelectorPassesTheTickedComponentsToTheOperation(t *testing.T) {
	var got []app.Ref
	var mu sync.Mutex
	cfg := Config{
		Components: components(),
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Width:      80, Height: 26,
		Run: func(_ context.Context, _ Action, chosen []app.Ref, _ RunOptions) error {
			mu.Lock()
			got = append([]app.Ref(nil), chosen...)
			mu.Unlock()
			return nil
		},
	}
	m := New(cfg)
	// Into the selector, untick the first component - the tools parent, which
	// carries its children - and confirm.
	m = tap(m, "enter", " ", "enter")
	if m.screen != ScreenRun {
		t.Fatalf("screen = %v", m.screen)
	}
	// The work runs on the command Bubble Tea would have executed; run it here.
	_, cmd := m.begin(ActionInstall, m.Chosen())
	drain(t, cmd)
	mu.Lock()
	defer mu.Unlock()

	for _, ref := range got {
		switch ref.Label {
		case "brew packages", "jq", "fd":
			t.Errorf("the tools were unticked and %q was passed anyway: %v",
				ref.Label, labelsOf(got))
		}
	}
	// And the rest still went, so this is not passing by passing nothing.
	for _, want := range []string{"zsh", "mcp servers", "mssql-envs"} {
		if !slices.ContainsFunc(got, func(r app.Ref) bool { return r.Label == want }) {
			t.Errorf("%q was dropped: %v", want, labelsOf(got))
		}
	}
}

// Every ref carries its kind, because the labels collide: `git` is both a tool
// the manifest installs and a stow package it links, and a flat list of names
// could not say which of the two a tick meant.
func TestTheChosenRefsCarryTheirKind(t *testing.T) {
	m := tap(model(), "enter")
	for _, ref := range m.Chosen() {
		if ref.Kind == "" {
			t.Errorf("%q arrived unqualified: %+v", ref.Label, ref)
		}
	}
}

func TestReportedLinesAppearInTheWindow(t *testing.T) {
	m := send(running(t),
		phase("configs"),
		line(app.MarkChange, "zsh        linked 4"),
		line(app.MarkWarn, "ghostty: backing up ~/.config/ghostty"),
	)
	body := plain(m)
	for _, want := range []string{"configs", "zsh        linked 4", "backing up"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q is not on screen:\n%s", want, body)
		}
	}
}

// Each mark keeps the glyph the plain reporter prints, so the two renderings of
// one run do not merely agree about what happened.
func TestEachMarkKeepsItsGlyph(t *testing.T) {
	m := send(running(t),
		line(app.MarkOK, "already there"),
		line(app.MarkChange, "changed it"),
		line(app.MarkSkip, "left alone"),
		line(app.MarkWarn, "went wrong"),
	)
	body := plain(m)
	for _, mark := range []app.Mark{app.MarkOK, app.MarkChange, app.MarkSkip, app.MarkWarn} {
		if !strings.Contains(body, app.Glyph(mark)+" ") {
			t.Errorf("no %q glyph on screen:\n%s", app.Glyph(mark), body)
		}
	}
}

// A slow step is held aside rather than appended, because its result line
// replaces it - and the spinner is what says the two-minute brew is not a hang.
func TestASlowStepSpinsAndIsReplacedByItsResult(t *testing.T) {
	m := send(running(t), eventMsg(app.Record{Kind: "working", Text: "brew bundle"}))
	if m.run.working != "brew bundle" {
		t.Fatalf("working = %q", m.run.working)
	}
	if !strings.Contains(plain(m), "brew bundle") {
		t.Errorf("the step is not on screen:\n%s", plain(m))
	}
	m = send(m, line(app.MarkChange, "installed 4 packages"))
	if m.run.working != "" {
		t.Errorf("working = %q after its result line", m.run.working)
	}
	if strings.Count(plain(m), "brew bundle") != 0 {
		t.Errorf("the spinner line outlived its result:\n%s", plain(m))
	}
}

// Two command goroutines with no ordering between them. Settling on the
// operation's return alone dropped the closing lines of every run.
func TestTheScreenSettlesOnlyWhenTheWorkAndItsOutputAreBothDone(t *testing.T) {
	for _, order := range []struct {
		name  string
		first any
		last  any
	}{
		{"work returns first", finishedMsg{}, streamDoneMsg{}},
		{"the stream closes first", streamDoneMsg{}, finishedMsg{}},
	} {
		t.Run(order.name, func(t *testing.T) {
			m := send(running(t), line(app.MarkOK, "one"))
			m = send(m, order.first)
			if m.run.settled() {
				t.Fatal("settled on one of the two")
			}
			if got, _ := m.runStatus(); got == "done" {
				t.Errorf("status = %q before both landed", got)
			}
			m = send(m, order.last)
			if !m.run.settled() {
				t.Fatal("did not settle once both landed")
			}
			if got, _ := m.runStatus(); got != "done" {
				t.Errorf("status = %q, want done", got)
			}
		})
	}
}

// The "done" in the footer the reader asked for, and its opposite.
func TestAFailedRunSaysSoAndSurfacesTheError(t *testing.T) {
	boom := errors.New("brew bundle exited 1")
	m := send(running(t), finishedMsg{err: boom}, streamDoneMsg{})
	if got, _ := m.runStatus(); got != "failed" {
		t.Errorf("status = %q, want failed", got)
	}
	// So the shell hears what the screen said.
	if !errors.Is(m.Err(), boom) {
		t.Errorf("Err() = %v, want the operation's error", m.Err())
	}
}

func TestASettledRunGoesBackToTheMenu(t *testing.T) {
	m := send(running(t), finishedMsg{}, streamDoneMsg{})
	back := tap(m, "enter")
	if back.screen != ScreenMenu {
		t.Fatalf("enter left us on screen %v, want the menu", back.screen)
	}
	if back.run.active {
		t.Error("the finished run is still marked active")
	}
	// And the menu is usable again rather than a picture of one. The cursor is
	// still where running() left it - on entry 5 - which is itself worth
	// keeping: coming back from a run should not move it.
	if back.menuAt != 4 {
		t.Errorf("returning from a run moved the cursor to %d, want 4", back.menuAt)
	}
	if got := tap(back, "down").menuAt; got != 5 {
		t.Errorf("the menu stopped responding: menuAt = %d", got)
	}
}

// `doti install --tui` opened on one operation, so finishing leaves rather
// than dropping into a menu nobody asked for.
func TestALaunchedRunClosesInsteadOfShowingTheMenu(t *testing.T) {
	m := New(Config{
		Components: components(),
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Width:      80, Height: 26,
		Run:   noWork,
		Start: ActionInstall,
	})
	m = send(m, finishedMsg{}, streamDoneMsg{})
	if !m.launched {
		t.Fatal("the run was not marked as the one the window opened on")
	}
	if next := tap(m, "enter"); !next.quit {
		t.Error("enter should close a launched run, not show a menu")
	}
}

// Scrolling is the reader saying they are reading. Yanking them back to the
// bottom is the worst thing a log view can do.
func TestScrollingStopsTheTailFromChasingTheReader(t *testing.T) {
	m := running(t)
	for i := range 80 {
		m = send(m, line(app.MarkOK, fmt.Sprintf("line %d", i)))
	}
	if !m.run.follow {
		t.Fatal("a run should follow its own tail until told otherwise")
	}
	if !m.run.view.AtBottom() {
		t.Fatal("following, but not at the bottom")
	}

	m = tap(m, "up", "up", "up")
	if m.run.follow {
		t.Error("still following after the reader scrolled up")
	}
	before := m.run.view.YOffset
	m = send(m, line(app.MarkOK, "a new line while they read"))
	if m.run.view.YOffset != before {
		t.Errorf("the view moved under the reader: %d -> %d", before, m.run.view.YOffset)
	}

	// Back at the bottom, following resumes.
	m = tap(m, "G")
	if !m.run.follow {
		t.Error("returning to the bottom should resume following")
	}
}

// A long run gets a scrollbar; a short one does not. One that is always there
// tells you nothing.
func TestTheScrollbarAppearsOnlyWhenThereIsMoreToRead(t *testing.T) {
	short := send(running(t), line(app.MarkOK, "one"))
	if strings.Contains(short.View(), "┃") {
		t.Error("a run that fits should have no scrollbar")
	}
	long := running(t)
	for i := range 100 {
		long = send(long, line(app.MarkOK, fmt.Sprintf("line %d", i)))
	}
	if !strings.Contains(long.View(), "┃") {
		t.Error("a run that does not fit should have a scrollbar")
	}
}

// ctrl+c during an install stops the install. q closes the program. A single
// key that means either depending on timing is one nobody can trust.
func TestCtrlCStopsTheRunAndQuitIsRefusedWhileItRuns(t *testing.T) {
	m := running(t)
	if next := tap(m, "q"); next.quit {
		t.Error("q quit the program with an operation still running")
	}

	m = tap(m, "ctrl+c")
	if m.quit {
		t.Error("ctrl+c quit the program rather than the operation")
	}
	select {
	case <-m.run.job.done:
	default:
		t.Error("ctrl+c did not release the reporter")
	}

	// Once it has settled, q is a way out again.
	settled := send(m, finishedMsg{}, streamDoneMsg{})
	if !tap(settled, "q").quit {
		t.Error("q should quit once nothing is running")
	}
}

// Stopping twice must not panic on a closed channel: the reader can press
// ctrl+c again, and the teardown calls it as well.
func TestStoppingTwiceIsHarmless(t *testing.T) {
	m := running(t)
	m.run.job.stop()
	m.run.job.stop()
	(*job)(nil).stop()
}

func TestElapsedIsCompact(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{3 * time.Second, "(3s)"},
		{59 * time.Second, "(59s)"},
		{90 * time.Second, "(1m30s)"},
		{125 * time.Second, "(2m05s)"},
	} {
		if got := elapsed(tc.in); got != tc.want {
			t.Errorf("elapsed(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A window with nothing wired says so rather than pretending to work.
func TestAWindowWithNoOperationsSaysSo(t *testing.T) {
	m := New(Config{
		Components: components(),
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Width:      80, Height: 26,
	})
	m = tap(m, "5", "enter")
	if !strings.Contains(plain(m), "no operations are wired") {
		t.Errorf("no explanation on screen:\n%s", plain(m))
	}
	if !m.run.settled() {
		t.Error("a run that cannot start should be settled, not spinning forever")
	}
}

// ------------------------------------------------ rendering, incrementally --

// The spinner advanced in the model and the line on screen sat still, because
// nothing put the new frame into the viewport. A frozen spinner is worse than
// no spinner: it reads as the hang it exists to rule out.
func TestTheSpinnerAdvancesOnScreen(t *testing.T) {
	m := send(running(t), eventMsg(app.Record{Kind: "working", Text: "brew bundle"}))
	before := plain(m)

	// A zero-value tick is accepted by any spinner: bubbles only filters on a
	// non-zero ID or Tag.
	after := plain(send(m, spinner.TickMsg{}))
	if before == after {
		t.Errorf("the frame did not change across a tick:\n%s", before)
	}

	// And once it has settled there is nothing to animate, so the tick is
	// dropped rather than repainting the whole card eight times a second.
	settled := send(m, finishedMsg{}, streamDoneMsg{})
	if plain(settled) != plain(send(settled, spinner.TickMsg{})) {
		t.Error("a settled screen still repaints on every spinner tick")
	}
}

// The optimisation has to produce the answer the slow path would. Rendering one
// record at a time is only safe while it stays identical to rendering all of
// them.
func TestRenderingOneLineAtATimeMatchesRenderingThemAll(t *testing.T) {
	m := running(t)
	m = send(m,
		phase("packages"),
		line(app.MarkOK, "all 16 tools present"),
		line(app.MarkChange, "7 MCP servers installed"),
		phase("configs"),
		line(app.MarkWarn, "zsh: backing up ~/.zsh/plugins/zsh-shift-select/zsh-shift-select.plugin.zsh (symlink points elsewhere)"),
		line(app.MarkChange, "zsh linked 4"),
		eventMsg(app.Record{Kind: "summary", Text: "3 changed"}),
	)
	incremental := append([]string(nil), m.run.rendered...)

	full := m.rewrap().run.rendered
	if len(incremental) != len(full) {
		t.Fatalf("incremental produced %d rows, a full rewrap %d", len(incremental), len(full))
	}
	for i := range full {
		if incremental[i] != full[i] {
			t.Errorf("row %d differs:\n incremental %q\n full        %q", i, incremental[i], full[i])
		}
	}
}

// A resize is the one thing that invalidates rows wrapped at the old width.
func TestAResizeRewrapsTheLog(t *testing.T) {
	m := running(t)
	long := "zsh: backing up ~/.zsh/plugins/zsh-shift-select/zsh-shift-select.plugin.zsh"
	m = send(m, line(app.MarkWarn, long))
	wide := len(m.run.rendered)

	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	narrow := next.(Model)
	if len(narrow.run.rendered) <= wide {
		t.Errorf("a narrower card produced %d rows, was %d - it should wrap into more",
			len(narrow.run.rendered), wide)
	}
	if narrow.run.renderedFor >= m.run.renderedFor {
		t.Errorf("renderedFor did not shrink: %d then %d", m.run.renderedFor, narrow.run.renderedFor)
	}
	// And every row is still exactly the text width, so the viewport never
	// pads one itself.
	for i, row := range narrow.run.rendered {
		if got := ansi.StringWidth(row); got != narrow.run.renderedFor {
			t.Errorf("row %d is %d columns, want %d", i, got, narrow.run.renderedFor)
		}
	}
}

// The longest thing a run reports is a path, and a path is one word. Wordwrap
// leaves a word longer than the limit intact, which pushed the card's border
// out of the terminal.
func TestALongPathIsBrokenRatherThanOverflowing(t *testing.T) {
	paths := []string{
		"zsh: backing up ~/.zsh/plugins/zsh-shift-select/zsh-shift-select.plugin.zsh",
		"/Users/someone/Library/Application Support/Bitwarden CLI/data.json",
		strings.Repeat("x", 200),
	}
	for _, size := range [][2]int{{80, 26}, {40, 16}, {30, 12}} {
		m := New(Config{
			Components: components(),
			Width:      size[0],
			Height:     size[1],
			Renderer:   lipgloss.NewRenderer(io.Discard),
			Run:        noWork,
		})
		m = tap(m, "5", "enter")
		for _, path := range paths {
			m = send(m, line(app.MarkWarn, path))
		}
		for i, row := range m.run.rendered {
			if got := ansi.StringWidth(row); got != m.run.renderedFor {
				t.Errorf("%dx%d row %d is %d columns, want %d",
					size[0], size[1], i, got, m.run.renderedFor)
			}
		}
		// And the card itself still fits the terminal.
		for i, row := range strings.Split(m.View(), "\n") {
			if got := ansi.StringWidth(row); got != size[0] {
				t.Errorf("%dx%d view row %d is %d columns", size[0], size[1], i, got)
			}
		}
	}
}

// A result line replaces the spinner row rather than landing under it.
func TestAResultLineReplacesTheSpinnerRow(t *testing.T) {
	m := send(running(t), eventMsg(app.Record{Kind: "working", Text: "brew bundle"}))
	m = send(m, line(app.MarkChange, "installed 4 packages"))
	body := plain(m)
	if strings.Contains(body, "brew bundle") {
		t.Errorf("the spinner row outlived its result:\n%s", body)
	}
	if !strings.Contains(body, "installed 4 packages") {
		t.Errorf("the result is missing:\n%s", body)
	}
}

func TestTheLineCounterIsNotOneLines(t *testing.T) {
	m := send(running(t), line(app.MarkOK, "one"))
	if got, _ := m.runStatus(); got != "1 line" {
		t.Errorf("status = %q, want %q", got, "1 line")
	}
	m = send(m, line(app.MarkOK, "two"))
	if got, _ := m.runStatus(); got != "2 lines" {
		t.Errorf("status = %q, want %q", got, "2 lines")
	}
}

// The footer said "failed" and nothing said why. An operation that returns an
// error has usually not reported it as a line - the error *is* how it reports.
func TestAFailureSaysWhyOnScreen(t *testing.T) {
	m := send(running(t), finishedMsg{err: errors.New("brew bundle exited 1")}, streamDoneMsg{})
	body := plain(m)
	if !strings.Contains(body, "brew bundle exited 1") {
		t.Errorf("the reason is not on screen:\n%s", body)
	}
	if !strings.Contains(body, app.Glyph(app.MarkWarn)) {
		t.Errorf("the reason is not marked as a warning:\n%s", body)
	}
}

// A panic in the work goroutine would take the process down with the terminal
// still in the alt screen and its colours still overridden - the reader's shell
// left unusable by a nil map somewhere in a package-manager wrapper.
func TestAPanicBecomesAFailedRunRatherThanABrokenTerminal(t *testing.T) {
	j, cmd := startJob(
		func(context.Context, Action, []app.Ref, RunOptions) error {
			panic("a nil map somewhere")
		}, ActionInstall, nil)
	defer j.stop()

	var failure error
	for _, msg := range drain(t, cmd) {
		if done, ok := msg.(finishedMsg); ok {
			failure = done.err
		}
	}
	if failure == nil {
		t.Fatal("the panic did not come back as an error")
	}
	for _, want := range []string{"panicked", "a nil map somewhere", "install"} {
		if !strings.Contains(failure.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, failure)
		}
	}
	// And the stack, because a panic with no stack is a bug report nobody can act on.
	if !strings.Contains(failure.Error(), "goroutine") {
		t.Errorf("no stack in the error: %v", failure)
	}
}

// Transcript is what lands in the scrollback after the alt screen is gone.
// Records rather than rendered rows, so it is what the plain path would have
// printed rather than a screenshot of a card.
func TestTheTranscriptIsTheRunsEventsInOrder(t *testing.T) {
	m := send(running(t),
		phase("configs"),
		line(app.MarkChange, "zsh linked 4"),
		line(app.MarkWarn, "ghostty: backing up"),
		eventMsg(app.Record{Kind: "summary", Text: "1 changed"}),
	)

	got := m.Transcript()
	want := []app.Record{
		{Kind: "phase", Text: "configs"},
		{Kind: "line", Mark: app.MarkChange, Text: "zsh linked 4"},
		{Kind: "line", Mark: app.MarkWarn, Text: "ghostty: backing up"},
		{Kind: "summary", Text: "1 changed"},
	}
	if len(got) != len(want) {
		t.Fatalf("transcript has %d records, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// Browsing a menu and quitting has nothing to leave behind.
	if got := len(model().Transcript()); got != 0 {
		t.Errorf("a fresh model has a %d-record transcript", got)
	}
}

// Only a launched run leaves its output behind: quitting a menu you were
// browsing should not fill the terminal.
func TestOnlyALaunchedRunReportsItselfAsOne(t *testing.T) {
	if model().Launched() {
		t.Error("a menu reports itself as launched")
	}
	if tap(model(), "5", "enter").Launched() {
		t.Error("a run chosen from the menu reports itself as launched")
	}
	launched := New(Config{
		Components: components(),
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Width:      80, Height: 26,
		Run:   noWork,
		Start: ActionInstall,
	})
	if !launched.Launched() {
		t.Error("a window opened on one operation does not report itself as launched")
	}
}

// The title of a run the menu does not list. Reached by the footer's update
// offer, which is not a menu entry.
func TestLabelForNamesEvenTheOffMenuOperations(t *testing.T) {
	if got := labelFor(ActionSelfUpdate); got != "Self-update" {
		t.Errorf("labelFor(self-update) = %q", got)
	}
	if got := labelFor(ActionCheck); got != "Health check" {
		t.Errorf("labelFor(check) = %q", got)
	}
	// Anything else falls back to its own name rather than to an empty title.
	if got := labelFor(Action("dance")); got != "dance" {
		t.Errorf("labelFor(dance) = %q, want the name itself", got)
	}
}

// A mark with no style falls back to plain body text rather than to an empty
// style, which would drop the background and punch a hole in the line.
func TestAnUnknownMarkStillCarriesTheBackground(t *testing.T) {
	s := newStyles(gotui.OfflineRenderer(io.Discard))
	row := s.mark(app.Mark(99)).Render("x")
	if got := gotui.Unpainted(row); got != 0 {
		t.Errorf("an unknown mark left %d cells unpainted: %q", got, row)
	}
}

// "done" and "failed" in the same grey are two words that have to be told apart
// by spelling. The colour is the point.
func TestTheVerdictCarriesItsColour(t *testing.T) {
	for _, tc := range []struct {
		name   string
		settle []tea.Msg
		text   string
		colour lipgloss.TerminalColor
	}{
		{"done", []tea.Msg{finishedMsg{}, streamDoneMsg{}}, "done", gotui.Zoom},
		{"failed", []tea.Msg{finishedMsg{err: errors.New("boom")}, streamDoneMsg{}},
			"failed", gotui.Close},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := send(running(t), tc.settle...)
			text, colour := m.runStatus()
			if text != tc.text {
				t.Errorf("status = %q, want %q", text, tc.text)
			}
			if colour != tc.colour {
				t.Errorf("colour = %v, want %v", colour, tc.colour)
			}
			// And it reaches the screen, rather than being computed and
			// dropped. Through a painting renderer: the default one resolves
			// to Ascii against an io.Discard and strips every colour, which
			// would make this assertion pass for the wrong reason.
			painted := send(colourModel(), append([]tea.Msg{
				tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")},
				tea.KeyMsg{Type: tea.KeyEnter},
			}, tc.settle...)...)
			// The footer row alone, not the whole view: the three window
			// buttons are drawn in exactly these two colours, so searching the
			// frame would pass whatever the status did.
			if !strings.Contains(footerOf(painted.View()), sgrParams(tc.colour)) {
				t.Errorf("%s is not coloured in the footer:\n%q",
					tc.name, footerOf(painted.View()))
			}
		})
	}

	// While it runs there is no verdict to colour: a count is not a state.
	if _, colour := running(t).runStatus(); colour != nil {
		t.Errorf("a running screen coloured its line count %v", colour)
	}
}

// The one outcome with something left to do gets its own word, in the same
// green - it worked.
func TestAnUpdatedRunSaysUpdatedInGreen(t *testing.T) {
	m := tap(send(model(), updateFoundMsg("v0.2.0")), "u")
	m = send(m, finishedMsg{}, streamDoneMsg{})
	text, colour := m.runStatus()
	if text != "updated" || colour != gotui.Zoom {
		t.Errorf("status = %q / %v, want updated in green", text, colour)
	}
}

// colourModel is a model whose renderer actually paints, for asserting that a
// colour reaches the frame.
func colourModel() Model {
	return New(Config{
		Components: components(),
		Version:    "v1.0.0",
		Width:      100,
		Height:     30,
		Renderer:   gotui.OfflineRenderer(io.Discard),
		Run:        noWork,
	})
}

// sgrParams is a palette colour's SGR parameters without the terminator:
// lipgloss merges the foreground and the background into one sequence, so the
// "m" is not where gotui.FG puts it.
func sgrParams(colour lipgloss.TerminalColor) string {
	hex, ok := colour.(lipgloss.Color)
	if !ok {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(gotui.FG(hex), "\x1b["), "m")
}

// footerOf is the row above the card's bottom border.
//
// Needed because the window buttons are painted in the same red and green the
// verdict uses - decoration reusing the palette - so an assertion against the
// whole view cannot tell the status from a dot.
func footerOf(view string) string {
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.Contains(ansi.Strip(line), "╰") && i > 0 {
			return lines[i-1]
		}
	}
	return ""
}
