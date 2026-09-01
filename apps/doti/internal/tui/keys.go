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
	// Help opens the help screen, and closes it again.
	Help key.Binding
	// Unfold and Fold open and close a group on the selector; FoldToggle does
	// whichever applies.
	//
	// The arrows are what a tree uses everywhere, and tab is the one key that
	// always does something - for the reader who has not worked out which
	// arrow. `h` and `l` would have been the vim pair, but `h` is help and a
	// key that means two things depending on the screen is a key nobody trusts.
	Unfold     key.Binding
	Fold       key.Binding
	FoldToggle key.Binding
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
		Help:    key.NewBinding(key.WithKeys("h", "?")),
		Stop:    key.NewBinding(key.WithKeys("ctrl+c")),

		Unfold:     key.NewBinding(key.WithKeys("right", "l")),
		Fold:       key.NewBinding(key.WithKeys("left")),
		FoldToggle: key.NewBinding(key.WithKeys("tab")),
	}
	// Space belongs to Toggle on this program's lists, so the page keys give
	// it up rather than both claiming it and letting order decide.
	k.PageDown = key.NewBinding(key.WithKeys("pgdown", "f"))
	// esc quits as well as going back, which is what apps/ssh-cv does: it
	// closes a page from inside one and closes the session from the index. Both
	// bindings carry it and the dispatch in Model.key decides, so Back wins
	// wherever there is somewhere to go back to.
	//
	// ctrl+c is deliberately absent: it is Stop here, and a keypress meaning
	// "cancel this install" or "close the program" depending on timing is a
	// keypress nobody can trust.
	k.Quit = key.NewBinding(key.WithKeys("q", "esc"))
	return k
}
