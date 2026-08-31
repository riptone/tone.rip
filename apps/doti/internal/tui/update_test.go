package tui

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
)

// The release check and the footer offer it produces.

func TestTheUpdateOfferAppearsOnlyWhenThereIsOne(t *testing.T) {
	m := model()
	if strings.Contains(plain(m), "update to") {
		t.Errorf("an offer with nothing to offer:\n%s", plain(m))
	}
	offered := send(m, updateFoundMsg("v0.2.0"))
	if !strings.Contains(plain(offered), "u update to v0.2.0") {
		t.Errorf("the offer names no version:\n%s", plain(offered))
	}
}

// u does nothing until the check has found something: a key that silently
// reinstalls the running version is worse than one that is not there.
func TestUIsInertUntilThereIsAnUpdate(t *testing.T) {
	if got := tap(model(), "u").screen; got != ScreenMenu {
		t.Errorf("u started something with no update to install: screen %v", got)
	}
	m := tap(send(model(), updateFoundMsg("v0.2.0")), "u")
	if m.screen != ScreenRun {
		t.Fatalf("u did not start the self-update: screen %v", m.screen)
	}
	if m.run.action != ActionSelfUpdate {
		t.Errorf("action = %q, want the self-update", m.run.action)
	}
}

// The one outcome with something left to do: the binary that just ran is not
// the binary on disk any more.
func TestAFinishedSelfUpdateOffersARestart(t *testing.T) {
	m := tap(send(model(), updateFoundMsg("v0.2.0")), "u")
	m = send(m, finishedMsg{}, streamDoneMsg{})
	if !m.run.updated {
		t.Fatal("a clean self-update was not recorded as one")
	}
	if got := m.runStatus(); got != "updated" {
		t.Errorf("status = %q, want updated", got)
	}
	if !strings.Contains(plain(m), "r restart") {
		t.Errorf("no restart offered:\n%s", plain(m))
	}
	if next := tap(m, "r"); !next.Restart() {
		t.Error("r did not ask for a restart")
	}
}

// A failed self-update has not replaced anything, so restarting would only
// run the same binary again.
func TestAFailedSelfUpdateOffersNoRestart(t *testing.T) {
	m := tap(send(model(), updateFoundMsg("v0.2.0")), "u")
	m = send(m, finishedMsg{err: errors.New("curl: (6) could not resolve host")}, streamDoneMsg{})
	if m.run.updated {
		t.Error("a failed self-update claimed to have updated")
	}
	if strings.Contains(plain(m), "r restart") {
		t.Errorf("a restart offered after a failure:\n%s", plain(m))
	}
	if next := tap(m, "r"); next.Restart() {
		t.Error("r asked for a restart after a failed update")
	}
}

// A check that fails is silence. A menu that shouts about DNS is worse than
// one that never mentions updates.
func TestAFailedCheckIsSilent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check CheckFunc
	}{
		{"an error", func(context.Context) (string, error) {
			return "", errors.New("dial tcp: lookup api.github.com: no such host")
		}},
		{"nothing newer", func(context.Context) (string, error) { return "", nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := checkUpdate(tc.check)
			if cmd == nil {
				t.Fatal("want a command")
			}
			if msg := cmd(); msg != nil {
				t.Errorf("the check produced %#v; it should produce nothing", msg)
			}
		})
	}
}

// Cheaper than a command that returns nothing, and it is the shape a test or
// an offline build has.
func TestNoCheckMeansNoCommand(t *testing.T) {
	if checkUpdate(nil) != nil {
		t.Error("a nil check should produce no command at all")
	}
}

func TestACheckThatFindsSomethingProducesTheVersion(t *testing.T) {
	cmd := checkUpdate(func(context.Context) (string, error) { return "v9.9.9", nil })
	msg, ok := cmd().(updateFoundMsg)
	if !ok {
		t.Fatalf("got %#v, want an updateFoundMsg", cmd())
	}
	if string(msg) != "v9.9.9" {
		t.Errorf("version = %q", msg)
	}
}

// Init has to fire the check, or the offer never arrives.
func TestInitAsksAboutUpdates(t *testing.T) {
	var asked bool
	m := New(Config{
		Components: components(),
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Width:      80, Height: 26,
		Run: noWork,
		Check: func(context.Context) (string, error) {
			asked = true
			return "", nil
		},
	})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init produced no commands")
	}
	cmd()
	if !asked {
		t.Error("Init did not ask about updates")
	}
}

// The window opened on one operation starts it rather than showing a menu.
//
// The model has to *be* on the run screen before Bubble Tea draws its first
// frame. Doing the launch in Init built that model and discarded it - Init
// returns commands, not a model - so the menu stayed on screen and, worse, the
// events arrived at a model with no job: the stream stopped after one line.
func TestTheLaunchedOperationIsOnScreenAndKeepsItsStream(t *testing.T) {
	var ran app.Operation
	m := New(Config{
		Components: components(),
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Width:      80, Height: 26,
		Start: ActionInstall,
		Run: func(_ context.Context, action Action, _ []string, _ RunOptions) error {
			ran = app.Operation(action)
			return nil
		},
	})
	if m.screen != ScreenRun {
		t.Fatalf("screen = %v before the first frame, want the run screen", m.screen)
	}
	if m.run.job == nil {
		t.Fatal("no job, so nothing will re-arm the event stream")
	}
	if !m.launched {
		t.Error("the run was not marked as the one the window opened on")
	}

	// Two events, because the bug showed up on the second: the first arrives
	// from the command Init returned, and only a live job asks for another.
	next, cmd := m.Update(line(app.MarkOK, "one"))
	if cmd == nil {
		t.Error("the first event did not re-arm the stream")
	}
	if _, cmd = next.(Model).Update(line(app.MarkOK, "two")); cmd == nil {
		t.Error("the second event did not re-arm the stream")
	}
	if body := plain(next.(Model)); !strings.Contains(body, "one") {
		t.Errorf("the reported line is not on screen:\n%s", body)
	}

	drain(t, m.Init())
	if ran != app.OpInstall {
		t.Errorf("the launched operation was %q, want install", ran)
	}
}
