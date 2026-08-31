package tui

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/packages/gotui"
)

// Running an operation inside the window rather than instead of it.
//
// The menu used to quit, hand its Action back to main, and let main print onto
// the terminal the window had just given up - so choosing Install from a TUI
// meant watching the TUI disappear. Everything happens here now: the same
// app.Install, reporting into an app.StreamReporter whose channel this screen
// reads as Bubble Tea messages.
//
// Nothing about the operations changed to make that work, which is what the
// Reporter seam was for. `doti install`, `doti install --term` and the menu's
// Install are one function with three renderings.

// eventCap is how many reported lines may queue before the operation waits for
// the window to catch up.
//
// Generous, because the alternative to buffering is a package manager blocked
// on a redraw. Bounded, because unbounded is a leak with a nicer name.
const eventCap = 256

// job is one operation in flight: the channel its events arrive on, the switch
// that releases a blocked send once the window has stopped reading, and the
// cancel that stops the work itself.
type job struct {
	events chan app.Record
	// borrows carries requests for the terminal - see borrow.go. Unbuffered:
	// the operation is blocked on the answer anyway, so there is nothing to
	// gain by letting it get ahead.
	borrows chan borrow
	done    chan struct{}
	cancel  context.CancelFunc
	once    sync.Once
	closed  sync.Once
}

type (
	// eventMsg is one reported phase, line or step-started.
	eventMsg app.Record
	// streamDoneMsg says the channel closed: no more events are coming.
	streamDoneMsg struct{}
	// finishedMsg is the operation's own return value.
	//
	// It can arrive *before* the last events are drained - two command
	// goroutines with no ordering between them - so the screen settles only
	// once both have landed. Settling on this alone dropped the closing lines
	// of every run.
	finishedMsg struct{ err error }
)

// startJob launches an operation and returns the commands that feed the screen.
func startJob(run RunFunc, action Action, chosen []string) (*job, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{
		events:  make(chan app.Record, eventCap),
		borrows: make(chan borrow),
		done:    make(chan struct{}),
		cancel:  cancel,
	}
	work := func() (msg tea.Msg) {
		// A panic here would otherwise take the process down with the terminal
		// still in the alt screen and its colours still overridden - the
		// reader's shell left unusable by a nil map somewhere in a package
		// manager wrapper. Turned into a failed run instead, which the screen
		// already knows how to show.
		defer func() {
			if r := recover(); r != nil {
				j.closeChannels()
				msg = finishedMsg{err: fmt.Errorf("%s panicked: %v\n%s",
					action, r, debug.Stack())}
			}
		}()
		err := run(ctx, action, chosen, RunOptions{
			Report: app.StreamReporter{Out: j.events, Done: j.done},
			Vault:  newBorrowRunner(j.borrows, j.done),
		})
		// Closed by the only writer, after its last send. The reader turns
		// that into streamDoneMsg and stops asking for more.
		j.closeChannels()
		return finishedMsg{err: err}
	}
	return j, tea.Batch(work, waitEvent(j), waitBorrow(j))
}

// closeChannels ends both waiters. Idempotent, because the panic path and the
// normal path both reach it and closing a channel twice panics - which would be
// a crash inside the recover that exists to prevent one.
func (j *job) closeChannels() {
	j.closed.Do(func() {
		close(j.events)
		close(j.borrows)
	})
}

// waitEvent parks one Bubble Tea command goroutine on the next event.
//
// Re-issued after every event, which is the framework's way of consuming a
// stream: one goroutine blocked on a receive, no polling, and nothing to tick.
func waitEvent(j *job) tea.Cmd {
	// No job means nothing to wait for: a screen built by a preview or a test
	// feeds its own events, and a nil receive here would be a panic on a
	// goroutine nobody is watching.
	if j == nil {
		return nil
	}
	return func() tea.Msg {
		rec, ok := <-j.events
		if !ok {
			return streamDoneMsg{}
		}
		return eventMsg(rec)
	}
}

