package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/apps/ssh-cv/internal/authz"
	"github.com/riptone/tone.rip/apps/ssh-cv/internal/cv"
	"github.com/riptone/tone.rip/apps/ssh-cv/internal/version"
)

func testDoc(t *testing.T) *cv.Document {
	t.Helper()
	doc, err := cv.Load()
	if err != nil {
		t.Fatalf("load cv: %v", err)
	}
	return doc
}

func newTestModel(t *testing.T, width, height int, grant authz.Grant) Model {
	t.Helper()
	m := New(Config{
		Doc:    testDoc(t),
		Grant:  grant,
		Width:  width,
		Height: height,
	})
	// Bubble Tea sends this on start, and the real geometry comes from it.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return updated.(Model)
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, name := range keys {
		var msg tea.KeyMsg
		switch name {
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "shift+tab":
			msg = tea.KeyMsg{Type: tea.KeyShiftTab}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEscape}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
		}
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}
	return m
}

// namedRole finds a role that prints its employer's name. Naming them is the
// whole reason this app exists next to /cv, so the tests that check it are
// built on this - and skip rather than fail while every role is still waiting
// for a name to be filled in.
func namedRole(t *testing.T, m Model) (int, cv.Experience) {
	t.Helper()
	for i, exp := range m.content().Experience {
		if exp.Company != "" {
			return i, exp
		}
	}
	t.Skip("no role carries a company name yet")
	return 0, cv.Experience{}
}

// The index is the whole CV in one screen: every role, then everything else.
func TestIndexListsEveryRoleAndSection(t *testing.T) {
	m := newTestModel(t, 100, 40, authz.Grant{})
	content := m.content()

	roles := 0
	for _, it := range m.items {
		if it.kind == secRole {
			roles++
		}
	}
	if roles != len(content.Experience) {
		t.Errorf("index has %d role rows, CV has %d", roles, len(content.Experience))
	}

	for _, kind := range []sectionKind{
		secBestAt, secEducation, secCertifications, secSkills, secPersonal, secContact,
	} {
		found := false
		for _, it := range m.items {
			if it.kind == kind {
				found = true
			}
		}
		if !found {
			t.Errorf("index is missing section kind %d", kind)
		}
	}

	// Roles first: a reader scanning the list should hit the work before the
	// interests.
	if m.items[0].kind != secRole {
		t.Error("the index does not open with a role")
	}
	// And the cursor starts on the newest one rather than on nothing.
	if m.cursor != 0 || m.reading() {
		t.Errorf("session opened at cursor %d, reading %v", m.cursor, m.reading())
	}
}

// Every role row carries its role and its dates, and as much of the
// organisation as the row has room for - a name is short and survives whole;
// the placeholder descriptions standing in for the two missing names are long
// enough to be cut, which is what the page behind the row is for.
func TestIndexRowsCarryRoleAndPeriod(t *testing.T) {
	m := newTestModel(t, 100, 40, authz.Grant{})
	index, _ := m.renderIndex(m.geo.Text)

	for i, exp := range m.content().Experience {
		if !strings.Contains(index, exp.Role) {
			t.Errorf("index row %d does not name the role %q", i, exp.Role)
		}
		if !strings.Contains(index, exp.Period) {
			t.Errorf("index row %d does not show %q", i, exp.Period)
		}
	}
	// The group headings organise it; without them this is a flat list of
	// nine unrelated rows.
	if !strings.Contains(index, m.labels.experience) ||
		!strings.Contains(index, m.labels.more) {
		t.Error("the index is missing its group headings")
	}
}

// Whenever a role does carry a name, the index is where it has to appear:
// those rows are the only place a reader sees every organisation at once.
func TestIndexNamesACompanyWhenThereIsOne(t *testing.T) {
	m := newTestModel(t, 100, 40, authz.Grant{})
	index, _ := m.renderIndex(m.geo.Text)
	if _, named := namedRole(t, m); !strings.Contains(index, named.Company) {
		t.Errorf("the index does not name %q", named.Company)
	}
}

// The role page is the reason for this app: the name, the stack, and the
// detail the website has no room for.
func TestRolePageShowsCompanyStackAndDetail(t *testing.T) {
	m := newTestModel(t, 100, 40, authz.Grant{})
	index, exp := namedRole(t, m)

	page := m.renderSection(m.items[index], m.geo.Text)

	if !strings.Contains(page, exp.Company) {
		t.Errorf("role page does not name %q", exp.Company)
	}
	// The name and what they do, both: knowing it is "Nwest" and knowing it
	// is a digital solutions studio are different facts.
	if !strings.Contains(page, exp.Org) {
		t.Errorf("role page does not say what %q does", exp.Company)
	}
	for _, tech := range exp.Stack {
		if !strings.Contains(page, tech) {
			t.Errorf("role page does not list %q", tech)
		}
	}
	if !strings.Contains(page, m.labels.stack) {
		t.Error("role page has no stack heading")
	}
	if len(exp.Detail) > 0 && !strings.Contains(page, firstWords(exp.Detail[0])) {
		t.Error("role page does not include the detail paragraphs")
	}
}

