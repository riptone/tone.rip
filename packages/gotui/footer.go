package gotui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The footer: key hints at the left, a status at the right, and everything
// competing for one row.

// Hint is one key legend, and how hard it fights to stay on screen.
//
// Display order and drop order are deliberately different. "↑/↓ scroll" reads
// best first, but "q quit" has to outlive "←/→ section": a reader who cannot
// page around is inconvenienced, and a reader who cannot see how to leave is
// trapped in somebody else's program.
type Hint struct {
	Text string
	// Keep is the drop order - lower survives longer.
	Keep int
}

// StatusRank is where the status sits in the same order the hints are ranked
// by: it outlives a language switch, because a reader who cannot tell there is
// more to read stops reading, and it is outlived by everything that says which
// key to press.
const StatusRank = 4

// FooterRow lays the footer out, dropping whatever will not fit.
//
// Dropped by rank rather than by position, so a 46-column terminal keeps
// "↑/↓ scroll · esc back · q quit" and loses the counter, while a 60-column one
// keeps the counter and loses the least important hint. What it must never do
// is show the arithmetic and no way out.
func (c Chrome) FooterRow(g Geometry, hints []Hint, status string) string {
	join := func(kept []Hint) string {
		parts := make([]string, 0, len(kept))
		for _, h := range kept {
			parts = append(parts, h.Text)
		}
		return strings.Join(parts, " · ")
	}

	kept := append([]Hint(nil), hints...)
	showStatus := status != ""
	width := func() int {
		total := lipgloss.Width(join(kept))
		if showStatus {
			total += lipgloss.Width(status) + 2
		}
		return total
	}

	for width() > g.Inner {
		worst := -1
		for i, h := range kept {
			if worst < 0 || h.Keep > kept[worst].Keep {
				worst = i
			}
		}
		if showStatus && (worst < 0 || StatusRank > kept[worst].Keep) {
			showStatus = false
			continue
		}
		if worst < 0 || len(kept) <= 1 {
			break
		}
		kept = append(kept[:worst], kept[worst+1:]...)
	}

	// Truncated before it is styled: cutting a string that already carries
	// escape sequences cuts one of them in half.
	left := c.Footer.Render(Truncate(join(kept), g.Inner))
	if !showStatus {
		return left
	}
	return c.Ends(g.Inner, left, c.Footer.Render(status))
}
