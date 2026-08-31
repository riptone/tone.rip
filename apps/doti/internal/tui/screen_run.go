package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
)

// The screen an operation runs inside: its output, scrollable, with the spinner
// on whatever is currently slow and a verdict in the footer when it is over.

// nowFunc is time.Now, indirected so a test can pin the elapsed counter.
var nowFunc = time.Now

// runGeometry is the run screen's card: the full height, because a log is the
// one thing here with more than a screenful.
func (m Model) runGeometry() geometry { return geometryFor(m.width, m.height) }

func (m Model) runKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	// Stopping is a decision, so it has its own key and works only while there
	// is something to stop. The context cancels; the operation returns what it
	// got to, and the lines it already reported stay on screen.
	case key.Matches(msg, m.keys.Stop) && !m.run.settled():
		m.run.job.stop()
		return m, nil

	// A replaced binary is not the one running. Relaunching is the only useful
	// thing left, and main does it by exec'ing the new file.
	case key.Matches(msg, m.keys.Restart) && m.run.updated:
		m.restart = true
		return m, tea.Quit

	// Home and end are handled here rather than left to the viewport, whose
	// default keymap has neither - and because each one is also a statement
	// about following: going to the end means "keep me at the end".
	case key.Matches(msg, m.keys.Top):
		m.run.view.GotoTop()
		m.run.follow = false
		return m, nil
	case key.Matches(msg, m.keys.Bottom):
		m.run.view.GotoBottom()
		m.run.follow = true
		return m, nil

	case (key.Matches(msg, m.keys.Open) || key.Matches(msg, m.keys.Back)) && m.run.settled():
		// Opened on one operation - `doti install` in a terminal - so
		// finishing leaves rather than dropping into a menu nobody asked for.
		if m.launched {
			m.quit = true
			return m, tea.Quit
		}
		m.screen = ScreenMenu
		m.run = runState{spin: m.run.spin}
		return m, nil
	}

	// Everything else is the viewport's: scrolling, paging, home and end. Any
	// of them means the reader is reading, so the tail stops chasing them.
	before := m.run.view.YOffset
	var cmd tea.Cmd
	m.run.view, cmd = m.run.view.Update(msg)
	if m.run.view.YOffset != before {
		m.run.follow = m.run.view.AtBottom()
	}
	return m, cmd
}

// rowsFor renders one record into the rows it occupies at this width.
//
// Built from the stored record rather than from rendered text, so a resize
// re-wraps the run instead of re-flowing arithmetic done at the old width.
//
// Every row leaves here padded to the full text width, which is not cosmetic:
// a bubbles/viewport pads short lines itself, with plain spaces carrying no
// background, and those read as a ragged hole down the right of the log.
func (m Model) rowsFor(line logLine, width int, first bool) []string {
	s := m.styles
	fill := func(rows ...string) []string {
		for i, row := range rows {
			rows[i] = s.chrome.Fill(row, width)
		}
		return rows
	}

	switch line.kind {
	case "phase":
		// A blank above each heading, except the first: the phases are the
		// only structure a long run has.
		if first {
			return fill(s.phase.Render(line.text))
		}
		return fill("", s.phase.Render(line.text))
	case "summary":
		return fill("", s.body.Render(line.text))
	}

	// Two for the indent, one for the glyph, one for the space after it.
	const gutter = 4
	wrapWidth := max(width-gutter, 8)

	// Wrap rather than Wordwrap: Wordwrap leaves a word longer than the limit
	// intact, and the longest thing a run reports is a path -
	// "zsh-shift-select.plugin.zsh" overflowed a 40-column card and pushed the
	// border out with it. Wrap breaks at the same points and hard-breaks what
	// still will not fit.
	wrapped := strings.Split(ansi.Wrap(line.text, wrapWidth, " /-_"), "\n")
	out := make([]string, 0, len(wrapped))
	for i, part := range wrapped {
		if i == 0 {
			out = append(out, s.pad(2)+s.mark(line.mark).Render(app.Glyph(line.mark))+
				s.pad(1)+s.body.Render(part))
			continue
		}
		out = append(out, s.pad(gutter)+s.faint.Render(part))
	}
	return fill(out...)
}

// workingRow is the spinner line, which is not part of rendered: it changes on
// every frame and is then replaced by its own result line.
func (m Model) workingRow(width int) string {
	s := m.styles
	return s.chrome.Fill(s.pad(2)+m.run.spin.View()+s.pad(1)+
		s.faint.Render(m.run.working+" "+elapsed(nowFunc().Sub(m.run.since))), width)
}

func (m Model) viewRun() string {
	g := m.runGeometry()
	s := m.styles

	rows := s.chrome.BodyRows(g, m.run.view.View(),
		m.run.view.YOffset, m.run.view.TotalLineCount())

	return s.chrome.Render(g, pane{
		Name:   m.name() + " · " + strings.ToLower(m.run.label),
		Rows:   rows,
		Hints:  m.runHints(),
		Status: m.runStatus(),
	})
}

// runHints say only what the screen can currently do.
func (m Model) runHints() []hint {
	hints := make([]hint, 0, 4)
	if m.run.view.TotalLineCount() > m.run.view.Height {
		hints = append(hints, hint{Text: "↑/↓ scroll", Keep: 3})
	}
	switch {
	case !m.run.settled():
		hints = append(hints, hint{Text: "ctrl+c stop", Keep: 1})
	case m.run.updated:
		hints = append(hints,
			hint{Text: "r restart", Keep: 0},
			hint{Text: "q quit", Keep: 1})
	case m.launched:
		hints = append(hints, hint{Text: "enter close", Keep: 1})
	default:
		hints = append(hints,
			hint{Text: "enter menu", Keep: 2},
			hint{Text: "q quit", Keep: 1})
	}
	return hints
}

// runStatus is the verdict, at the right of the footer.
//
// The whole point of the "done" the reader was asking for: while it runs this
// counts what has happened, and when it stops it says how it ended - in one
// word, in the place the eye already goes for a line counter.
func (m Model) runStatus() string {
	switch {
	case !m.run.settled():
		return fmt.Sprintf("%d %s", len(m.run.lines), plural(len(m.run.lines), "line"))
	case m.run.err != nil:
		return "failed"
	case m.run.updated:
		return "updated"
	default:
		return "done"
	}
}

// elapsed is a compact duration: seconds under a minute, then m:ss. The same
// shape the plain reporter prints.
func elapsed(d time.Duration) string {
	seconds := int(d.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("(%ds)", seconds)
	}
	return fmt.Sprintf("(%dm%02ds)", seconds/60, seconds%60)
}

// plural is the least ceremony that avoids "1 lines".
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