// stop cancels the work and releases anything blocked reporting into it.
//
// Idempotent: it is called when the reader presses ctrl+c and again when the
// screen is torn down, and closing a channel twice panics.
func (j *job) stop() {
	if j == nil {
		return
	}
	j.cancel()
	j.once.Do(func() { close(j.done) })
}

// logLine is one line of a run's output, kept as data rather than as rendered
// text so a resize re-wraps it instead of re-flowing yesterday's arithmetic.
type logLine struct {
	kind string
	mark app.Mark
	text string
}

// runState is the run screen.
type runState struct {
	active bool
	action Action
	label  string

	lines []logLine
	// rendered is lines, already wrapped and padded for the current width.
	//
	// Kept beside the records rather than derived on every event, which is
	// what it used to be: re-wrapping the whole log each time a line arrived
	// made a five-hundred-line install quadratic in the number of lines, and
	// every one of those re-wraps threw away an identical result.
	rendered []string
	// renderedFor is the text width rendered was built at. A resize is the
	// only thing that invalidates it.
	renderedFor int

	// working is the text of a step that has started and not yet finished -
	// the one the spinner is spinning for. Held apart from rendered because it
	// is redrawn on every spinner frame and then replaced by its result.
	working string
	since   time.Time

	spin spinner.Model
	view viewport.Model
	// follow keeps the newest line in sight. Switched off as soon as the
	// reader scrolls up, because yanking somebody back to the bottom while
	// they are reading is the worst thing a log view can do.
	follow bool

	job      *job
	err      error
	finished bool
	drained  bool
	// updated records that this run replaced the binary, which is the one
	// outcome with something left to do afterwards.
	updated bool
}

// settled reports whether the operation is over *and* its output complete.
func (r runState) settled() bool { return r.finished && r.drained }

// newSpinner is the same frames the plain reporter animates, at a rate chosen
// to be seen rather than to be fast: every redraw repaints the whole card, and
// nothing here moves quickly enough to deserve more.
func newSpinner(s styles) spinner.Model {
	sp := spinner.New(spinner.WithSpinner(spinner.Spinner{
		Frames: gotui.SpinnerFrames,
		FPS:    time.Second / 8,
	}))
	sp.Style = s.faint
	return sp
}

// ---------------------------------------------------------------------------
// The model's side of a run: starting one, folding its events in, and keeping
// the viewport holding what it has reported so far.
//
// Here rather than in model.go because every one of these reads or writes
// runState, which is above - and model.go is routing.

// resize re-sizes the log viewport and re-wraps what is in it. Everything else
// is measured per render.
func (m Model) resize() Model {
	g := m.runGeometry()
	m.run.view.Width, m.run.view.Height = g.Text, g.Body
	return m.rewrap()
}

// The expensive path, and the only one that needs to be: a resize is the one
// thing that invalidates rows wrapped at the old width.
func (m Model) rewrap() Model {
	g := m.runGeometry()
	rows := make([]string, 0, len(m.run.lines)+2)
	for _, line := range m.run.lines {
		rows = append(rows, m.rowsFor(line, g.Text, len(rows) == 0)...)
	}
	m.run.rendered = rows
	m.run.renderedFor = g.Text
	return m.flow()
}

// flow puts the rendered rows, plus the spinner line if there is one, into the
// viewport.
func (m Model) flow() Model {
	rows := m.run.rendered
	if m.run.working != "" {
		// Copied rather than appended in place: append would write into
		// rendered's spare capacity, and the next real line would overwrite
		// the spinner row that is still on screen.
		rows = append(append(make([]string, 0, len(rows)+1), rows...),
			m.workingRow(m.runGeometry().Text))
	}
	m.run.view.SetContent(strings.Join(rows, "\n"))
	if m.run.follow {
		m.run.view.GotoBottom()
	}
	return m
}

