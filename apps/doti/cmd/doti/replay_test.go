package main

import (
	"strings"
	"testing"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
)

// The claim in runWindow: what a replay writes is what --term would have
// printed. It was not - the window's transcript dropped every "working" record,
// so a replayed run said "installed the missing tools" with no mention of what
// had run.
func TestAReplayIsWhatTermWouldHavePrinted(t *testing.T) {
	// One run, reported once into a PlainReporter and once into a Recorder,
	// then the Recorder's records replayed. The two outputs have to match.
	report := func(r app.Reporter) {
		r.Phase("packages")
		r.Line(app.MarkOK, "15 of 16 tools present")
		done := r.Working("brew bundle install")
		done(app.MarkChange, "installed the missing tools")
		r.Line(app.MarkWarn, "zsh: backing up ~/.zshrc")
		r.Summary("3 changed")
	}

	var direct strings.Builder
	report(app.PlainReporter{Out: &direct})

	recorder := &app.Recorder{}
	report(recorder)
	var replayed strings.Builder
	replay(recorder.Records, &replayed)

	if direct.String() != replayed.String() {
		t.Errorf("a replay is not what --term printed\n--- --term\n%s\n--- replay\n%s",
			direct.String(), replayed.String())
	}
	if !strings.Contains(replayed.String(), "… brew bundle install") {
		t.Errorf("the replay lost which command ran:\n%s", replayed.String())
	}
}
