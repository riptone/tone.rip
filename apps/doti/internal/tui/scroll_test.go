package tui

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/packages/gotui"
)

// The menu and the selector handed every row to the frame, which draws as many
// as the body has and drops the rest - so `G` moved the cursor onto an item
// nobody could see, and space then toggled it.

func TestWindow(t *testing.T) {
	rows := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}

	for _, tc := range []struct {
		name       string
		cursor     int
		height     int
		wantFirst  string
		wantOffset int
	}{
		// A list that fits does not move at all.
		{"everything fits", 9, 10, "0", 0},
		{"more room than rows", 3, 20, "0", 0},
		// Near the top, nothing scrolls.
		{"cursor at the top", 0, 4, "0", 0},
		{"cursor still visible", 3, 4, "0", 0},
		// Stepping off the bottom edge advances by one row, not half a page.
		{"one past the edge", 4, 4, "1", 1},
		{"two past the edge", 5, 4, "2", 2},
		{"the last row", 9, 4, "6", 6},
		// A cursor past the end would slice out of range.
		{"cursor past the end", 99, 4, "6", 6},
		{"negative cursor", -5, 4, "0", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, offset := window(rows, tc.cursor, tc.height)
			if offset != tc.wantOffset {
				t.Errorf("offset = %d, want %d", offset, tc.wantOffset)
			}
			if len(got) == 0 {
				t.Fatal("no rows")
			}
			if got[0] != tc.wantFirst {
				t.Errorf("first row = %q, want %q", got[0], tc.wantFirst)
			}
			if tc.height < len(rows) && len(got) != tc.height {
				t.Errorf("returned %d rows for a height of %d", len(got), tc.height)
			}
			// The cursor has to be inside what came back. That is the whole job.
			if tc.cursor >= 0 && tc.cursor < len(rows) {
				want := rows[tc.cursor]
				var found bool
				for _, row := range got {
					if row == want {
						found = true
					}
				}
				if !found {
					t.Errorf("row %q is not in the window %v", want, got)
				}
			}
		})
	}
}

func TestWindowSurvivesDegenerateHeights(t *testing.T) {
	rows := []string{"a", "b", "c"}
	for _, height := range []int{0, -1, -100} {
		got, offset := window(rows, 2, height)
		if offset != 0 || len(got) != len(rows) {
			t.Errorf("height %d gave %d rows at offset %d", height, len(got), offset)
		}
	}
	if got, _ := window(nil, 0, 5); got != nil {
		t.Errorf("an empty list gave %v", got)
	}
}

// A long component list has to scroll with the cursor rather than lose its tail.
func TestTheSelectorScrollsWithTheCursor(t *testing.T) {
	many := make([]app.Component, 0, 30)
	for i := range 30 {
		many = append(many, app.Component{
			Group: "Configs", Label: fmt.Sprintf("package-%02d", i),
			Status: "not linked", Selected: true,
		})
	}
	m := New(Config{
		Components: many, Width: 100, Height: 24,
		Renderer: gotui.OfflineRenderer(io.Discard), Run: noWork,
	})

	top := tap(m, "enter")
	if !strings.Contains(plain(top), "package-00") {
		t.Fatalf("the first component is not on screen:\n%s", plain(top))
	}
	// A list that does not fit says so.
	if !strings.Contains(top.View(), "┃") {
		t.Error("no scrollbar on a component list that does not fit")
	}

	end := tap(top, "G")
	body := plain(end)
	last := many[len(many)-1].Label
	if !strings.Contains(body, last) {
		t.Errorf("the cursor is on %q and it is not on screen:\n%s", last, body)
	}
	// And the cursor marker is with it.
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, last) && !strings.Contains(line, "›") {
			t.Errorf("the last row is shown without the cursor on it: %q", line)
		}
	}

	// Back to the top, and the first is visible again.
	if !strings.Contains(plain(tap(end, "g")), "package-00") {
		t.Error("g did not scroll back to the first component")
	}

	// Every row still exactly the terminal width, and painted.
	for _, view := range []string{top.View(), end.View()} {
		for i, row := range strings.Split(view, "\n") {
			if got := ansi.StringWidth(row); got != 100 {
				t.Errorf("row %d is %d columns, want 100", i, got)
			}
			if n := gotui.Unpainted(row); n > 0 {
				t.Errorf("row %d leaves %d cells unpainted", i, n)
			}
		}
	}
}

