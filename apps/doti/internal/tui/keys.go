package tui

import (
	"github.com/charmbracelet/bubbles/key"

	"github.com/riptone/tone.rip/packages/gotui"
)

// The keys, through bubbles/key rather than a switch on msg.String().
//
// The switch was the old shape, and it cost exactly what a hand-rolled version
// costs: the menu had no page keys, no home/end and no vim aliases on the
// select screen, because each one had to be written out again in another case.
// Navigation comes from gotui.Nav, shared with apps/ssh-cv, so "j" means down
// in both.
type keymap struct {
	gotui.Nav

	// Toggle ticks a component. It takes space, which Nav gives to page-down -
	// on a list of checkboxes that is what the hand expects, and the page keys
	// keep pgdown and f.
	Toggle key.Binding
	All    key.Binding
	None   key.Binding
	// Update starts a self-update, and is only ever offered when the check
	// found one.
	Update key.Binding
	// Restart relaunches after a self-update has replaced the binary.
	Restart key.Binding
	// Stop cancels a running operation. Separate from Quit, because ctrl+c
	// during an install should stop the install rather than the program.
	Stop key.Binding
}

func newKeymap() keymap {
	k := keymap{
		Nav:     gotui.NewNav(),
		Toggle:  key.NewBinding(key.WithKeys(" ", "x")),
		All:     key.NewBinding(key.WithKeys("a")),
		None:    key.NewBinding(key.WithKeys("n")),
		Update:  key.NewBinding(key.WithKeys("u")),
		Restart: key.NewBinding(key.WithKeys("r")),
		Stop:    key.NewBinding(key.WithKeys("ctrl+c")),
	}
	// Space belongs to Toggle on this program's lists, so the page keys give
	// it up rather than both claiming it and letting order decide.
	k.PageDown = key.NewBinding(key.WithKeys("pgdown", "f"))
	// Quit is q alone here: ctrl+c is Stop, and a keypress that means "cancel
	// this install" and "close the program" depending on timing is a keypress
	// nobody can trust.
	k.Quit = key.NewBinding(key.WithKeys("q"))
	return k
}
