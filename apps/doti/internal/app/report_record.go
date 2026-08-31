package app

import (
	"strings"
	"sync"
)

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

// StreamReporter sends every event to a channel, for a renderer that is not a
// writer at all.
//
// This is what lets the window *run* a command rather than launch one: the TUI
// starts app.Install in a goroutine holding one of these and pulls Records off
// the channel as Bubble Tea messages. The commands are unchanged and unaware,
// which is the whole point of the Reporter seam - and the events are the same
// Records the Recorder collects, so the tests that describe the shape of a run
// describe this too.
type StreamReporter struct {
	// Out receives every event. The caller buffers it: a full channel blocks
	// the command, which is the right way round - the alternative is dropping
	// the line that says what went wrong.
	Out chan<- Record
	// Done releases a blocked send when the reader has gone. Without it a
	// window closed mid-install leaks the goroutine still running it, holding
	// a line nobody will ever read.
	Done <-chan struct{}
}

func (r StreamReporter) send(kind string, mark Mark, text string) {
	rec := Record{Kind: kind, Mark: mark, Text: text}
	if r.Done == nil {
		r.Out <- rec
		return
	}
	select {
	case r.Out <- rec:
	case <-r.Done:
	}
}

func (r StreamReporter) Phase(name string)     { r.send("phase", MarkNone, name) }
func (r StreamReporter) Line(m Mark, t string) { r.send("line", m, t) }
func (r StreamReporter) Summary(text string)   { r.send("summary", MarkNone, text) }

func (r StreamReporter) Working(text string) func(Mark, string) {
	r.send("working", MarkNone, text)
	return func(m Mark, result string) { r.send("line", m, result) }
}
