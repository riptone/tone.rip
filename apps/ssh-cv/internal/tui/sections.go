package tui

import (
	"strings"

	"github.com/riptone/tonil/apps/ssh-cv/internal/cv"
	"github.com/riptone/tonil/apps/ssh-cv/internal/version"
)

// One page per index row.
//
// Each of these is a composition of doc's blocks and nothing else: no section
// invents an indent, a colour or a spacing rule of its own, which is why they
// read as chapters of one document rather than as six screens that happen to
// share a border.
func (m Model) renderSection(it item, width int) string {
	content := m.content()
	l := m.labels
	d := newDoc(m.styles, width)

	switch it.kind {
	case secRole:
		if it.role < 0 || it.role >= len(content.Experience) {
			return ""
		}
		exp := content.Experience[it.role]
		d.title(exp.Role)
		d.meta(orgLine(exp))
		if len(exp.Stack) > 0 {
			d.heading(l.stack)
			d.inline(exp.Stack)
		}
		if len(exp.Bullets) > 0 {
			d.heading(l.work)
			for _, bullet := range exp.Bullets {
				d.bullet(bullet)
			}
		}
		// The part the website has no room for: what the work actually
		// involved, in sentences rather than in a summary line.
		if len(exp.Detail) > 0 {
			d.heading(l.depth)
			for _, paragraph := range exp.Detail {
				d.para(paragraph)
			}
		}

	case secBestAt:
		d.title(l.bestAt)
		d.line("")
		for _, row := range content.BestAt {
			d.term(row.K)
			d.expansion(row.V)
		}

	case secEducation:
		d.title(l.education)
		d.line("")
		for _, edu := range content.Education {
			d.term(edu.Title)
			d.meta(edu.Period)
			for _, bullet := range edu.Bullets {
				d.bullet(bullet)
			}
		}

	case secCertifications:
		d.title(l.certifications)
		d.line("")
		for _, cert := range content.Certifications {
			d.term(cert.Title)
			d.expansion(cert.Note)
		}

	case secSkills:
		d.title(l.skills)
		for _, group := range content.Skills {
			d.heading(group.Label)
			d.inline(group.Items)
		}

	case secPersonal:
		d.title(l.personal)
		if len(content.Spoken) > 0 {
			d.heading(l.spoken)
			d.inline(content.Spoken)
		}
		if len(content.Interests) > 0 {
			d.heading(l.interests)
			d.inline(content.Interests)
		}

	case secContact:
		d.title(l.contact)
		d.line("")
		contact := m.cfg.Doc.Contact
		// The widest key, so the values line up into a column. Hard-coded
		// because these four rows are the whole table.
		const keyWidth = 6
		for _, row := range [][2]string{
			{"web", contact.Web},
			{"email", contact.Email},
			{"github", contact.GitHub},
			{"ssh", contact.SSH},
		} {
			if row[1] == "" {
				continue
			}
			d.row(row[0], row[1], keyWidth)
		}
		// The version rides on the colophon rather than getting a line of its
		// own: it is the same sentence's worth of "what you are talking to",
		// and it means `ssh cv.tone.rip` confirms an update landed without
		// anyone opening a shell on the box.
		d.note(l.colophon + " " + version.Short())
	}

	return d.String()
}

// orgLine is the dim line under a role: who, what they do, where, when.
//
// Name() falls back to the description when there is no company name, so
// printing both unconditionally would print the description twice for any
// role still waiting for its name.
func orgLine(exp cv.Experience) string {
	parts := []string{exp.Name()}
	if exp.Company != "" && exp.Org != "" {
		parts = append(parts, exp.Org)
	}
	parts = append(parts, exp.Place, exp.Period)

	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " · ")
}
