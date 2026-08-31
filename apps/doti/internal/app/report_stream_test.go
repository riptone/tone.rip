package app

import (
	"context"
	"sync"
	"testing"
	"time"
)

// StreamReporter is what lets the window run a command rather than launch one.
// The events are the same Records the Recorder collects, so these are about
// the channel and the release valve rather than about the shape.

func TestStreamReporterSendsEveryEventInOrder(t *testing.T) {
	out := make(chan Record, 16)
	r := StreamReporter{Out: out}

	r.Phase("configs")
	done := r.Working("brew bundle")
	done(MarkChange, "installed 4")
	r.Line(MarkOK, "already there")
	r.Summary("3 changed")
	close(out)

	var got []Record
	for rec := range out {
		got = append(got, rec)
	}
	want := []Record{
		{Kind: "phase", Text: "configs"},
		{Kind: "working", Text: "brew bundle"},
		{Kind: "line", Mark: MarkChange, Text: "installed 4"},
		{Kind: "line", Mark: MarkOK, Text: "already there"},
		{Kind: "summary", Text: "3 changed"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// The window closed mid-install. Without the release valve the goroutine still
// running it is blocked forever on a send, holding a line nobody will read.
func TestDoneReleasesABlockedSend(t *testing.T) {
	// Unbuffered, so the first send blocks until somebody receives.
	out := make(chan Record)
	stop := make(chan struct{})
	r := StreamReporter{Out: out, Done: stop}

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		r.Line(MarkOK, "nobody is listening")
	}()

	select {
	case <-returned:
		t.Fatal("the send did not block, so this proves nothing")
	case <-time.After(20 * time.Millisecond):
	}

	close(stop)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("closing Done did not release the send")
	}
}

// A reporter with no valve is the simple case - a test, a drain that cannot
// stop early - and must not select on a nil channel, which blocks forever.
func TestWithoutDoneItJustSends(t *testing.T) {
	out := make(chan Record, 1)
	StreamReporter{Out: out}.Line(MarkChange, "through")
	if got := <-out; got.Text != "through" {
		t.Errorf("got %+v", got)
	}
}

// The commands report from whatever goroutine they are on, and a run has more
// than one when a phase fans out.
func TestStreamReporterIsSafeFromSeveralGoroutines(t *testing.T) {
	out := make(chan Record, 128)
	r := StreamReporter{Out: out}

	var wait sync.WaitGroup
	for i := range 8 {
		wait.Add(1)
		go func(int) {
			defer wait.Done()
			r.Line(MarkOK, "x")
		}(i)
	}
	wait.Wait()
	close(out)

	var n int
	for range out {
		n++
	}
	if n != 8 {
		t.Errorf("got %d events, want 8", n)
	}
}

// Glyph is exported so the window renders the same mark the plain reporter
// prints. A second table would be a second answer.
func TestGlyphMatchesWhatThePlainReporterWrites(t *testing.T) {
	for _, mark := range []Mark{MarkNone, MarkOK, MarkChange, MarkSkip, MarkWarn} {
		if Glyph(mark) != marks[mark] {
			t.Errorf("Glyph(%v) = %q, table says %q", mark, Glyph(mark), marks[mark])
		}
		if got := len([]rune(Glyph(mark))); got != 1 {
			t.Errorf("Glyph(%v) is %d runes; the column has to line up", mark, got)
		}
	}
}

// A command run through the window is the same command: one call, one Reporter,
// no second path.
func TestAnOperationReportsTheSameThingIntoAChannel(t *testing.T) {
	viaRecorder, _, rec := fixture(t)
	if err := viaRecorder.Do(context.Background(), OpCheck, nil, "v1.0.0"); err != nil {
		t.Fatal(err)
	}

	viaStream, _, _ := fixture(t)
	out := make(chan Record, 256)
	viaStream.Report = StreamReporter{Out: out}
	if err := viaStream.Do(context.Background(), OpCheck, nil, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	close(out)

	var streamed []string
	for record := range out {
		streamed = append(streamed, record.Text)
	}
	recorded := rec.Texts()
	if len(streamed) != len(recorded) {
		t.Fatalf("the channel saw %d events, the recorder %d", len(streamed), len(recorded))
	}
	for i := range recorded {
		if streamed[i] != recorded[i] {
			t.Errorf("event %d: channel %q, recorder %q", i, streamed[i], recorded[i])
		}
	}
}
