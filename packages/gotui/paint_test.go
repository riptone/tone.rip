package gotui

import (
	"io"
	"strings"
	"testing"
)

func TestUnpaintedFindsARawGap(t *testing.T) {
	s := NewSurface(OfflineRenderer(io.Discard))

	// The right way: every gap through Pad.
	good := s.Base.Foreground(Text).Render("a") + s.Pad(4) + s.Base.Foreground(Text).Render("b")
	if got := Unpainted(good); got != 0 {
		t.Errorf("a properly padded row reports %d unpainted cells:\n%q", got, good)
	}

	// The wrong way, and the bug this exists to catch.
	bad := s.Base.Foreground(Text).Render("a") + strings.Repeat(" ", 4) +
		s.Base.Foreground(Text).Render("b")
	if got := Unpainted(bad); got != 4 {
		t.Errorf("a raw run of spaces reports %d unpainted cells, want 4:\n%q", got, bad)
	}
}

func TestUnpaintedCountsPlainTextAsUnpainted(t *testing.T) {
	if got := Unpainted("hello"); got != 5 {
		t.Errorf("Unpainted(%q) = %d, want 5", "hello", got)
	}
	if got := Unpainted(""); got != 0 {
		t.Errorf("Unpainted(\"\") = %d, want 0", got)
	}
}

// A whole card, which is what the two apps assert against.
func TestARenderedCardHasNoHoles(t *testing.T) {
	chrome := NewChrome(OfflineRenderer(io.Discard), Spec{
		WidthMax: 40, HeightMax: 12, PadX: 2, Gutter: 2,
		WidthMin: 20, HeightMin: 8, MinBody: 4, CompactBelow: 10,
	})
	g := chrome.Geometry(60, 20)
	view := chrome.Render(g, Pane{
		Name:   "test",
		Rows:   []string{chrome.Base.Foreground(Text).Render("a row")},
		Hints:  []Hint{{Text: "q quit", Keep: 1}},
		Status: "ok",
	})
	for i, row := range strings.Split(view, "\n") {
		if n := Unpainted(row); n > 0 {
			t.Errorf("row %d leaves %d cells unpainted:\n%q", i, n, row)
		}
	}
}

// nil means "not a session" - the preview and the tests. It has to produce a
// working surface rather than a nil dereference.
func TestNewSurfaceToleratesNoRenderer(t *testing.T) {
	s := NewSurface(nil)
	if got := s.Pad(3); got == "" {
		t.Error("a default surface cannot pad")
	}
	if got := s.Buttons(); !strings.Contains(got, "●") {
		t.Errorf("a default surface drew no buttons: %q", got)
	}
}
