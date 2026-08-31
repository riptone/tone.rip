// Package app holds what doti's commands actually do.
//
// It lives here rather than in package main for two reasons. The first is
// that package main had grown to 773 lines with no tests, because a func in
// main is not reachable from one. The second is the interesting one:
// commands here do not print. They report.
//
// That indirection is what makes `doti install` and the menu's Install
// identical rather than merely similar. Both call the same function with the
// same Reporter, so there is no second code path to keep in step - and the
// rendering is chosen once, from whether anything is watching.
package app

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Mark is the glyph a line carries, and what it means.
type Mark int

const (
	// MarkNone is a plain line with no verdict.
	MarkNone Mark = iota
	// MarkOK is "already the way it should be" - nothing was done.
	MarkOK
	// MarkChange is "this run changed it".
	MarkChange
	// MarkSkip is "deliberately not done here".
	MarkSkip
	// MarkWarn is "did not work, and the run continued anyway".
	MarkWarn
)

// Reporter renders progress. Commands hold one and never touch a writer, so
// the same command can be run interactively, piped to a file, or asserted
// against in a test without knowing the difference.
type Reporter interface {
	// Phase begins a named stage: "packages", "configs", "secrets".
	Phase(name string)
	// Line records one outcome inside the current phase.
	Line(mark Mark, text string)
	// Working announces something slow and returns the function that ends
	// it. Implementations may animate between the two calls; callers must
	// call the returned function exactly once, and must not report anything
	// else in between.
	Working(text string) func(mark Mark, result string)
	// Summary closes the command out.
	Summary(text string)
}

// marks are the glyphs, chosen to line up in a column: every one is a single
// terminal cell, so a run of lines does not ripple sideways.
var marks = map[Mark]string{
	MarkNone:   " ",
	MarkOK:     "·",
	MarkChange: "+",
	MarkSkip:   "-",
	MarkWarn:   "!",
}

// PlainReporter writes lines and nothing else.
//
// Used when stdout is not a terminal - a pipe, a file, CI - where cursor
// movement would be noise in a log and a spinner would be thousands of
// wasted lines.
type PlainReporter struct {
	Out io.Writer
}

func (r PlainReporter) Phase(name string) {
	fmt.Fprintf(r.Out, "\n%s\n", name)
}

func (r PlainReporter) Line(mark Mark, text string) {
	fmt.Fprintf(r.Out, "  %s %s\n", marks[mark], text)
}

func (r PlainReporter) Working(text string) func(Mark, string) {
	fmt.Fprintf(r.Out, "  … %s\n", text)
	return func(mark Mark, result string) { r.Line(mark, result) }
}

func (r PlainReporter) Summary(text string) {
	fmt.Fprintf(r.Out, "\n%s\n", text)
}

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
		// Set directly rather than through lipgloss: this writes to a raw
		// stream interleaved with cursor control, not into a styled block,
		// and a Style would reset the background mid-line.
		Phase_: "\x1b[1m",
		OK:     "\x1b[38;2;138;138;138m",
		Change: "\x1b[38;2;40;200;64m",
		Skip:   "\x1b[38;2;138;138;138m",
		Warn:   "\x1b[38;2;255;92;0m",
		Faint:  "\x1b[38;2;138;138;138m",
		Reset:  "\x1b[0m",
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

// Braille frames: one cell wide in every font that has them, so the line does
// not change width as it turns.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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

// Record is one reported event, for tests.
type Record struct {
	Kind string // "phase" | "line" | "working" | "summary"
	Mark Mark
	Text string
}

// Recorder collects events instead of rendering them.
//
// This is what makes the commands testable at all: a test can assert that
// installing on a machine with everything already in place reports no
// changes, without a terminal, a package manager or a $HOME.
type Recorder struct {
	mu      sync.Mutex
	Records []Record
}

func (r *Recorder) add(kind string, mark Mark, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Records = append(r.Records, Record{Kind: kind, Mark: mark, Text: text})
}

func (r *Recorder) Phase(name string)     { r.add("phase", MarkNone, name) }
func (r *Recorder) Line(m Mark, t string) { r.add("line", m, t) }
func (r *Recorder) Summary(text string)   { r.add("summary", MarkNone, text) }

func (r *Recorder) Working(text string) func(Mark, string) {
	r.add("working", MarkNone, text)
	return func(m Mark, result string) { r.add("line", m, result) }
}

// Texts is every recorded line, for a coarse assertion.
func (r *Recorder) Texts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.Records))
	for _, rec := range r.Records {
		out = append(out, rec.Text)
	}
	return out
}

// Contains reports whether any recorded text contains substr.
func (r *Recorder) Contains(substr string) bool {
	for _, text := range r.Texts() {
		if strings.Contains(text, substr) {
			return true
		}
	}
	return false
}

// Marked counts the lines carrying a given mark.
func (r *Recorder) Marked(mark Mark) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int
	for _, rec := range r.Records {
		if rec.Kind == "line" && rec.Mark == mark {
			n++
		}
	}
	return n
}
