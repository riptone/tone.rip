package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// doc builds the body of one section.
//
// Every section used to render its own text with its own indents and its own
// idea of spacing, which is how a CV that fits on one page turned into six
// documents that happened to share a terminal. This is the vocabulary they
// all draw from now: a title, a meta line, headings, paragraphs, bullets,
// aligned rows, inline lists. Each has exactly one definition of how it looks
// and how it wraps, so consistency is the default rather than a review note.
type doc struct {
	sb    strings.Builder
	s     styles
	width int
	// blank tracks whether the last line written was empty, so blocks can ask
	// for separation without any of them counting newlines.
	blank bool
}

func newDoc(s styles, width int) *doc {
	// A viewport this narrow is already broken; wrapping to it at least keeps
	// the words on screen.
	if width < 16 {
		width = 16
	}
	return &doc{s: s, width: width}
}

// String is the finished body, with no trailing blank lines - they would
// count towards the scroll length and make a section look longer than it is.
func (d *doc) String() string {
	return strings.TrimRight(d.sb.String(), "\n")
}

// line writes one row of the section, padded out to the full text width.
//
// The padding is the point: it means the viewport never has to pad a line
// itself, and the viewport pads with plain spaces that carry no background -
// which would show as a ragged hole down the right of every short line. See
// the note on the palette in theme.go.
func (d *doc) line(text string) {
	d.blank = text == ""
	if pad := d.width - lipgloss.Width(text); pad > 0 {
		text += d.s.pad(pad)
	}
	d.sb.WriteString(text + "\n")
}

// gap separates two blocks, and does nothing at the top of a section or where
// something already separated them.
func (d *doc) gap() {
	if d.sb.Len() == 0 || d.blank {
		return
	}
	d.line("")
}

// title is the section's name, the first line of every page.
func (d *doc) title(text string) {
	d.line(d.s.title.Render(truncate(text, d.width)))
}

// meta is the dim line under a title: dates, places, everything that is a
// label rather than a sentence.
func (d *doc) meta(text string) {
	for _, line := range wrapText(text, d.width) {
		d.line(d.s.meta.Render(line))
	}
}

// heading opens a block inside a section, with air on both sides.
func (d *doc) heading(text string) {
	d.gap()
	d.line(d.s.heading.Render(truncate(text, d.width)))
	d.line("")
}

// term is a bold key with its expansion underneath - the shape "best at"
// wants, where the key is the claim and the line below it is the evidence.
func (d *doc) term(text string) {
	d.gap()
	d.line(d.s.term.Render(truncate(text, d.width)))
}

// para is a wrapped paragraph, separated from whatever came before it.
func (d *doc) para(text string) {
	d.gap()
	d.wrapped(text)
}

// expansion is the paragraph directly under a term, with no gap between them:
// a claim and its evidence are one block, not two.
func (d *doc) expansion(text string) {
	d.wrapped(text)
}

// note is a faint aside - a colophon, a caveat, anything a reader may skip
// without losing the CV.
func (d *doc) note(text string) {
	d.gap()
	for _, line := range wrapText(text, d.width) {
		d.line(d.s.faint.Render(line))
	}
}

func (d *doc) wrapped(text string) {
	for _, line := range wrapText(text, d.width) {
		d.line(d.s.body.Render(line))
	}
}

// bullet is one item of a tight list: no blank lines between siblings, and
// continuations hang under the text rather than under the marker.
func (d *doc) bullet(text string) {
	for i, line := range wrapText(text, d.width-2) {
		marker := "· "
		if i > 0 {
			marker = "  "
		}
		d.line(d.s.body.Render(marker + line))
	}
}

// row is one line of a two-column table: a faint key, then its value, with
// the value wrapping into its own column instead of under the key.
func (d *doc) row(key, value string, keyWidth int) {
	gap := d.s.pad(max(keyWidth-lipgloss.Width(key), 0) + 2)
	indent := d.s.pad(keyWidth + 2)
	for i, line := range wrapText(value, max(d.width-keyWidth-2, 12)) {
		if i == 0 {
			d.line(d.s.rowKey.Render(key) + gap + d.s.body.Render(line))
			continue
		}
		d.line(indent + d.s.body.Render(line))
	}
}

// inline lays a list out as one wrapped run of dot-separated items.
//
// The previous version drew these as bordered pills. They looked like a
// component library rather than a CV, and every one of them was three rows
// tall for one word of content.
func (d *doc) inline(items []string) {
	const sep = " · "
	var plain, styled string
	flush := func() {
		if plain == "" {
			return
		}
		d.line(styled)
		plain, styled = "", ""
	}
	for _, item := range items {
		candidate := item
		if plain != "" {
			candidate = plain + sep + item
		}
		if lipgloss.Width(candidate) > d.width && plain != "" {
			flush()
			plain, styled = item, d.s.body.Render(item)
			continue
		}
		if plain != "" {
			styled += d.s.faint.Render(sep)
		}
		plain, styled = candidate, styled+d.s.body.Render(item)
	}
	flush()
}

// wrapText greedily breaks text at word boundaries. lipgloss.Width is used
// rather than len so wide glyphs and accents measure as they render.
func wrapText(text string, limit int) []string {
	if limit < 1 {
		limit = 1
	}
	var lines []string
	current := ""
	for _, word := range strings.Fields(text) {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if lipgloss.Width(candidate) > limit && current != "" {
			lines = append(lines, current)
			current = word
		} else {
			current = candidate
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// truncate shortens text to fit on one line, saying so with an ellipsis.
//
// Only the index and the window chrome do this. A section body wraps instead:
// cutting a sentence loses information, while cutting an index row only
// delays it until the page it points at.
func truncate(text string, limit int) string {
	if limit < 1 {
		return ""
	}
	if lipgloss.Width(text) <= limit {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > limit {
		runes = runes[:len(runes)-1]
	}
	return strings.TrimRight(string(runes), " ") + "…"
}
