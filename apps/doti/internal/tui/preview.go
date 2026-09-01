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
	next, _ := sendWithCmd(m, msgs...)
	return next
}

// sendWithCmd is send, keeping the last command Update produced.
//
// For the background work a message sets off - the re-scan a settled run asks
// for - which a test has to be able to reach without calling the handler a
// second time by hand. Doing that was how afterRun's line came out twice.
func sendWithCmd(m Model, msgs ...tea.Msg) (Model, tea.Cmd) {
	var last tea.Cmd
	for _, msg := range msgs {
		next, cmd := m.Update(msg)
		m = next.(Model)
		if cmd != nil {
			last = cmd
		}
	}
	return m, last
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
		Run: func(context.Context, Action, []app.Ref, RunOptions) error { return nil },
	}

	root := New(cfg)
	// The size the caller asked for, through the resize the real program gets
	// from Bubble Tea on its first frame.
	root = send(root, tea.WindowSizeMsg{Width: width, Height: height})

	frames := []Frame{{Name: "menu", Body: root.View()}}

	// The footer offer, which only exists when the check found something.
	offered := send(root, updateFoundMsg("v0.2.0"))
	frames = append(frames, Frame{Name: "menu-update", Body: offered.View()})

	// Third entry down: Adopt, whose selector shows only what is left.
	onAdopt := press(root, "down", "down")
	frames = append(frames, Frame{Name: "menu-adopt", Body: onAdopt.View()})

	// Install's, not Adopt's: this is the capture that has to show the whole
	// machine, and Adopt's list is by definition whatever happens to be missing
	// on the machine the capture was taken from - empty, on a set-up one.
	selector := press(root, "enter")
	frames = append(frames, Frame{Name: "select", Body: selector.View()})

	// A group opened and one thing inside it unticked, so the fold marker, a
	// child row and a parent's partial box are all in the same shot.
	frames = append(frames, Frame{
		Name: "select-toggled",
		Body: press(selector, "right", "down", " ").View(),
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
		line(app.MarkOK, "15 of 16 tools present"),
		line(app.MarkChange, "installing bat"),
		line(app.MarkChange, "7 MCP servers installed"),
		eventMsg(app.Record{Kind: "phase", Text: "configs"}),
		line(app.MarkChange, "stow       linked 1"),
		line(app.MarkWarn, "zsh: backing up ~/.zshrc (a real file is in the way)"),
		line(app.MarkChange, "zsh        linked 4, replaced 1, unfolded 3"),
		eventMsg(app.Record{Kind: "working", Text: "brew bundle"}),
	}
}
