package tui

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/packages/gotui"
)

// Stopping a run: what the footer says about it, what the shell hears, and
// whether the selectors know what actually got done.

// stoppableModel runs an operation that reports one line and then returns err.
func stoppableModel(t *testing.T, err error, scan ScanFunc) Model {
	t.Helper()
	return New(Config{
		Components: components(), Version: "v1.0.0", Width: 80, Height: 26,
		Renderer: lipgloss.NewRenderer(io.Discard),
		Scan:     scan,
		Run: func(context.Context, Action, []app.Ref, RunOptions) error {
			return err
		},
	})
}

// stopped drives a run to the point of a ctrl+c and then settles it.
func stopped(t *testing.T, err error, scan ScanFunc) Model {
	t.Helper()
	m := tap(stoppableModel(t, err, scan), "enter", "enter")
	if m.screen != ScreenRun {
		t.Fatalf("screen = %v", m.screen)
	}
	m = tap(m, "ctrl+c")
	if !m.run.stopped {
		t.Fatal("ctrl+c was not recorded")
	}
	return send(m, finishedMsg{err: err}, streamDoneMsg{})
}

// Whether a cancelled operation returns ctx.Canceled or nil is an accident of
// where it happened to be: a phase blocked in `brew bundle` returns the context
// error, and one between subprocesses - or one taking no context at all, like
// Link - returns nil. The footer said "failed" in red for the first and "done"
// in green for the second, and neither is what happened.
func TestAStoppedRunSaysInterruptedWhicheverWayItReturned(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"the operation noticed the cancel", context.Canceled},
		{"the operation returned nothing", nil},
		{"the operation failed for its own reasons", errors.New("brew exploded")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := stopped(t, tc.err, nil)
			text, colour := m.runStatus()
			if text != "interrupted" {
				t.Errorf("status = %q", text)
			}
			// The amber of the middle window button: red for a failure, green
			// for a finish, and the middle one for the middle case.
			if colour != gotui.Minimise {
				t.Errorf("colour = %v, want the amber %v", colour, gotui.Minimise)
			}
			if colour == gotui.Close || colour == gotui.Zoom {
				t.Error("an interrupted run is neither a failure nor a success")
			}
		})
	}
}

// The status is in the footer, in that colour, on the real screen - not just in
// the function that computes it.
func TestTheInterruptedStatusIsOnScreenInAmber(t *testing.T) {
	m := stopped(t, context.Canceled, nil)
	m.styles = newStyles(gotui.OfflineRenderer(io.Discard))
	footer := footerOf(m.View())
	if !strings.Contains(footer, "interrupted") {
		t.Errorf("the footer does not say so:\n%q", footer)
	}
	if !strings.Contains(footer, sgrParams(gotui.Minimise)) {
		t.Errorf("the status is not amber:\n%q", footer)
	}
	// Not the red the close button uses, which is painted in the same footer -
	// so a test that searched the whole frame would find it either way.
	if strings.Contains(footer, sgrParams(gotui.Zoom)) {
		t.Errorf("something in the footer is still green:\n%q", footer)
	}
}

// And the log says it, last, so the transcript that lands in the scrollback
// carries it too.
func TestAStoppedRunSaysSoInTheLog(t *testing.T) {
	records := stopped(t, context.Canceled, nil).Transcript()
	if len(records) == 0 {
		t.Fatal("no transcript")
	}
	last := records[len(records)-1]
	if !strings.Contains(last.Text, "nothing after this ran") {
		t.Errorf("the last line is %q", last.Text)
	}
	if last.Mark != app.MarkWarn {
		t.Errorf("mark = %v", last.Mark)
	}
	// Once, not once per settle message.
	var says int
	for _, record := range records {
		if strings.Contains(record.Text, "nothing after this ran") {
			says++
		}
	}
	if says != 1 {
		t.Errorf("it said so %d times", says)
	}
}

// Half an install is not a successful one, so the shell hears about it. A window
// that shows "interrupted" and exits 0 tells the same lie as one that shows a red
// line and exits 0.
func TestAStoppedRunReachesTheExitCode(t *testing.T) {
	if err := stopped(t, nil, nil).Err(); err == nil {
		t.Error("a stopped run with no error of its own exited clean")
	}
	// And the operation's own error is kept when it had one, because that is
	// the more specific answer.
	own := errors.New("brew exploded")
	if err := stopped(t, own, nil).Err(); !errors.Is(err, own) {
		t.Errorf("err = %v, want the operation's own", err)
	}
}

