package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/apps/ssh-cv/internal/cv"
)

// The index.
//
// Roles come first, one row each, because they are what a CV is for and
// because a name and a date are the two things a reader scans for. Everything
// else - what I am best at, education, certifications, skills, the personal
// lines, how to get in touch - sits under one heading below them, in the order
// someone reading downwards would want it.
//
// Group headings are drawn but never selected. The cursor walks items, so
// ↑/↓ and tab can never land on a word that does nothing when opened.
type sectionKind int

const (
	secRole sectionKind = iota
	secBestAt
	secEducation
	secCertifications
	secSkills
	secPersonal
	secContact
)

// item is one row of the index, and the page behind it.
type item struct {
	kind sectionKind
	// role indexes Content.Experience when kind is secRole, and is -1
	// otherwise.
	role int
	// group is the heading this item opens; empty for an item that continues
	// the group above it.
	group string
	// label is the row's text, meta its dim right-hand column.
	label string
	meta  string
	// title is the heading of the page the row opens.
	title string
}

// buildItems lays out the index for one language.
//
// A section with nothing in it is left out entirely rather than opened to an
// empty page - the certifications list is one entry long today and could be
// none tomorrow.
func buildItems(content cv.Content, l labels) []item {
	items := make([]item, 0, len(content.Experience)+6)

	for i, exp := range content.Experience {
		group := ""
		if i == 0 {
			group = l.experience
		}
		items = append(items, item{
			kind:  secRole,
			role:  i,
			group: group,
			label: exp.Role + " · " + exp.Name(),
			meta:  exp.Period,
			title: exp.Role,
		})
	}

	rest := []struct {
		kind  sectionKind
		label string
		show  bool
	}{
		{secBestAt, l.bestAt, len(content.BestAt) > 0},
		{secEducation, l.education, len(content.Education) > 0},
		{secCertifications, l.certifications, len(content.Certifications) > 0},
		{secSkills, l.skills, len(content.Skills) > 0},
		{secPersonal, l.personal, len(content.Spoken)+len(content.Interests) > 0},
		{secContact, l.contact, true},
	}

	first := true
	for _, entry := range rest {
		if !entry.show {
			continue
		}
		group := ""
		if first && len(items) > 0 {
			group = l.more
			first = false
		}
		items = append(items, item{
			kind:  entry.kind,
			role:  -1,
			group: group,
			label: entry.label,
			title: entry.label,
		})
	}
	return items
}

// renderIndex draws the list, and reports which of its lines the cursor is
// on so the model can keep that line in view when the terminal is too short
// to hold the whole index.
func (m Model) renderIndex(width int) (string, int) {
	d := newDoc(m.styles, width)
	cursorLine := 0
	line := 0
	for i, it := range m.items {
		if it.group != "" {
			if line > 0 {
				d.line("")
				line++
			}
			d.line(m.styles.group.Render(truncate(it.group, width)))
			d.line("")
			line += 2
		}
		if i == m.cursor {
			cursorLine = line
		}
		d.line(m.indexRow(it, i == m.cursor, width))
		line++
	}
	return d.String(), cursorLine
}

// indexRow is one row: a cursor, a label, and the period at the far right.
//
// The label is truncated rather than wrapped. A row is one line - that is
// what makes a list scannable - and nothing is lost, because the page it
// opens prints the same name in full.
func (m Model) indexRow(it item, selected bool, width int) string {
	marker := m.styles.pad(2)
	style := m.styles.rowOff
	if selected {
		marker = m.styles.cursor.Render("›") + m.styles.pad(1)
		style = m.styles.rowOn
	}

	room := width - 2
	meta := ""
	if it.meta != "" {
		meta = m.styles.rowKey.Render(it.meta)
		room -= lipgloss.Width(it.meta) + 2
	}
	label := style.Render(truncate(it.label, max(room, 8)))
	return marker + m.styles.ends(width-2, label, meta)
}
