package tui

import (
	"context"
	"io"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/packages/gotui"
)

// Captured screens, for the README and for looking at a change without
// installing anything.

// Frame is one captured screen.
type Frame struct {
	Name string
	Body string
}

// press feeds one keypress through the real Update, so a captured frame is the
// screen the program actually draws rather than a mock of it.
func press(m Model, keys ...string) Model {
	for _, k := range keys {
		next, _ := m.Update(keyMsg(k))
		m = next.(Model)
	}
	return m
}

// send feeds one message through the real Update, for the screens that are
// driven by an operation rather than by a key.
func send(m Model, msgs ...tea.Msg) Model {
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
}

// Frames renders the screens worth documenting.
//
// gotui.OfflineRenderer is the reason a capture has colour in it at all: a
// renderer resolves its profile from its writer, and io.Discard is not a
// terminal, so a detected one strips every colour and the screenshot comes out
// monochrome.
func Frames(components []app.Component, version string, width, height int) []Frame {
	cfg := Config{
		Components: components,
		Version:    version,
		Width:      width,
		Height:     height,
		Renderer:   gotui.OfflineRenderer(io.Discard),
		// Never called: the frames below feed their own events, so the run
		// screen is captured without a machine to install onto. It has to be
		// non-nil, because a window with nothing wired says so on screen.
		Run: func(context.Context, Action, []string, RunOptions) error { return nil },
	}

	root := New(cfg)
	// The size the caller asked for, through the resize the real program gets
	// from Bubble Tea on its first frame.
	root = send(root, tea.WindowSizeMsg{Width: width, Height: height})

	frames := []Frame{{Name: "menu", Body: root.View()}}

	// The footer offer, which only exists when the check found something.
	offered := send(root, updateFoundMsg("v0.2.0"))
	frames = append(frames, Frame{Name: "menu-update", Body: offered.View()})

	// Third entry down: Adopt, the one that opens the selector with a
	// machine's real state already filled in.
	onAdopt := press(root, "down", "down")
	frames = append(frames, Frame{Name: "menu-adopt", Body: onAdopt.View()})

	selector := press(onAdopt, "enter")
	frames = append(frames, Frame{Name: "select", Body: selector.View()})

	// Move down a few and untick one, so the cursor and an unchecked box are
	// both visible in the same shot.
	frames = append(frames, Frame{
		Name: "select-toggled",
		Body: press(selector, "down", "down", "down", " ").View(),
	})

	// A run in progress, and the same run finished. These are the screens the
	// old menu could not draw at all, because it had quit by this point.
	running := send(press(selector, "enter"), runFrames()...)
	frames = append(frames, Frame{Name: "run", Body: running.View()})

	finished := send(running,
		eventMsg(app.Record{Kind: "line", Mark: app.MarkChange, Text: "starship   linked 1"}),
		eventMsg(app.Record{Kind: "summary", Text: "3 changed, 12 already in place"}),
		finishedMsg{}, streamDoneMsg{})
	frames = append(frames, Frame{Name: "run-done", Body: finished.View()})

	return frames
}

// runFrames is a run part-way through, in the shape a real one reports: phases,
// marks, and a slow step with the spinner still on it.
func runFrames() []tea.Msg {
	line := func(mark app.Mark, text string) tea.Msg {
		return eventMsg(app.Record{Kind: "line", Mark: mark, Text: text})
	}
	return []tea.Msg{
		eventMsg(app.Record{Kind: "phase", Text: "repository"}),
		line(app.MarkOK, "/Users/you/dotfiles"),
		eventMsg(app.Record{Kind: "phase", Text: "packages"}),
		line(app.MarkOK, "all 16 tools present"),
		line(app.MarkChange, "7 MCP servers installed"),
		eventMsg(app.Record{Kind: "phase", Text: "configs"}),
		line(app.MarkChange, "stow       linked 1"),
		line(app.MarkWarn, "zsh: backing up ~/.zshrc (a real file is in the way)"),
		line(app.MarkChange, "zsh        linked 4, replaced 1, unfolded 3"),
		eventMsg(app.Record{Kind: "working", Text: "brew bundle"}),
	}
}