// A run that was not stopped is unaffected.
func TestAFinishedRunIsStillDone(t *testing.T) {
	m := send(tap(stoppableModel(t, nil, nil), "enter", "enter"),
		finishedMsg{}, streamDoneMsg{})
	if text, colour := m.runStatus(); text != "done" || colour != gotui.Zoom {
		t.Errorf("status = %q %v", text, colour)
	}
	if err := m.Err(); err != nil {
		t.Errorf("err = %v", err)
	}
	if strings.Contains(strings.Join(transcriptText(m), "\n"), "nothing after this ran") {
		t.Error("a finished run claims it was stopped")
	}
}

// ------------------------------------------------ what the selectors know --

// The question behind all of this: tick everything, let one thing install, stop
// it - do the lists then describe the machine, or the machine as it was when the
// window opened?
func TestTheSelectorsFollowAPartialRun(t *testing.T) {
	var scans atomic.Int32
	// Before: nothing linked. After the partial run: zsh linked, ghostty not.
	after := []app.Component{
		{Group: "Packages", Kind: app.KindTools, Label: "brew packages",
			Status: "1 of 2 present", Selected: true},
		{Group: "Packages", Kind: app.KindTool, Parent: "brew packages",
			Label: "jq", Status: "installed", Done: true, Selected: true},
		{Group: "Packages", Kind: app.KindTool, Parent: "brew packages",
			Label: "fd", Status: "missing", Selected: true},
		{Group: "Configs", Kind: app.KindStow, Label: "zsh",
			Status: "linked", Done: true, Selected: true},
		{Group: "Configs", Kind: app.KindStow, Label: "ghostty",
			Status: "not linked", Selected: true},
	}
	before := append([]app.Component(nil), after...)
	for i := range before {
		if before[i].Kind == app.KindStow {
			before[i].Status, before[i].Done = "not linked", false
		}
	}

	m := New(Config{
		Components: before, Version: "v1.0.0", Width: 80, Height: 30,
		Renderer: lipgloss.NewRenderer(io.Discard),
		Run:      func(context.Context, Action, []app.Ref, RunOptions) error { return nil },
		Scan: func(context.Context) (Inventory, error) {
			scans.Add(1)
			return Inventory{Components: after, Removable: nil}, nil
		},
	})

	// Tick everything, run it, stop it part-way.
	m = tap(m, "enter", "enter")
	m = tap(m, "ctrl+c")
	settled, cmd := sendWithCmd(m, finishedMsg{err: context.Canceled}, streamDoneMsg{})

	if cmd == nil {
		t.Fatal("a stopped run did not ask for a re-scan")
	}
	drain(t, cmd)
	if scans.Load() == 0 {
		t.Fatal("the re-scan never ran")
	}
	// The re-scan lands as a message, like it does in the real program.
	fresh := send(settled, inventoryMsg(Inventory{Components: after}))

	// Back to the menu, into the install list: it has to describe the machine
	// the stopped run left behind.
	install := tap(tap(fresh, "enter"), "enter")
	body := plain(install)
	if rowFor(body, "zsh") == "" || !strings.Contains(rowFor(body, "zsh"), "linked") {
		t.Errorf("the install list does not know zsh was linked:\n%s", body)
	}
	if !strings.Contains(rowFor(body, "ghostty"), "not linked") {
		t.Errorf("the install list thinks ghostty was linked:\n%s", body)
	}
}

// And Adopt shows exactly the work that is left, which is the whole point of
// having it: a stopped install becomes a to-do list.
func TestAdoptAfterAPartialRunIsTheRemainingWork(t *testing.T) {
	after := []app.Component{
		{Group: "Packages", Kind: app.KindTools, Label: "brew packages",
			Status: "1 of 2 present", Selected: true},
		{Group: "Packages", Kind: app.KindTool, Parent: "brew packages",
			Label: "jq", Status: "installed", Done: true, Selected: true},
		{Group: "Packages", Kind: app.KindTool, Parent: "brew packages",
			Label: "fd", Status: "missing", Selected: true},
		{Group: "Configs", Kind: app.KindStow, Label: "zsh",
			Status: "linked", Done: true, Selected: true},
		{Group: "Configs", Kind: app.KindStow, Label: "ghostty",
			Status: "not linked", Selected: true},
	}
	m := New(Config{
		Components: after, Version: "v1.0.0", Width: 80, Height: 30,
		Renderer: lipgloss.NewRenderer(io.Discard), Run: noWork,
	})
	adopt := openAdopt(t, m)

	got := map[string]bool{}
	for _, item := range adopt.items {
		got[item.Label] = true
	}
	for _, want := range []string{"brew packages", "fd", "ghostty"} {
		if !got[want] {
			t.Errorf("%q is still to do and is not on the list: %v", want,
				labelsOf(adopt.Chosen()))
		}
	}
	for _, absent := range []string{"jq", "zsh"} {
		if got[absent] {
			t.Errorf("%q was done and is on the list: %v", absent,
				labelsOf(adopt.Chosen()))
		}
	}
}