// A role with no company name must read as what the organisation does, once -
// not as that description printed twice.
func TestUnnamedRoleDoesNotRepeatItself(t *testing.T) {
	m := newTestModel(t, 100, 40, authz.Grant{})
	for i, exp := range m.content().Experience {
		if exp.Company != "" {
			continue
		}
		page := m.renderSection(m.items[i], m.geo.Text)
		if strings.Count(page, exp.Org) > 1 {
			t.Errorf("role %d prints %q %d times", i, exp.Org, strings.Count(page, exp.Org))
		}
	}
}

// Every row must open onto something. An index entry leading to a blank page
// is worse than no entry at all.
func TestEveryPageRendersItsTitleAndBody(t *testing.T) {
	m := newTestModel(t, 100, 40, authz.Grant{})
	for i, it := range m.items {
		page := m.renderSection(it, m.geo.Text)
		if strings.TrimSpace(page) == "" {
			t.Errorf("item %d (%q) renders an empty page", i, it.label)
			continue
		}
		if !strings.Contains(page, firstWords(it.title)) {
			t.Errorf("item %d does not open with its own title %q", i, it.title)
		}
	}
}

// The frame must fit the terminal exactly. One row too many and the title
// scrolls off the top, leaving a second footer behind; one column too many
// and every line wraps into the next.
func TestTheFrameFitsTheTerminal(t *testing.T) {
	for _, size := range [][2]int{
		{80, 24}, {100, 32}, {120, 50}, {200, 60}, {60, 20}, {40, 14}, {34, 11}, {20, 8},
	} {
		width, height := size[0], size[1]
		m := newTestModel(t, width, height, authz.Grant{Label: "laptop"})
		for _, view := range []string{m.View(), press(t, m, "enter").View()} {
			lines := strings.Split(view, "\n")
			if len(lines) > height {
				t.Errorf("%dx%d rendered %d rows, over by %d",
					width, height, len(lines), len(lines)-height)
			}
			for i, line := range lines {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("%dx%d row %d is %d columns wide", width, height, i, w)
					break
				}
			}
		}
	}
}

// The window is centred rather than stretched: a CV read at 200 columns is a
// document on a page, not a line of text across a wall.
func TestTheWindowKeepsItsMeasure(t *testing.T) {
	g := geometryFor(200, 60)
	if g.Width > spec.WidthMax {
		t.Errorf("card width = %d, want at most %d", g.Width, spec.WidthMax)
	}
	view := newTestModel(t, 200, 60, authz.Grant{}).View()
	first := strings.SplitN(view, "\n", 2)[0]
	if !strings.HasPrefix(first, " ") {
		t.Error("the card is not centred - the first row starts at column 0")
	}
}

func TestLanguageToggleSwitchesContentAndChrome(t *testing.T) {
	m := newTestModel(t, 100, 40, authz.Grant{})
	langs := m.cfg.Doc.Langs
	if len(langs) < 2 {
		t.Skip("needs at least two languages")
	}

	before := m.View()
	m = press(t, m, "l")

	if got := m.cfg.Doc.Langs[m.lang]; got != langs[1] {
		t.Fatalf("language = %q, want %q", got, langs[1])
	}
	after := m.View()
	if before == after {
		t.Fatal("toggling language changed nothing")
	}
	// The chrome moves with the content: a Portuguese CV under English
	// headings is the sort of half-translation that looks like a bug. Both
	// halves are checked - the heading the CV carries, and a hint this
	// package owns - because they come from different places now.
	other := m.cfg.Doc.Content(langs[1])
	if !strings.Contains(after, other.Labels.Experience) {
		t.Errorf("the section headings are still in %q", langs[0])
	}
	if !strings.Contains(after, labelsByLang[langs[1]].more) {
		t.Errorf("the key hints and group headings are still in %q", langs[0])
	}
	if !strings.Contains(after, langs[1]) {
		t.Error("the title bar does not name the language")
	}
	// And the language survives into a page.
	page := press(t, m, "enter").View()
	if !strings.Contains(page, firstWords(m.content().Experience[0].Role)) {
		t.Error("the page did not follow the language")
	}
}

