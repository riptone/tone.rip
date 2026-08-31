package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func items() []Item {
	return []Item{
		{Group: "Packages", Label: "brew packages", Status: "11 of 16 present", Selected: true},
		{Group: "Configs", Label: "zsh", Status: "linked", Done: true, Selected: true},
		{Group: "Configs", Label: "ghostty", Status: "not linked", Selected: true},
		{Group: "Secrets", Label: "mssql-envs", Status: "not rendered", Selected: true},
	}
}

func model() Model {
	return New(Config{Items: items(), Version: "v1.0.0", Width: 80, Height: 26,
		Renderer: lipgloss.NewRenderer(nil)})
}

func key(m Model, s string) Model {
	var msg tea.KeyMsg
	switch s {
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

func plain(m Model) string {
	var out strings.Builder
	skip := false
	for _, r := range m.View() {
		switch {
		case r == '\x1b':
			skip = true
		case skip && r == 'm':
			skip = false
		case !skip:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// Every entry has to be on screen. The first version cropped the last one,
// because the row count handed to the geometry was one short of the rows
// actually built - and a menu that silently loses its final option is worse
// than one that does not fit at all.
func TestTheWholeMenuFits(t *testing.T) {
	view := plain(model())
	for _, entry := range menu {
		if !strings.Contains(view, entry.label) {
			t.Errorf("menu entry %q is not on screen:\n%s", entry.label, view)
		}
	}
}

func TestTheCardIsDrawnWithItsChrome(t *testing.T) {
	view := plain(model())
	for _, want := range []string{"╭", "╯", "● ● ●", "doti · v1.0.0", "q quit"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q from the frame:\n%s", want, view)
		}
	}
}

func TestArrowsMoveAndNumbersJump(t *testing.T) {
	m := key(key(model(), "down"), "down")
	if m.menuAt != 2 {
		t.Fatalf("cursor at %d, want 2", m.menuAt)
	}
	if m = key(m, "5"); m.menuAt != 4 {
		t.Fatalf("number key put the cursor at %d, want 4", m.menuAt)
	}
	// The cursor stops rather than wrapping; wrapping in a seven-item list makes
	// "hold down" overshoot silently.
	for range 20 {
		m = key(m, "up")
	}
	if m.menuAt != 0 {
		t.Fatalf("cursor should stop at the top, got %d", m.menuAt)
	}
}

// Install and Adopt ask what to include. The rest act on everything, so a
// selector would be a keypress that never changes the outcome.
func TestOnlyInstallAndAdoptOpenTheSelector(t *testing.T) {
	for i, entry := range menu {
		m := model()
		m.menuAt = i
		m = key(m, "enter")
		opens := m.screen == ScreenSelect
		wants := entry.action == ActionInstall || entry.action == ActionAdopt
		if opens != wants {
			t.Errorf("%s: opened selector = %v, want %v", entry.label, opens, wants)
		}
		if m.action != entry.action {
			t.Errorf("%s: action = %q", entry.label, m.action)
		}
	}
}

func TestTheSelectorShowsEveryItemUnderItsGroup(t *testing.T) {
	m := key(model(), "enter") // Install
	view := plain(m)
	for _, want := range []string{
		"Packages", "Configs", "Secrets",
		"brew packages", "zsh", "ghostty", "mssql-envs",
		"11 of 16 present", "not linked",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("selector missing %q:\n%s", want, view)
		}
	}
}

func TestSpaceTogglesAndTheCountFollows(t *testing.T) {
	m := key(model(), "enter")
	if !strings.Contains(plain(m), "4 of 4") {
		t.Fatalf("everything should start ticked:\n%s", plain(m))
	}
	m = key(m, " ")
	if !strings.Contains(plain(m), "3 of 4") {
		t.Fatalf("toggling should drop the count:\n%s", plain(m))
	}
	if got := m.Chosen(); len(got) != 3 {
		t.Fatalf("Chosen() = %v, want 3", got)
	}
}

func TestAllAndNone(t *testing.T) {
	m := key(key(model(), "enter"), "n")
	if len(m.Chosen()) != 0 {
		t.Fatalf("n should clear everything, got %v", m.Chosen())
	}
	if m = key(m, "a"); len(m.Chosen()) != 4 {
		t.Fatalf("a should select everything, got %v", m.Chosen())
	}
}

// Backing out must not leave an action behind, or the caller runs the
// operation the user just cancelled.
func TestEscapeCancelsTheAction(t *testing.T) {
	m := key(key(model(), "enter"), "esc")
	if m.screen != ScreenMenu {
		t.Error("esc should return to the menu")
	}
	if m.Action() != ActionNone {
		t.Fatalf("action = %q, want none after cancelling", m.Action())
	}
}

func TestEnterOnTheSelectorConfirms(t *testing.T) {
	m := key(key(model(), "enter"), "enter")
	if !m.Confirmed() {
		t.Error("enter should confirm")
	}
	if m.Action() != ActionInstall {
		t.Fatalf("action = %q", m.Action())
	}
}

func TestQuittingConfirmsNothing(t *testing.T) {
	m := key(model(), "q")
	if m.Confirmed() {
		t.Error("quitting must not confirm")
	}
	if view := m.View(); view != "" {
		t.Errorf("a quit model should render nothing, got %q", view)
	}
}

// The frames used for documentation are rendered from the real model, so this
// also proves the capture path keeps working.
func TestFramesRenderWithColour(t *testing.T) {
	frames := Frames(items(), "v1.0.0", 80, 26)
	if len(frames) != 4 {
		t.Fatalf("want 4 frames, got %d", len(frames))
	}
	for _, f := range frames {
		if f.Body == "" {
			t.Errorf("frame %q is empty", f.Name)
		}
		// Without an explicit profile the renderer resolves to Ascii against
		// a non-terminal writer and every colour is silently stripped.
		if !strings.Contains(f.Body, "\x1b[") {
			t.Errorf("frame %q has no ANSI colour", f.Name)
		}
	}
}

func TestNarrowTerminalsStillGetACard(t *testing.T) {
	m := New(Config{Items: items(), Version: "v1", Width: 40, Height: 14,
		Renderer: lipgloss.NewRenderer(nil)})
	view := plain(m)
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╯") {
		t.Errorf("the frame should survive a small terminal:\n%s", view)
	}
}
