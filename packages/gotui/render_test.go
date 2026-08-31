package gotui

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestPaintTerminalSetsAndRestoresTheDefaults(t *testing.T) {
	var out strings.Builder
	restore := PaintTerminal(&out)

	set := out.String()
	// OSC 11 is the background and OSC 10 the foreground. Asserted by number
	// rather than by whole string so the black stays readable as a colour.
	for _, want := range []string{"\x1b]11;#000000", "\x1b]10;#ffffff"} {
		if !strings.Contains(set, want) {
			t.Errorf("set = %q, want it to contain %q", set, want)
		}
	}

	out.Reset()
	restore()
	for _, want := range []string{"\x1b]111", "\x1b]110"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("restore = %q, want it to contain %q", out.String(), want)
		}
	}
}

// Callers defer the undo and may also call it on a clean exit path. Writing
// the reset twice would be harmless on a terminal and confusing in a capture.
func TestRestoringTwiceWritesOnce(t *testing.T) {
	var out strings.Builder
	restore := PaintTerminal(&out)
	restore()
	first := out.Len()
	restore()
	if out.Len() != first {
		t.Errorf("the second restore wrote %d more bytes", out.Len()-first)
	}
}

func TestPaintTerminalToleratesNoWriter(t *testing.T) {
	// A program with nowhere to write should not be the reason it crashes.
	PaintTerminal(nil)()
}

func TestClampProfile(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start termenv.Profile
		term  string
		want  termenv.Profile
	}{
		// The floor only raises. termenv orders profiles most-colours-first,
		// so a truecolor terminal must come out unchanged rather than capped.
		{"truecolor is left alone", termenv.TrueColor, "xterm-256color", termenv.TrueColor},
		{"256 is left alone", termenv.ANSI256, "screen-256color", termenv.ANSI256},
		{"sixteen colours are raised", termenv.ANSI, "xterm", termenv.ANSI256},
		{"none is raised", termenv.Ascii, "screen", termenv.ANSI256},
		// Taken at its word: sequences it cannot render are worse than grey.
		{"dumb gets nothing", termenv.TrueColor, "dumb", termenv.Ascii},
		{"no TERM gets nothing", termenv.TrueColor, "", termenv.Ascii},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := lipgloss.NewRenderer(io.Discard)
			r.SetColorProfile(tc.start)
			if got := ClampProfile(r, tc.term).ColorProfile(); got != tc.want {
				t.Errorf("profile = %v, want %v", got, tc.want)
			}
		})
	}
}

// The trap this exists for: a renderer resolves its profile from its writer,
// and nothing being captured to is ever a terminal - so the default renderer
// strips every colour and a screenshot comes out monochrome.
func TestOfflineRendererStillPaints(t *testing.T) {
	styled := NewSurface(OfflineRenderer(io.Discard)).Base.Foreground(Accent).Render("x")
	if !strings.Contains(styled, "\x1b[") {
		t.Fatalf("offline render produced no escape sequences: %q", styled)
	}
	if lipgloss.NewRenderer(io.Discard).ColorProfile() == termenv.TrueColor {
		t.Skip("this writer detects as truecolor, so the trap cannot be shown")
	}
	plain := NewSurface(lipgloss.NewRenderer(io.Discard)).Base.Foreground(Accent).Render("x")
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("a detected renderer painted into a non-terminal: %q", plain)
	}
}
