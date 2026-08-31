package gotui

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Unpainted counts the cells in one rendered row that carry no background at
// all - which is to say the cells where the reader's own terminal shows through
// the black.
//
// Exported because it is a property of this package's contract rather than of
// either program, and because both of them need it: apps/ssh-cv had this check
// and apps/doti did not, which is how doti came to be missing the surrounding
// paint that makes the card look like a window rather than a box.
//
// Two failures look like this. One is a raw run of spaces: an inner style's
// reset ends the background an outer style started, so a gap written as
// strings.Repeat(" ", n) leaves the rest of that line bare, and it reads as
// stripes. The other is the space around the card, which is only black because
// Centre paints it.
//
// Meaningful at TrueColor, where lipgloss writes the background as an explicit
// 48;2;0;0;0 - use OfflineRenderer for the render being measured.
func Unpainted(row string) int {
	painted := false
	count := 0

	for len(row) > 0 {
		if loc := sgrPattern.FindStringIndex(row); loc != nil && loc[0] == 0 {
			code := row[loc[0]:loc[1]]
			switch {
			case strings.Contains(code, blackSGR):
				painted = true
			case code == "\x1b[0m" || code == "\x1b[m":
				painted = false
			}
			row = row[loc[1]:]
			continue
		}
		_, size := utf8.DecodeRuneInString(row)
		if size == 0 {
			break
		}
		row = row[size:]
		if !painted {
			count++
		}
	}
	return count
}

// blackSGR is the black as lipgloss writes it at TrueColor.
const blackSGR = "48;2;0;0;0"

// sgrPattern matches one SGR sequence - the only escape either program emits
// inside a row.
var sgrPattern = regexp.MustCompile(`^\x1b\[[0-9;]*m`)