// begin moves to the run screen and starts an operation.
func (m Model) begin(action Action, chosen []string) (Model, tea.Cmd) {
	g := m.runGeometry()
	view := viewport.New(g.Text, g.Body)
	// The viewport keeps its own bindings, which is a second answer to what
	// "down" means. Point them at the shared ones so there is one.
	view.KeyMap.Up = m.keys.Up
	view.KeyMap.Down = m.keys.Down
	view.KeyMap.PageUp = m.keys.PageUp
	view.KeyMap.PageDown = m.keys.PageDown

	m.screen = ScreenRun
	m.run = runState{
		active: true,
		action: action,
		label:  labelFor(action),
		spin:   newSpinner(m.styles),
		view:   view,
		follow: true,
	}
	if m.cfg.Start == action && m.cfg.Start != "" {
		m.launched = true
	}
	if m.cfg.Run == nil {
		// Nothing wired: say so on the screen rather than pretending to work.
		// Through appendLine, because a line added to the log is not on screen
		// until it has been flowed into the viewport - which is the one thing
		// every other path here gets from handling an event.
		m.run.finished, m.run.drained = true, true
		return m.appendLine(app.MarkWarn, "no operations are wired into this window"), nil
	}
	job, cmd := startJob(m.cfg.Run, action, chosen)
	m.run.job = job
	return m, tea.Batch(cmd, m.run.spin.Tick)
}

// One record renders one record's worth of rows. This used to re-wrap the whole
// log, which is the same answer computed from scratch on every line.
func (m Model) event(rec app.Record) (tea.Model, tea.Cmd) {
	if rec.Kind == "working" {
		m.run.working = rec.Text
		m.run.since = nowFunc()
		return m.flow(), waitEvent(m.run.job)
	}

	// A result line replaces the spinner it belongs to.
	m.run.working = ""
	line := logLine{kind: rec.Kind, mark: rec.Mark, text: rec.Text}
	g := m.runGeometry()
	if m.run.renderedFor != g.Text {
		// The width moved without a resize reaching us. Rebuild, which also
		// appends this line.
		m.run.lines = append(m.run.lines, line)
		return m.rewrap(), waitEvent(m.run.job)
	}
	m.run.lines = append(m.run.lines, line)
	m.run.rendered = append(m.run.rendered,
		m.rowsFor(line, g.Text, len(m.run.rendered) == 0)...)

	// Ask for the next one. Nothing else re-arms the stream.
	return m.flow(), waitEvent(m.run.job)
}

// For the things the window itself has to say: an operation's returned error,
// and the explanation when nothing is wired.
func (m Model) appendLine(mark app.Mark, text string) Model {
	line := logLine{kind: "line", mark: mark, text: text}
	g := m.runGeometry()
	m.run.lines = append(m.run.lines, line)
	if m.run.renderedFor != g.Text {
		return m.rewrap()
	}
	m.run.rendered = append(m.run.rendered,
		m.rowsFor(line, g.Text, len(m.run.rendered) == 0)...)
	return m.flow()
}

// afterRun is the bookkeeping that only makes sense once both the work and its
// output are complete.
func (m Model) afterRun() Model {
	if !m.run.settled() {
		return m
	}
	m.run.working = ""
	if m.run.action == ActionSelfUpdate && m.run.err == nil {
		m.run.updated = true
	}
	// Through flow, so the spinner row that was the last line goes away.
	return m.flow()
}

// spinning reports whether there is unfinished work to animate.
func (m Model) spinning() bool {
	return m.screen == ScreenRun && m.run.active && !m.run.settled()
}

// borrow hands the terminal to `bw` for as long as it needs it.
//
// The one place in this program that returns tea.Exec, and the reason the
// window can own a password prompt at all: Exec suspends the program, restores
// the terminal to the state bw expects, runs it, and resumes. The log says what
// is happening first, so the reader knows why their screen just changed.
func (m Model) borrow(req borrow) (tea.Model, tea.Cmd) {
	m = m.appendLine(app.MarkNone, "handing the terminal to "+describeBorrow(req.args))

	ctx := context.Background()
	command := newBorrowedCommand(ctx, vaultBin, req)
	return m, tea.Exec(command, func(err error) tea.Msg {
		// Buffered by the requester, so this cannot block the runtime.
		req.reply <- borrowResult{stdout: command.out.Bytes(), err: err}
		return borrowDoneMsg{}
	})
}
