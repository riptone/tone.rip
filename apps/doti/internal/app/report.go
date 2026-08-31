// Package app holds what doti's commands actually do.
//
// It lives here rather than in package main for two reasons. The first is
// that package main had grown to 773 lines with no tests, because a func in
// main is not reachable from one. The second is the interesting one:
// commands here do not print. They report.
//
// That indirection is what makes `doti install`, `doti install --term` and the
// menu's Install identical rather than merely similar. All three call the same
// function with the same Reporter, so there is no second code path to keep in
// step - and the rendering is chosen once, from whether anything is watching.
//
// Nothing in this package imports a UI. The reporters here write bytes or fill
// a channel; internal/tui is what turns the channel into a window.
package app

// Mark is the glyph a line carries, and what it means.
type Mark int

const (
	// MarkNone is a plain line with no verdict.
	MarkNone Mark = iota
	// MarkOK is "already the way it should be" - nothing was done.
	MarkOK
	// MarkChange is "this run changed it".
	MarkChange
	// MarkSkip is "deliberately not done here".
	MarkSkip
	// MarkWarn is "did not work, and the run continued anyway".
	MarkWarn
)

// Reporter renders progress. Commands hold one and never touch a writer, so
// the same command can be run interactively, piped to a file, or asserted
// against in a test without knowing the difference.
type Reporter interface {
	// Phase begins a named stage: "packages", "configs", "secrets".
	Phase(name string)
	// Line records one outcome inside the current phase.
	Line(mark Mark, text string)
	// Working announces something slow and returns the function that ends
	// it. Implementations may animate between the two calls; callers must
	// call the returned function exactly once, and must not report anything
	// else in between.
	Working(text string) func(mark Mark, result string)
	// Summary closes the command out.
	Summary(text string)
}

// marks are the glyphs, chosen to line up in a column: every one is a single
// terminal cell, so a run of lines does not ripple sideways.
var marks = map[Mark]string{
	MarkNone:   " ",
	MarkOK:     "·",
	MarkChange: "+",
	MarkSkip:   "-",
	MarkWarn:   "!",
}

// Glyph is the mark's glyph, for anything that renders a reported line - the
// plain reporter, the live one, and the window apps/doti draws.
//
// Exported because the TUI renders the same events, and a second copy of this
// table is a second answer to "what does a changed line look like".
func Glyph(mark Mark) string { return marks[mark] }
