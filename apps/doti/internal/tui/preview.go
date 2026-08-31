package tui

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Frame is one captured screen.
type Frame struct {
	Name string
	Body string
}

// press feeds one keypress through the real Update, so a captured frame is
// the screen the program actually draws rather than a mock of it.
func press(m Model, key string) Model {
	var msg tea.KeyMsg
	switch key {
	case "up", "down", "enter", "esc", " ":
		msg = tea.KeyMsg{Type: keyType(key)}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

func keyType(key string) tea.KeyType {
	switch key {
	case "up":
		return tea.KeyUp
	case "down":
		return tea.KeyDown
	case "enter":
		return tea.KeyEnter
	case "esc":
		return tea.KeyEsc
	case " ":
		return tea.KeySpace
	}
	return tea.KeyRunes
}

// Frames renders the screens worth documenting.
//
// The renderer is built with an explicit TrueColor profile because the
// default one is bound to this process's stdout: piped to a file it resolves
// to Ascii and every colour is silently stripped, which is the same trap
// ssh-cv's sessionRenderer exists to avoid - and it would make a screenshot
// of this look like a screenshot of a different program.
func Frames(items []Item, version string, width, height int) []Frame {
	renderer := lipgloss.NewRenderer(io.Discard)
	// SetColorProfile, not the constructor option: the renderer resolves its
	// profile from the writer, and io.Discard is not a terminal - so without
	// this every colour is stripped and the frame is monochrome.
	renderer.SetColorProfile(termenv.TrueColor)
	cfg := Config{
		Items:    items,
		Version:  version,
		Width:    width,
		Height:   height,
		Renderer: renderer,
	}

	menu := New(cfg)
	frames := []Frame{{Name: "menu", Body: menu.View()}}

	// Third entry down: Adopt, the one that opens the selector with a
	// machine's real state already filled in.
	onAdopt := press(press(menu, "down"), "down")
	frames = append(frames, Frame{Name: "menu-adopt", Body: onAdopt.View()})

	selector := press(onAdopt, "enter")
	frames = append(frames, Frame{Name: "select", Body: selector.View()})

	// Move down a few and untick one, so the cursor and an unchecked box are
	// both visible in the same shot.
	moved := press(press(press(selector, "down"), "down"), "down")
	frames = append(frames, Frame{Name: "select-toggled", Body: press(moved, " ").View()})

	return frames
}
