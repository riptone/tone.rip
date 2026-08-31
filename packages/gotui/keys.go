package gotui

import "github.com/charmbracelet/bubbles/key"

// The keys that mean the same thing in both programs.
//
// Through bubbles/key rather than a switch on msg.String(), which is what
// apps/doti did: a binding carries its own aliases, so "down" and "j" are one
// fact in one place instead of two cases that drift apart. apps/doti's menu had
// no page keys, no home/end and no vim aliases on the select screen, purely
// because each had to be written out again.
//
// A program adds its own bindings beside these by embedding Nav - it does not
// redefine what down means.
type Nav struct {
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Top      key.Binding
	Bottom   key.Binding
	Open     key.Binding
	Back     key.Binding
	Quit     key.Binding
}

// NewNav is the shared navigation vocabulary.
//
// Space is a page-down here because that is what it does in a pager, and a
// program that gives it another job - ticking a checkbox, say - overrides the
// binding rather than working around it.
func NewNav() Nav {
	return Nav{
		Up:       key.NewBinding(key.WithKeys("up", "k")),
		Down:     key.NewBinding(key.WithKeys("down", "j")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "b")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", " ", "f")),
		Top:      key.NewBinding(key.WithKeys("home", "g")),
		Bottom:   key.NewBinding(key.WithKeys("end", "G")),
		Open:     key.NewBinding(key.WithKeys("enter")),
		Back:     key.NewBinding(key.WithKeys("esc", "backspace")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c")),
	}
}