// esc means "close this page" while reading and "leave" on the index. Both
// are the only reasonable meaning in their context, and getting it backwards
// either traps the reader or hangs up on them.
func TestEscClosesThePageThenQuits(t *testing.T) {
	m := press(t, newTestModel(t, 100, 40, authz.Grant{}), "enter")
	if !m.reading() {
		t.Fatal("enter did not open a page")
	}

	m = press(t, m, "esc")
	if m.reading() {
		t.Error("esc did not close the page")
	}
	if m.quitted {
		t.Fatal("esc closed the session instead of the page")
	}

	if m = press(t, m, "esc"); !m.quitted {
		t.Error("esc on the index should end the session")
	}
}

// → reads the CV straight through, which is the other way people read a CV:
// not by picking sections but by starting at the top and continuing.
func TestTheArrowsWalkEveryPageInOrder(t *testing.T) {
	m := press(t, newTestModel(t, 100, 40, authz.Grant{}), "enter")
	for i := 1; i < len(m.items); i++ {
		m = press(t, m, "right")
		if !m.reading() {
			t.Fatalf("→ %d left the page view", i)
		}
		if m.open != i {
			t.Fatalf("→ %d opened item %d", i, m.open)
		}
	}
	// And wraps, rather than dead-ending on the last row.
	if m = press(t, m, "right"); m.open != 0 {
		t.Errorf("→ at the end opened item %d, want the first", m.open)
	}
	if m = press(t, m, "left"); m.open != len(m.items)-1 {
		t.Errorf("← at the start opened item %d, want the last", m.open)
	}
}

// → from the index is "forward" too: into the row under the cursor. ← there
// has nothing behind it, and must not be a way to lose the session.
func TestTheArrowsFromTheIndex(t *testing.T) {
	m := press(t, newTestModel(t, 100, 40, authz.Grant{}), "down", "right")
	if !m.reading() || m.open != 1 {
		t.Errorf("→ on the index opened %d, reading %v", m.open, m.reading())
	}

	back := press(t, newTestModel(t, 100, 40, authz.Grant{}), "left")
	if back.reading() || back.quitted {
		t.Error("← on the index should do nothing at all")
	}
}

// tab was the old binding for this and is deliberately unbound now: a key
// that used to move should not silently keep moving.
func TestTabDoesNothing(t *testing.T) {
	m := newTestModel(t, 100, 40, authz.Grant{})
	if after := press(t, m, "tab"); after.cursor != m.cursor || after.reading() {
		t.Error("tab still navigates")
	}
}

// A page opens at its beginning, even when the page before it was left
// halfway down.
func TestOpeningAPageStartsAtTheTop(t *testing.T) {
	m := press(t, newTestModel(t, 80, 24, authz.Grant{}), "enter", "down", "down", "down")
	if m.view.YOffset == 0 {
		t.Skip("nothing scrolled; the page fits")
	}
	if m = press(t, m, "right"); m.view.YOffset != 0 {
		t.Errorf("the next page opened at line %d", m.view.YOffset)
	}
}

// The scroll affordances are the signal that there is more to read, so they
// have to be absent when there is not.
func TestScrollAffordancesOnlyWhenThereIsMore(t *testing.T) {
	short := press(t, newTestModel(t, 100, 24, authz.Grant{}), "enter")
	if !short.scrollable() {
		t.Skip("the first role fits in 24 rows; nothing to assert")
	}
	if !strings.Contains(short.View(), short.status()) || short.status() == "" {
		t.Error("a page that scrolls should say how far through it you are")
	}

	tall := press(t, newTestModel(t, 100, 44, authz.Grant{}), "enter")
	if tall.scrollable() {
		t.Skip("the first role does not fit in 44 rows either")
	}
	if tall.status() != "" {
		t.Errorf("a page that fits counted its lines anyway: %q", tall.status())
	}
	for _, h := range tall.hints() {
		if strings.Contains(h.Text, tall.labels.scroll) {
			t.Error("a page that fits should not offer to scroll")
		}
	}
}

// The scrollbar is two parts: a thin line the height of the body, which says
// the document has a length, and a thicker section on it, which says where in
// that length you are.
func TestScrollbarTracksThePosition(t *testing.T) {
	s := newStyles(nil)

	for _, glyph := range s.scrollbar(10, 8, 0) {
		if strings.TrimSpace(glyph) != "" {
			t.Fatal("a document that fits should have no scrollbar at all")
		}
	}

	top := s.scrollbar(10, 40, 0)
	if len(top) != 10 {
		t.Fatalf("scrollbar returned %d glyphs for 10 rows", len(top))
	}
	for i, glyph := range top {
		if strings.TrimSpace(glyph) == "" {
			t.Errorf("row %d of a long document has no scrollbar line", i)
		}
	}
	if top[0] == top[9] {
		t.Error("at the top of a long document, the top and bottom of the bar look the same")
	}

	bottom := s.scrollbar(10, 40, 30)
	if bottom[9] != top[0] {
		t.Error("the thick section did not reach the bottom")
	}
	if bottom[0] != top[9] {
		t.Error("the thick section did not leave the top")
	}
}

