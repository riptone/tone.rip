package gotui

import (
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// How a program in this repository decides what colour it may use, and how it
// asks the terminal to go black with it.
//
// Both questions used to be answered in apps/ssh-cv/main.go alone, which is
// why apps/doti answered neither: it drew the same black card and then let the
// reader's theme show through the emulator's own padding.

// The terminal's own default colours, set for the duration of a program and
// put back afterwards.
//
// OSC 11 is the default background, OSC 10 the default foreground; 111 and 110
// reset them. A program here paints every cell it occupies black already, so
// this is for the one thing painting cannot reach: the emulator's *own*
// chrome. A terminal draws its window padding, its tab bar and its status line
// from the default background, and no amount of drawing inside the grid can
// touch those - which is exactly what a themed terminal shows around a card
// that thinks it is black. Ghostty on tokyonight puts a #1a1b26 navy border
// around it.
//
// A terminal that ignores these is no worse off than before; one that honours
// them goes black to the edges of its window.
const (
	setTerminalColours   = "\x1b]11;#000000\x07\x1b]10;#ffffff\x07"
	resetTerminalColours = "\x1b]111\x07\x1b]110\x07"
)

// PaintTerminal asks the terminal to make its own defaults black, and returns
// the undo.
//
// The undo runs even when the program dies badly - callers defer it - because
// a terminal left black after the program has gone is the program's fault.
// Calling it more than once is harmless.
func PaintTerminal(w io.Writer) (restore func()) {
	if w == nil {
		return func() {}
	}
	_, _ = io.WriteString(w, setTerminalColours)
	var done bool
	return func() {
		if done {
			return
		}
		done = true
		_, _ = io.WriteString(w, resetTerminalColours)
	}
}

// ClampProfile applies the shared floor to a renderer's colour profile.
//
// TERM under-reports constantly. Over SSH it is forwarded and little else, so
// plain `xterm` means sixteen colours to termenv; locally a multiplexer will
// claim `screen` for the same reason. Hex degrades gracefully, but three
// window buttons deserve better than the nearest sixteen, so anything
// reporting fewer than 256 colours is treated as 256.
//
// The floor only ever raises: termenv orders profiles most-colours-first, so
// `> ANSI256` reads as "fewer than 256" and a truecolor terminal keeps
// truecolor. A terminal that says it is `dumb` is taken at its word and gets
// nothing - escape sequences it cannot render are worse than no colour.
func ClampProfile(r *lipgloss.Renderer, term string) *lipgloss.Renderer {
	if term == "" || term == "dumb" {
		r.SetColorProfile(termenv.Ascii)
		return r
	}
	if r.ColorProfile() > termenv.ANSI256 {
		r.SetColorProfile(termenv.ANSI256)
	}
	return r
}

// LocalRenderer builds the renderer for a program drawing on this process's
// own terminal.
//
// Not lipgloss.DefaultRenderer(): that one is a package-level singleton whose
// profile was resolved the first time anything touched it, which in a test or
// behind a pipe is Ascii - and it is shared, so a preview that sets a profile
// changes what the next caller renders with.
func LocalRenderer(out *os.File) *lipgloss.Renderer {
	r := lipgloss.NewRenderer(out, termenv.WithColorCache(true))
	return ClampProfile(r, os.Getenv("TERM"))
}

// OfflineRenderer builds a renderer for something that is not a terminal at
// all: a captured frame, a screenshot, a test.
//
// The profile is set explicitly because a renderer resolves it from its
// writer, and a file or an io.Discard is not a terminal - so without this
// every colour is silently stripped and the capture is a monochrome picture of
// a program that is not monochrome.
func OfflineRenderer(w io.Writer) *lipgloss.Renderer {
	r := lipgloss.NewRenderer(w)
	r.SetColorProfile(termenv.TrueColor)
	return r
}
