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
)

// The lists the selectors offer were read once, before the program started, and
// never again - so a removal that worked left the tools it had just uninstalled
// still saying "installed".

// afterRemoval is the machine as it looks once jq has gone.
func afterRemoval() Inventory {
	return Inventory{
		Components: []app.Component{
			{Group: "Configs", Label: "zsh", Status: "linked", Done: true, Selected: true},
		},
		Removable: []app.Component{
			{Group: "Packages", Label: "fd", Status: "installed", Done: true},
			{Group: "Packages", Label: "rg", Status: "installed", Done: true},
		},
	}
}

func scannedModel(t *testing.T, scans *atomic.Int32) Model {
	t.Helper()
	return New(Config{
		Components: components(),
		Removable:  removable(),
		Version:    "v1.0.0",
		Width:      90,
		Height:     30,
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Run:        noWork,
		Scan: func(context.Context) (Inventory, error) {
			scans.Add(1)
			return afterRemoval(), nil
		},
	})
}

// The bug, from the outside: remove something, go back in, and it is still
// listed.
func TestARemovalRefreshesWhatTheSelectorOffers(t *testing.T) {
	var scans atomic.Int32
	m := scannedModel(t, &scans)

	before := openRemoval(t, m)
	if !strings.Contains(plain(before), "jq") {
		t.Fatalf("the fixture does not list jq:\n%s", plain(before))
	}

	// Tick it, confirm, and let the run finish.
	m = tap(before, " ", "enter")
	if m.screen != ScreenRun {
		t.Fatalf("screen %v", m.screen)
	}
	next, cmd := m.Update(finishedMsg{})
	m = next.(Model)
	next, cmd2 := m.Update(streamDoneMsg{})
	m = next.(Model)

	// Settling is what asks the machine again.
	var refreshed tea.Msg
	for _, c := range []tea.Cmd{cmd, cmd2} {
		if c == nil {
			continue
		}
		if msg := c(); msg != nil {
			refreshed = msg
		}
	}
	if scans.Load() == 0 {
		t.Fatal("nothing re-read the machine after the run")
	}
	inventory, ok := refreshed.(inventoryMsg)
	if !ok {
		t.Fatalf("the re-scan produced %#v", refreshed)
	}
	m = send(m, inventory)

	// Back to the menu, back into the removal: jq is gone.
	back := openRemoval(t, tap(m, "enter"))
	body := plain(back)
	if strings.Contains(body, "jq") {
		t.Errorf("jq is still offered after being removed:\n%s", body)
	}
	for _, want := range []string{"fd", "rg"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s is missing from the refreshed list:\n%s", want, body)
		}
	}
}

// The same staleness on the other list: an install that linked something left
// it saying "not linked".
func TestARunRefreshesTheInstallSelectorToo(t *testing.T) {
	var scans atomic.Int32
	m := scannedModel(t, &scans)

	if !strings.Contains(plain(tap(m, "enter")), "not linked") {
		t.Fatalf("the fixture does not start unlinked:\n%s", plain(tap(m, "enter")))
	}

	m = send(m, inventoryMsg(afterRemoval()))
	body := plain(tap(m, "enter"))
	if strings.Contains(body, "not linked") {
		t.Errorf("the install selector is still stale:\n%s", body)
	}
	if !strings.Contains(body, "linked") {
		t.Errorf("the refreshed state is missing:\n%s", body)
	}
}

// Only the sources. m.items is the working copy an open selector is toggling,
// and replacing it would throw away ticks somebody had just made.
func TestARescanLandingMidSelectionKeepsTheTicks(t *testing.T) {
	var scans atomic.Int32
	m := openRemoval(t, scannedModel(t, &scans))
	m = tap(m, " ") // tick jq

	ticked := m.Chosen()
	if len(ticked) != 1 || ticked[0] != "jq" {
		t.Fatalf("ticked %v", ticked)
	}

	m = send(m, inventoryMsg(afterRemoval()))
	if got := m.Chosen(); len(got) != 1 || got[0] != "jq" {
		t.Errorf("the re-scan changed the selection to %v", got)
	}
	// And the next open picks the new data up, which is the moment it matters.
	fresh := openRemoval(t, tap(m, "esc"))
	if strings.Contains(plain(fresh), "jq") {
		t.Errorf("the next open did not use the new list:\n%s", plain(fresh))
	}
}

// A failure is silence: the lists it would have replaced were right a moment
// ago, and a menu that reports a failed re-scan is louder than the staleness.
func TestAFailedRescanChangesNothing(t *testing.T) {
	cmd := rescan(func(context.Context) (Inventory, error) {
		return Inventory{}, errors.New("brew: command not found")
	})
	if cmd == nil {
		t.Fatal("want a command")
	}
	if msg := cmd(); msg != nil {
		t.Errorf("a failed re-scan produced %#v", msg)
	}
}

// Nil is what a test wires, and it should cost no command at all.
func TestNoScannerMeansNoRescan(t *testing.T) {
	if rescan(nil) != nil {
		t.Error("a nil scanner should produce no command")
	}
	// And a window without one still works, with the lists it was given.
	m := send(tap(model(), "5", "enter"), finishedMsg{}, streamDoneMsg{})
	if !m.run.settled() {
		t.Error("the run did not settle without a scanner")
	}
}