// The index scrolls to follow the cursor. Without this, a 24-row terminal
// hides the last rows and the cursor walks off screen into them.
func TestTheCursorStaysVisible(t *testing.T) {
	m := newTestModel(t, 80, 20, authz.Grant{})
	last := m.items[len(m.items)-1]
	m = press(t, m, "G")

	if !strings.Contains(m.View(), last.label) {
		t.Errorf("the last row (%q) is not on screen", last.label)
	}
	if !strings.Contains(press(t, m, "g").View(), m.items[0].label) {
		t.Error("g did not bring the first row back")
	}
}

// Nothing in the CV is gated, but a recognised key is worth naming - it is
// how you tell which of your own machines you are on.
func TestFooterNamesTheKey(t *testing.T) {
	labelled := newTestModel(t, 100, 40, authz.Grant{Label: "laptop"})
	if !strings.Contains(labelled.View(), "laptop") {
		t.Error("expected the key label in the footer")
	}

	anonymous := New(Config{
		Doc:         testDoc(t),
		Width:       100,
		Height:      40,
		Fingerprint: "SHA256:abcdefghijklmnopqrstuvwxyz0123456789",
	})
	updated, _ := anonymous.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if !strings.Contains(updated.(Model).View(), "SHA256:abcdefghij") {
		t.Error("expected the fingerprint when there is no label")
	}
}

// Keys read in one chunk arrive as a single message. Over SSH that is the
// normal case for anything faster than deliberate typing, and matching the
// whole run against a binding matches nothing at all.
func TestABurstOfKeysIsNotOneKey(t *testing.T) {
	m := newTestModel(t, 100, 40, authz.Grant{})
	burst := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjj")}

	updated, _ := m.Update(burst)
	if got := updated.(Model).cursor; got != 3 {
		t.Errorf("cursor = %d after three js in one message, want 3", got)
	}

	// And a quit anywhere in the run still quits, rather than being typed
	// into a session that has already gone.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jqj")})
	if !updated.(Model).quitted || cmd == nil {
		t.Error("a burst containing q should end the session")
	}
}

func TestQuitRendersNothing(t *testing.T) {
	m := press(t, newTestModel(t, 100, 40, authz.Grant{}), "q")
	if !m.quitted {
		t.Error("q should quit")
	}
	if m.View() != "" {
		t.Error("a quitted model should render nothing")
	}
}

// A model built with no CV at all still has to render: this runs inside a
// session handler, where a panic hangs up on somebody.
func TestAnEmptyDocumentStillRenders(t *testing.T) {
	m := New(Config{Width: 80, Height: 24})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if view := updated.(Model).View(); strings.TrimSpace(view) == "" {
		t.Error("an empty CV rendered nothing at all")
	}
	press(t, updated.(Model), "enter", "tab", "down", "l", "esc")
}

// The version on the Contact page is how an update is confirmed from the
// outside: `ssh cv.tone.rip`, read the last line, done. Nothing else on the
// box has to be reachable for that to work, which is the point - so if this
// stops rendering, the check that scripts/install.sh landed goes with it.
func TestTheContactPageNamesTheVersion(t *testing.T) {
	m := newTestModel(t, 100, 40, authz.Grant{})

	contact := -1
	for i, it := range m.items {
		if it.kind == secContact {
			contact = i
		}
	}
	if contact < 0 {
		t.Fatal("the index has no Contact row")
	}

	// Walked with the same keys a reader has, rather than by assigning to
	// m.open: opening a page is what fills the viewport, and a hand-set field
	// would assert against a page that was never laid out.
	m = press(t, m, "enter")
	for m.open != contact {
		m = press(t, m, "right")
	}
	// The colophon is the last thing on the page, so on a short body it is
	// below the fold until something scrolls there.
	m = press(t, m, "bottom")

	view := stripANSI(m.View())
	if !strings.Contains(view, version.Short()) {
		t.Errorf("the Contact page does not name the version %q", version.Short())
	}
}

// firstWords is enough of a sentence to look for in a wrapped paragraph
// without the assertion depending on where the wrap fell.
func firstWords(text string) string {
	fields := strings.Fields(text)
	if len(fields) > 3 {
		fields = fields[:3]
	}
	return strings.Join(fields, " ")
}
