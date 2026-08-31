package app

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riptone/tone.rip/packages/gotui"
)

// The rendering a terminal gets. Untested until now because it writes escape
// sequences to a stream, which is exactly what makes it easy to test: give it
// a buffer.

// syncBuffer is a bytes.Buffer the spinner goroutine may also write to.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestTheLiveReporterWritesTheSameLinesAsThePlainOne(t *testing.T) {
	var live, plain syncBuffer
	l := NewLiveReporter(&live)
	p := PlainReporter{Out: &plain}

	for _, r := range []Reporter{l, p} {
		r.Phase("configs")
		r.Line(MarkChange, "zsh        linked 4")
		r.Line(MarkOK, "ghostty    already linked")
		r.Summary("1 changed")
	}

	// The same text, once the colour is taken off.
	if got := stripSGR(live.String()); got != plain.String() {
		t.Errorf("the two renderings differ:\n live  %q\n plain %q", got, plain.String())
	}
}

// The colours come from the shared palette, which is the point of gotui.FG:
// they used to be spelled out here as raw escape sequences, free to drift from
// the values the window draws with.
func TestTheLiveReporterColoursComeFromThePalette(t *testing.T) {
	var out syncBuffer
	r := NewLiveReporter(&out)
	r.Line(MarkChange, "changed")
	r.Line(MarkWarn, "went wrong")
	r.Line(MarkOK, "already there")

	body := out.String()
	for _, want := range []struct {
		name string
		seq  string
	}{
		{"change is the zoom green", gotui.FG(gotui.Zoom)},
		{"warn is the accent", gotui.FG(gotui.Accent)},
		{"ok is faint", gotui.FG(gotui.Faint)},
	} {
		if !strings.Contains(body, want.seq) {
			t.Errorf("%s (%q) is not in the output", want.name, want.seq)
		}
	}
	if !strings.Contains(body, gotui.SGRReset) {
		t.Error("nothing reset the colour, so it bleeds into the next line")
	}
}

func TestAPhaseHeadingIsBold(t *testing.T) {
	var out syncBuffer
	NewLiveReporter(&out).Phase("packages")
	if !strings.Contains(out.String(), gotui.SGRBold) {
		t.Errorf("a phase heading is not bold: %q", out.String())
	}
}

// A mark with no colour writes none rather than a stray escape.
func TestAnUncolouredMarkWritesNoSequence(t *testing.T) {
	r := NewLiveReporter(&syncBuffer{})
	if got := r.colourFor(MarkNone); got != "" {
		t.Errorf("MarkNone has colour %q", got)
	}
	if got := r.colourFor(Mark(99)); got != "" {
		t.Errorf("an unknown mark has colour %q", got)
	}
}

// The spinner is not decoration: every slow step shells out, and their output
// is captured, so without something moving a two-minute brew looks like a hang.
func TestTheSpinnerTurnsAndThenClearsItself(t *testing.T) {
	var out syncBuffer
	r := NewLiveReporter(&out)

	done := r.Working("brew bundle")
	// Long enough for at least two frames at the reporter's own rate.
	time.Sleep(250 * time.Millisecond)

	spinning := out.String()
	if !strings.Contains(spinning, "brew bundle") {
		t.Errorf("the step is not on screen: %q", spinning)
	}
	var frames int
	for _, frame := range gotui.SpinnerFrames {
		if strings.Contains(spinning, frame) {
			frames++
		}
	}
	if frames < 2 {
		t.Errorf("the spinner showed %d distinct frames; it is not turning:\n%q", frames, spinning)
	}
	// One row, redrawn in place, so a long run does not scroll away.
	if strings.Contains(strings.TrimSuffix(spinning, "\n"), "\n") {
		t.Errorf("the spinner used more than one row:\n%q", spinning)
	}
	if !strings.Contains(spinning, "\r\x1b[K") {
		t.Error("the spinner does not clear its own line, so a shorter frame leaves a tail")
	}

	done(MarkChange, "installed 4 packages")
	final := out.String()
	if !strings.Contains(final, "installed 4 packages") {
		t.Errorf("the result is missing: %q", final)
	}
	// Cleared, not merely overwritten: the result line is shorter than the
	// spinner line, and the tail of the longer one would sit behind it.
	tail := final[strings.LastIndex(final, "installed 4 packages"):]
	for _, frame := range gotui.SpinnerFrames {
		if strings.Contains(tail, frame) {
			t.Errorf("a spinner frame outlived its result: %q", tail)
		}
	}
}

// The reporter is held by commands that report from more than one goroutine.
func TestTheLiveReporterIsSafeFromSeveralGoroutines(t *testing.T) {
	var out syncBuffer
	r := NewLiveReporter(&out)

	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			r.Line(MarkOK, "x")
		}()
	}
	wait.Wait()
	if got := strings.Count(out.String(), "x"); got != 8 {
		t.Errorf("wrote %d lines, want 8", got)
	}
}

// stripSGR removes the colour so two renderings can be compared as text.
func stripSGR(s string) string {
	var out strings.Builder
	for {
		start := strings.Index(s, "\x1b[")
		if start < 0 {
			out.WriteString(s)
			return out.String()
		}
		out.WriteString(s[:start])
		end := strings.IndexByte(s[start:], 'm')
		if end < 0 {
			return out.String()
		}
		s = s[start+end+1:]
	}
}
