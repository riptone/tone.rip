package app

import (
	"fmt"
	"io"
)

// PlainReporter writes lines and nothing else.
//
// Used when stdout is not a terminal - a pipe, a file, CI - where cursor
// movement would be noise in a log and a spinner would be thousands of
// wasted lines.
type PlainReporter struct {
	Out io.Writer
}

func (r PlainReporter) Phase(name string) {
	fmt.Fprintf(r.Out, "\n%s\n", name)
}

func (r PlainReporter) Line(mark Mark, text string) {
	fmt.Fprintf(r.Out, "  %s %s\n", marks[mark], text)
}

func (r PlainReporter) Working(text string) func(Mark, string) {
	fmt.Fprintf(r.Out, "  … %s\n", text)
	return func(mark Mark, result string) { r.Line(mark, result) }
}

func (r PlainReporter) Summary(text string) {
	fmt.Fprintf(r.Out, "\n%s\n", text)
}