// transcriptText is the transcript's lines, for a readable assertion.
func transcriptText(m Model) []string {
	out := make([]string, 0, len(m.run.lines))
	for _, record := range m.Transcript() {
		out = append(out, record.Text)
	}
	return out
}

// The next run starts clean. runState is rebuilt per run, and a "stopped" that
// survived into the one after it would put the wrong word - and the wrong exit
// code - on a run nobody touched.
func TestASecondRunIsNotStillInterrupted(t *testing.T) {
	first := stopped(t, context.Canceled, nil)
	if text, _ := first.runStatus(); text != "interrupted" {
		t.Fatalf("the first run says %q", text)
	}
	// Back to the menu, run it again, let it finish.
	second := send(tap(tap(first, "enter"), "enter"), finishedMsg{}, streamDoneMsg{})
	if second.run.stopped {
		t.Error("the stop carried into the next run")
	}
	if text, colour := second.runStatus(); text != "done" || colour != gotui.Zoom {
		t.Errorf("status = %q %v", text, colour)
	}
	if err := second.Err(); err != nil {
		t.Errorf("err = %v", err)
	}
	if strings.Contains(strings.Join(transcriptText(second), "\n"), "nothing after this ran") {
		t.Error("the previous run's line survived into this one")
	}
}

// The verdict has to survive a narrow terminal. "interrupted" is eleven columns
// where "done" was four, and the footer drops what will not fit by rank - so the
// widest word is the one most likely to be the word that disappears.
func TestTheVerdictSurvivesANarrowTerminal(t *testing.T) {
	for _, width := range []int{60, 64, 72, 80, 100} {
		m := stopped(t, context.Canceled, nil)
		m = send(m, tea.WindowSizeMsg{Width: width, Height: 30})
		if body := plain(m); !strings.Contains(body, "interrupted") {
			t.Errorf("at %d columns the outcome is not on screen:\n%s", width, body)
		}
	}
}

// The transcript keeps the "working" records even though they render to no rows,
// because the replay into the scrollback is the only place they are ever
// printed - and without them a replayed run said "installed the missing tools"
// with no mention of what had run.
func TestTheTranscriptKeepsWhatEachResultCameFrom(t *testing.T) {
	m := tap(stoppableModel(t, nil, nil), "enter", "enter")
	m = send(m,
		eventMsg(app.Record{Kind: "phase", Text: "packages"}),
		eventMsg(app.Record{Kind: "working", Text: "brew bundle install"}),
		eventMsg(app.Record{Kind: "line", Mark: app.MarkChange, Text: "installed the missing tools"}),
		finishedMsg{}, streamDoneMsg{})

	var kinds []string
	for _, record := range m.Transcript() {
		kinds = append(kinds, record.Kind+":"+record.Text)
	}
	joined := strings.Join(kinds, " | ")
	if !strings.Contains(joined, "working:brew bundle install") {
		t.Errorf("the transcript dropped it: %s", joined)
	}
	if !strings.Contains(joined, "line:installed the missing tools") {
		t.Errorf("the result went missing: %s", joined)
	}

	// And it draws no row of its own: the spinner line is how it is shown, and
	// a second copy in the log would be a duplicate that never goes away.
	//
	// After a resize, because that is the only path that renders it: an event
	// appends rows for the line it just handled, so a working record only ever
	// reaches rowsFor when rewrap rebuilds the log from scratch. Which is the
	// nastier version of the bug - the duplicate would appear the first time
	// somebody dragged the window.
	resized := send(m, tea.WindowSizeMsg{Width: 90, Height: 30})
	if strings.Count(plain(resized), "brew bundle install") != 0 {
		t.Errorf("a resize drew the working record as a log row:\n%s", plain(resized))
	}
	// The footer counts what can be seen, not what was recorded.
	m2 := tap(stoppableModel(t, nil, nil), "enter", "enter")
	m2 = send(m2,
		eventMsg(app.Record{Kind: "working", Text: "brew bundle install"}),
		eventMsg(app.Record{Kind: "line", Mark: app.MarkChange, Text: "done that"}))
	if got, _ := m2.runStatus(); got != "1 line" {
		t.Errorf("the footer says %q for one visible line", got)
	}
}