// Eight menu entries fit any normal terminal and do not fit a twelve-row one,
// where the frame silently dropped the last three.
func TestTheMenuScrollsOnAShortTerminal(t *testing.T) {
	m := New(Config{
		Components: components(), Width: 80, Height: 12,
		Renderer: lipgloss.NewRenderer(io.Discard), Run: noWork,
	})
	last := menu[len(menu)-1].label

	end := tap(m, "G")
	if !strings.Contains(plain(end), last) {
		t.Errorf("the cursor is on %q and it is not on screen:\n%s", last, plain(end))
	}
	if !strings.Contains(plain(tap(end, "g")), menu[0].label) {
		t.Errorf("g did not scroll back:\n%s", plain(tap(end, "g")))
	}
}

// And a card with room for everything does not scroll, because a scrollbar that
// is always there tells you nothing.
func TestNeitherScrollsWhenEverythingFits(t *testing.T) {
	m := New(Config{
		Components: components(), Width: 100, Height: 40,
		Renderer: gotui.OfflineRenderer(io.Discard), Run: noWork,
	})
	for name, view := range map[string]string{
		"menu":     m.View(),
		"selector": tap(m, "enter").View(),
	} {
		if strings.Contains(view, "┃") {
			t.Errorf("the %s has a scrollbar with room to spare", name)
		}
	}
}

// Up from the first row is the last one, and down from the last is the first -
// the arithmetic apps/ssh-cv uses. A list you can only leave by pressing the
// other arrow eight times is a list that ignores you.
func TestStepWrapsAtBothEnds(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		cursor, delta, length int
		want                  int
	}{
		{"down the middle", 2, 1, 8, 3},
		{"up the middle", 2, -1, 8, 1},
		{"down off the end", 7, 1, 8, 0},
		{"up off the top", 0, -1, 8, 7},
		{"a single entry stays put going down", 0, 1, 1, 0},
		{"a single entry stays put going up", 0, -1, 1, 0},
		// The removal selector is empty on a machine with nothing to remove,
		// and a modulo by zero panics.
		{"an empty list does not move", 0, 1, 0, 0},
		{"an empty list does not move backwards", 0, -1, 0, 0},
		{"a negative length is not a list", 3, 1, -2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := step(tc.cursor, tc.delta, tc.length); got != tc.want {
				t.Errorf("step(%d, %d, %d) = %d, want %d",
					tc.cursor, tc.delta, tc.length, got, tc.want)
			}
		})
	}
}

// Whatever it is handed, it comes back a usable index.
func TestStepAlwaysReturnsAnIndexInRange(t *testing.T) {
	for _, length := range []int{1, 3, 8} {
		for cursor := range length {
			for _, delta := range []int{-3, -1, 1, 3} {
				got := step(cursor, delta, length)
				if got < 0 || got >= length {
					t.Errorf("step(%d, %d, %d) = %d, outside 0..%d",
						cursor, delta, length, got, length-1)
				}
			}
		}
	}
}

// The menu wraps, and it is the thing the reader actually touches.
func TestTheMenuCursorWraps(t *testing.T) {
	m := model()
	last := len(menu) - 1

	if got := tap(m, "up").menuAt; got != last {
		t.Errorf("up from the first entry gave %d, want the last (%d)", got, last)
	}
	if got := tap(m, "G", "down").menuAt; got != 0 {
		t.Errorf("down from the last entry gave %d, want the first", got)
	}
	// Home and end still mean the first and the last, not one step past them.
	if got := tap(m, "G").menuAt; got != last {
		t.Errorf("G gave %d, want %d", got, last)
	}
	if got := tap(m, "G", "g").menuAt; got != 0 {
		t.Errorf("g gave %d, want 0", got)
	}
}

// And so does the selector, because a menu that wraps beside a list that does
// not is its own inconsistency.
func TestTheSelectorCursorWraps(t *testing.T) {
	m := tap(model(), "enter")
	last := len(m.items) - 1

	if got := tap(m, "up").itemAt; got != last {
		t.Errorf("up from the first component gave %d, want %d", got, last)
	}
	if got := tap(m, "G", "down").itemAt; got != 0 {
		t.Errorf("down from the last component gave %d, want 0", got)
	}
}

// An empty list has nowhere to wrap to, and must not panic trying.
func TestAnEmptySelectorDoesNotMove(t *testing.T) {
	m := openRemoval(t, removeModel(func(c *Config) { c.Removable = nil }))
	for _, key := range []string{"up", "down", "G", "g"} {
		if got := tap(m, key).itemAt; got != 0 {
			t.Errorf("%s on an empty list moved to %d", key, got)
		}
	}
}
