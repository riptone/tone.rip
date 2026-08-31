package gotui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The scrollbar, and the body rows it hangs off.

// BodyRows pads a viewport's output to the full body height and hangs the
// scrollbar off the right edge of every row.
//
// The padding is not cosmetic. bubbles/viewport pads short output with plain
// spaces, which carry no background - so every one of them is a hole in the
// black for the rest of that line. Every row leaves here full width and
// painted.
func (c Chrome) BodyRows(g Geometry, view string, offset, total int) []string {
	lines := strings.Split(view, "\n")
	bars := c.Scrollbar(g.Body, total, offset)
	rows := make([]string, 0, g.Body)
	for i := range g.Body {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		// Asked whether the row has any text left in it rather than any
		// bytes: the viewport's own padding is spaces, and a styled empty line
		// is escape sequences around nothing.
		if strings.TrimSpace(ansi.Strip(line)) == "" {
			rows = append(rows, c.Pad(g.Inner-1)+bars[i])
			continue
		}
		rows = append(rows, line+c.Pad(g.Inner-1-lipgloss.Width(line))+bars[i])
	}
	return rows
}

// Scrollbar returns one glyph per body row.
//
// Two parts, like a real one: a thin line the whole height of the body, which
// says the content has a length, and a thicker section on top of it, which
// says where in that length you are. Neither appears when everything fits - a
// scrollbar that is always there tells you nothing, and one that appears only
// when there is more to read is the whole signal.
func (c Chrome) Scrollbar(rows, total, offset int) []string {
	out := make([]string, max(rows, 0))
	for i := range out {
		out[i] = c.Pad(1)
	}
	if rows <= 0 || total <= rows {
		return out
	}

	for i := range out {
		out[i] = c.Track.Render("│")
	}

	size := max(rows*rows/total, 1)
	span := rows - size
	position := 0
	if scrollable := total - rows; scrollable > 0 && span > 0 {
		position = min(max(offset*span/scrollable, 0), span)
	}
	for i := position; i < position+size && i < rows; i++ {
		out[i] = c.Bar.Render("┃")
	}
	return out
}
