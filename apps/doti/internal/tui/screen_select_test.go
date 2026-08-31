package tui

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/packages/gotui"
)

// The selector, and the one operation whose selector is the confirmation.

func removable() []app.Component {
	return []app.Component{
		{Group: "Packages", Label: "jq", Status: "installed", Done: true},
		{Group: "Packages", Label: "fd", Status: "installed", Done: true},
		{Group: "Packages", Label: "rg", Status: "installed", Done: true},
	}
}

func removeModel(cfg ...func(*Config)) Model {
	c := Config{
		Components: components(),
		Removable:  removable(),
		Version:    "v1.0.0",
		Width:      80,
		Height:     30,
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Run:        noWork,
	}
	for _, apply := range cfg {
		apply(&c)
	}
	return New(c)
}

// removeAt is the menu index of the removal, found rather than hardcoded so
// re-ordering the menu does not quietly point this at Install.
func removeAt(t *testing.T) int {
	t.Helper()
	for i, entry := range menu {
		if entry.action == ActionRemove {
			return i
		}
	}
	t.Fatal("no removal in the menu")
	return 0
}

func openRemoval(t *testing.T, m Model) Model {
	t.Helper()
	m.menuAt = removeAt(t)
	next := tap(m, "enter")
	if next.screen != ScreenSelect {
		t.Fatalf("the removal did not open the selector: screen %v", next.screen)
	}
	return next
}

// The safe action - press enter without thinking - is the one that does
// nothing. Every other selector starts with everything ticked.
func TestARemovalSelectorStartsWithNothingTicked(t *testing.T) {
	m := openRemoval(t, removeModel())
	if got := len(m.Chosen()); got != 0 {
		t.Errorf("%d components arrived ticked: %v", got, m.Chosen())
	}
	if got := len(m.items); got != len(removable()) {
		t.Errorf("the selector offered %d components, want %d", got, len(removable()))
	}
	body := plain(m)
	if !strings.Contains(body, "nothing is ticked: tick what to uninstall") {
		t.Errorf("the screen does not say what is going on:\n%s", body)
	}
	if !strings.Contains(body, "0 to remove") {
		t.Errorf("the count does not say what it counts:\n%s", body)
	}

	// And an install still starts with everything ticked.
	install := tap(removeModel(), "enter")
	if got := len(install.Chosen()); got != len(components()) {
		t.Errorf("an install selector offered %d ticked, want all %d",
			got, len(components()))
	}
}

// It offers the removable list, not the general one - those are different
// questions with different answers.
func TestARemovalSelectorOffersTheRemovableList(t *testing.T) {
	m := openRemoval(t, removeModel())
	body := plain(m)
	for _, want := range []string{"jq", "fd", "rg"} {
		if !strings.Contains(body, want) {
			t.Errorf("the removable %q is not offered:\n%s", want, body)
		}
	}
	// The install list's components have no business here.
	for _, unwanted := range []string{"brew packages", "mssql-envs"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("%q is offered for removal:\n%s", unwanted, body)
		}
	}
}

// Enter with nothing ticked stays put. Running it would report the no-op at
// length; saying nothing happened by not moving is clearer.
func TestConfirmingARemovalWithNothingTickedDoesNothing(t *testing.T) {
	m := openRemoval(t, removeModel())
	next := tap(m, "enter")
	if next.screen != ScreenSelect {
		t.Errorf("enter with nothing ticked went to screen %v", next.screen)
	}
	if next.run.active {
		t.Error("enter with nothing ticked started a run")
	}
}

func TestConfirmingARemovalWithSomethingTickedRunsIt(t *testing.T) {
	var got []string
	m := openRemoval(t, removeModel(func(c *Config) {
		c.Run = func(_ context.Context, action Action, chosen []string, _ RunOptions) error {
			if action == ActionRemove {
				got = append([]string(nil), chosen...)
			}
			return nil
		}
	}))

	m = tap(m, " ", "down", " ") // jq and fd
	if len(m.Chosen()) != 2 {
		t.Fatalf("ticked %v", m.Chosen())
	}
	next := tap(m, "enter")
	if next.screen != ScreenRun {
		t.Fatalf("enter went to screen %v", next.screen)
	}
	if next.run.action != ActionRemove {
		t.Errorf("action = %q", next.run.action)
	}

	_, cmd := m.begin(ActionRemove, m.Chosen())
	drain(t, cmd)
	if strings.Join(got, ",") != "jq,fd" {
		t.Errorf("the operation was given %v, want jq and fd", got)
	}
}

// The count is the confirmation, so it turns the moment it is not zero.
func TestTheRemovalCountTurnsRedWhenSomethingIsTicked(t *testing.T) {
	painted := removeModel(func(c *Config) { c.Renderer = gotui.OfflineRenderer(io.Discard) })
	none := openRemoval(t, painted)
	// The footer row alone: the close button is painted in this same red, so
	// searching the whole frame would find it either way.
	if strings.Contains(footerOf(none.View()), sgrParams(gotui.Close)) {
		t.Errorf("the count is red with nothing ticked:\n%q", footerOf(none.View()))
	}
	some := tap(none, " ")
	if !strings.Contains(footerOf(some.View()), sgrParams(gotui.Close)) {
		t.Errorf("the count is not red with something ticked:\n%q", footerOf(some.View()))
	}
	if !strings.Contains(plain(some), "1 to remove") {
		t.Errorf("the count did not follow:\n%s", plain(some))
	}
}

// A machine with nothing removable gets a sentence rather than an empty box.
func TestAnEmptyRemovableListSaysSo(t *testing.T) {
	m := openRemoval(t, removeModel(func(c *Config) { c.Removable = nil }))
	body := plain(m)
	if !strings.Contains(body, "Nothing this repository installed is still present") {
		t.Errorf("an empty list said nothing:\n%s", body)
	}
	// And enter still does nothing rather than starting an empty removal.
	if tap(m, "enter").screen != ScreenSelect {
		t.Error("enter on an empty removal list started something")
	}
}

// Opening a different operation's selector replaces the list rather than
// leaving the last one's ticks behind.
func TestTheSelectorListFollowsTheOperation(t *testing.T) {
	m := removeModel()

	install := tap(m, "enter")
	if len(install.items) != len(components()) {
		t.Fatalf("install offered %d", len(install.items))
	}
	back := tap(install, "esc")
	removal := openRemoval(t, back)
	if len(removal.items) != len(removable()) {
		t.Errorf("the removal offered %d components, want %d",
			len(removal.items), len(removable()))
	}
	if got := len(removal.Chosen()); got != 0 {
		t.Errorf("the install's ticks survived into the removal: %v", removal.Chosen())
	}

	// And back the other way.
	again := tap(tap(removal, "esc"), "1", "enter")
	if got := len(again.Chosen()); got != len(components()) {
		t.Errorf("returning to install offered %d ticked, want all", got)
	}
}
