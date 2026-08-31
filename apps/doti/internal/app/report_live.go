package app

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/riptone/tone.rip/packages/gotui"
)

// LiveReporter is the same output for a terminal, with colour and a spinner
// on whatever is currently running.
//
// The spinner is not decoration. Every slow step here shells out to brew,
// git, winget or npm, and their output is captured rather than streamed (see
// pkgs.ExecRunner) so the display stays readable - which means that without
// something moving, a two-minute `brew bundle` looks exactly like a hang. The
// elapsed counter is there for the same reason.
type LiveReporter struct {
	Out io.Writer
	// Colour is the palette. Empty strings disable it, which is what makes
	// this testable without asserting escape sequences.
	Phase_, OK, Change, Skip, Warn, Faint, Reset string

	mu      sync.Mutex
	spinner *spinner
}

// NewLiveReporter builds one with the shared palette's colours.
func NewLiveReporter(out io.Writer) *LiveReporter {
	return &LiveReporter{
		Out: out,
		// Raw sequences rather than lipgloss styles: this writes to a stream
		// interleaved with cursor control, not into a styled block, and a
		// Style would end the background mid-line. The values still come from
		// the shared palette - they used to be spelled out here, which made
		// them a second copy of it.
		Phase_: gotui.SGRBold,
		OK:     gotui.FG(gotui.Faint),
		Change: gotui.FG(gotui.Zoom),
		Skip:   gotui.FG(gotui.Faint),
		Warn:   gotui.FG(gotui.Accent),
		Faint:  gotui.FG(gotui.Faint),
		Reset:  gotui.SGRReset,
	}
}

func (r *LiveReporter) colourFor(mark Mark) string {
	switch mark {
	case MarkChange:
		return r.Change
	case MarkWarn:
		return r.Warn
	case MarkSkip:
		return r.Skip
	case MarkOK:
		return r.OK
	}
	return ""
}

func (r *LiveReporter) Phase(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(r.Out, "\n%s%s%s\n", r.Phase_, name, r.Reset)
}

func (r *LiveReporter) Line(mark Mark, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.line(mark, text)
}

func (r *LiveReporter) line(mark Mark, text string) {
	colour := r.colourFor(mark)
	fmt.Fprintf(r.Out, "  %s%s%s %s\n", colour, marks[mark], r.Reset, text)
}

func (r *LiveReporter) Working(text string) func(Mark, string) {
	r.mu.Lock()
	r.spinner = newSpinner(r.Out, text, r.Faint, r.Reset)
	r.mu.Unlock()

	return func(mark Mark, result string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.spinner != nil {
			r.spinner.stop()
			r.spinner = nil
		}
		r.line(mark, result)
	}
}

func (r *LiveReporter) Summary(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(r.Out, "\n%s\n", text)
}

// spinner redraws one line in place until it is stopped.
type spinner struct {
	out    io.Writer
	stopCh chan struct{}
	done   chan struct{}
}

// The frames come from the palette package, shared with the window: the same
// run spinning differently in the two renderings is the kind of detail that
// makes two programs look like three.
var spinnerFrames = gotui.SpinnerFrames

func newSpinner(out io.Writer, text, faint, reset string) *spinner {
	s := &spinner{out: out, stopCh: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		start := time.Now()
		ticker := time.NewTicker(90 * time.Millisecond)
		defer ticker.Stop()
		for frame := 0; ; frame++ {
			// \r and a clear-to-end-of-line rather than a newline, so the
			// whole animation occupies one row and leaves nothing behind.
			fmt.Fprintf(out, "\r\x1b[K  %s %s %s(%s)%s",
				spinnerFrames[frame%len(spinnerFrames)], text,
				faint, elapsed(time.Since(start)), reset)
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
			}
		}
	}()
	return s
}

func (s *spinner) stop() {
	close(s.stopCh)
	<-s.done
	// Cleared, not merely overwritten: the caller writes the result line
	// next, and a shorter result would leave the tail of the longer spinner
	// line sitting behind it.
	fmt.Fprint(s.out, "\r\x1b[K")
}

// elapsed is a compact duration: seconds under a minute, then m:ss.
func elapsed(d time.Duration) string {
	seconds := int(d.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
}
