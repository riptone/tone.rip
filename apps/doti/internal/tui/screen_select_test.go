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
	var got []app.Ref
	m := openRemoval(t, removeModel(func(c *Config) {
		c.Run = func(_ context.Context, action Action, chosen []app.Ref, _ RunOptions) error {
			if action == ActionRemove {
				got = append([]app.Ref(nil), chosen...)
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
	if strings.Join(labelsOf(got), ",") != "jq,fd" {
		t.Errorf("the operation was given %v, want jq and fd", labelsOf(got))
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

// ------------------------------------------------------- picking to unlink --

// unlinkAt is the menu index of the unlink, found rather than hardcoded.
func unlinkAt(t *testing.T) int {
	t.Helper()
	for i, entry := range menu {
		if entry.action == ActionUnlink {
			return i
		}
	}
	t.Fatal("no unlink in the menu")
	return 0
}

// Unlink used to act on every stow package there was, which made "take ghostty
// back off this machine" a thing you could only do by unlinking all of them and
// re-installing the rest.
func TestUnlinkOpensASelector(t *testing.T) {
	m := model()
	m.menuAt = unlinkAt(t)
	next := tap(m, "enter")
	if next.screen != ScreenSelect {
		t.Fatalf("the unlink did not open a selector: screen %v", next.screen)
	}
}

// And it offers the stow packages alone. A list that also offered `brew
// packages`, the MCP servers, ~/.gitconfig.local and the secrets would be three
// quarters checkboxes that change nothing about what an unlink does.
func TestTheUnlinkSelectorOffersStowPackagesOnly(t *testing.T) {
	m := model()
	m.menuAt = unlinkAt(t)
	m = tap(m, "enter")

	var want int
	for _, item := range components() {
		if item.Kind == app.KindStow {
			want++
		}
	}
	if want < 2 {
		t.Fatal("the fixture has fewer than two stow packages, so this proves nothing")
	}
	if len(m.items) != want {
		t.Errorf("the selector offered %d components, want the %d stow packages: %v",
			len(m.items), want, m.Chosen())
	}
	for _, item := range m.items {
		if item.Kind != app.KindStow {
			t.Errorf("%q (%s) is not a stow package", item.Label, item.Kind)
		}
	}

	body := plain(m)
	for _, absent := range []string{"brew packages", "mcp servers", "mssql-envs"} {
		if strings.Contains(body, absent) {
			t.Errorf("%q is on the unlink selector:\n%s", absent, body)
		}
	}
	for _, present := range []string{"zsh", "ghostty"} {
		if !strings.Contains(body, present) {
			t.Errorf("%q is missing from the unlink selector:\n%s", present, body)
		}
	}
}

// Everything ticked, like the other two that repair rather than delete - and
// unlike the removal, because an unlink puts back what was there before.
func TestTheUnlinkSelectorStartsWithEverythingTicked(t *testing.T) {
	m := model()
	m.menuAt = unlinkAt(t)
	m = tap(m, "enter")
	if len(m.Chosen()) != len(m.items) {
		t.Errorf("%d of %d ticked", len(m.Chosen()), len(m.items))
	}
}

// One package, and only that one reaches the operation.
func TestUnlinkingPassesOnlyTheTickedPackages(t *testing.T) {
	m := model()
	m.menuAt = unlinkAt(t)
	m = tap(m, "enter", "n", " ")

	chosen := m.Chosen()
	if len(chosen) != 1 {
		t.Fatalf("chosen = %v, want one", chosen)
	}
	next := tap(m, "enter")
	if next.screen != ScreenRun {
		t.Fatalf("screen = %v", next.screen)
	}
	if next.run.action != ActionUnlink {
		t.Errorf("action = %q", next.run.action)
	}
}

// ------------------------------------------------- nothing ticked is nothing --

// Untick every box on an Install, press enter, and it installed the lot.
//
// By accident, and a bad one: an empty selection reaches internal/app as an
// empty Include, and an empty Include is how the command line spells "no
// narrowing". So the safest-looking keypress on the screen was the most
// destructive one available.
func TestAnEmptySelectionRunsNothing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action Action
		at     func(*testing.T) int
	}{
		{"install", ActionInstall, func(*testing.T) int { return 0 }},
		{"unlink", ActionUnlink, unlinkAt},
		{"removal", ActionRemove, removeAt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := removeModel()
			m.menuAt = tc.at(t)
			m = tap(m, "enter", "n", "enter")

			if m.screen != ScreenSelect {
				t.Fatalf("nothing was ticked and it ran anyway: screen %v", m.screen)
			}
			if m.run.active {
				t.Errorf("a %s was started with nothing ticked", tc.action)
			}
		})
	}
}

// A keypress that does nothing and explains nothing reads as a hang.
func TestAnEmptySelectionSaysSo(t *testing.T) {
	m := tap(removeModel(), "enter", "n", "enter")
	body := plain(m)
	if !strings.Contains(body, "nothing is ticked") {
		t.Errorf("the screen does not say why enter did nothing:\n%s", body)
	}
	if !strings.Contains(body, "press a for all") {
		t.Errorf("the screen does not say what to do about it:\n%s", body)
	}
}

// And the complaint goes away as soon as it stops being true.
func TestTheEmptyNoticeClearsOnTheNextKey(t *testing.T) {
	m := tap(removeModel(), "enter", "n", "enter")
	if m.notice == "" {
		t.Fatal("no notice, so this proves nothing")
	}
	if got := tap(m, " ").notice; got != "" {
		t.Errorf("the notice survived a toggle: %q", got)
	}
	if got := tap(m, "down").notice; got != "" {
		t.Errorf("the notice survived a move: %q", got)
	}
	if got := tap(m, "a").notice; got != "" {
		t.Errorf("the notice survived ticking everything: %q", got)
	}
}

// Going back and coming in again is a clean slate.
func TestTheEmptyNoticeDoesNotFollowYouAround(t *testing.T) {
	m := tap(removeModel(), "enter", "n", "enter")
	if m.notice == "" {
		t.Fatal("no notice, so this proves nothing")
	}
	if got := tap(m, "esc").notice; got != "" {
		t.Errorf("the notice survived esc: %q", got)
	}
	if got := tap(tap(m, "esc"), "enter").notice; got != "" {
		t.Errorf("the notice came back with the selector: %q", got)
	}
}

// The notice is the answer to the key just pressed, so it outranks the standing
// instructions - including the removal's own warning, which says the same thing
// in the calmer voice appropriate to a screen nobody has pressed enter on yet.
func TestTheNoticeReplacesTheLeadLine(t *testing.T) {
	m := openRemoval(t, removeModel())
	if !strings.Contains(plain(m), "tick what to uninstall") {
		t.Fatalf("the removal's own lead is missing:\n%s", plain(m))
	}
	body := plain(tap(m, "enter"))
	if strings.Contains(body, "tick what to uninstall") {
		t.Errorf("two lead lines at once:\n%s", body)
	}
	if !strings.Contains(body, "nothing is ticked: press a") {
		t.Errorf("the notice is not on screen:\n%s", body)
	}
}
