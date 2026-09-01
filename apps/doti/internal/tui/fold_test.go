package tui

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
)

// Folding: what is on screen, where the cursor can go, and a parent that never
// disagrees with what is under it.

// tools is the fixture's tool children, by label.
var tools = []string{"jq", "fd"}

func openInstall(t *testing.T) Model {
	t.Helper()
	m := tap(model(), "enter")
	if m.screen != ScreenSelect {
		t.Fatalf("screen = %v", m.screen)
	}
	return m
}

// Folded, so the group reads exactly as it did when each list was one row -
// which is the whole reason offering sixteen tools individually costs nothing.
func TestGroupsStartFolded(t *testing.T) {
	body := plain(openInstall(t))
	if !strings.Contains(body, "brew packages") {
		t.Fatalf("no parent on screen:\n%s", body)
	}
	for _, child := range tools {
		if rowFor(body, child) != "" {
			t.Errorf("%q is on screen before anything was opened:\n%s", child, body)
		}
	}
	if !strings.Contains(body, foldClosed) {
		t.Errorf("no closed marker:\n%s", body)
	}
}

func TestRightOpensAGroupAndLeftClosesIt(t *testing.T) {
	m := openInstall(t)
	opened := plain(tap(m, "right"))
	for _, child := range tools {
		if rowFor(opened, child) == "" {
			t.Errorf("%q is still hidden after right:\n%s", child, opened)
		}
	}
	if !strings.Contains(opened, foldOpen) {
		t.Errorf("no open marker:\n%s", opened)
	}

	closed := plain(tap(m, "right", "left"))
	for _, child := range tools {
		if rowFor(closed, child) != "" {
			t.Errorf("%q survived left:\n%s", child, closed)
		}
	}
}

// tab is the one key that always does something, for the reader who has not
// worked out which arrow.
func TestTabOpensAndClosesTheSameGroup(t *testing.T) {
	m := openInstall(t)
	if rowFor(plain(tap(m, "tab")), "jq") == "" {
		t.Error("tab did not open the group")
	}
	if rowFor(plain(tap(m, "tab", "tab")), "jq") != "" {
		t.Error("tab did not close it again")
	}
}

// Left on a child closes the thing it is inside, which is what the arrow means
// in every tree anybody has used - and the cursor follows it up, because leaving
// it on a row that is no longer drawn is the bug this whole file exists to
// avoid.
func TestLeftOnAChildClosesItsParentAndTakesTheCursor(t *testing.T) {
	m := tap(openInstall(t), "right", "down")
	if m.items[m.itemAt].Label != "jq" {
		t.Fatalf("the cursor is on %q, not the first child", m.items[m.itemAt].Label)
	}
	closed := tap(m, "left")
	if closed.items[closed.itemAt].Label != "brew packages" {
		t.Errorf("the cursor is on %q", closed.items[closed.itemAt].Label)
	}
	if rowFor(plain(closed), "jq") != "" {
		t.Errorf("the group is still open:\n%s", plain(closed))
	}
}

// The cursor moves over what is drawn. Stepping through m.items would walk into
// a folded child and stop somewhere the reader cannot see it.
func TestTheCursorSkipsFoldedChildren(t *testing.T) {
	m := tap(openInstall(t), "down")
	if got := m.items[m.itemAt].Label; got == "jq" || got == "fd" {
		t.Errorf("down landed on the folded child %q", got)
	}
	// Every stop is a row that exists.
	for i := 0; i < len(m.items)*2; i++ {
		m = tap(m, "down")
		if item := m.items[m.itemAt]; item.Parent != "" && m.folded[item.Parent] {
			t.Fatalf("the cursor landed on %q, folded under %q", item.Label, item.Parent)
		}
	}
}

// G means the last thing on screen, not the last thing in the list.
func TestGJumpsToTheLastVisibleItem(t *testing.T) {
	m := tap(openInstall(t), "G")
	item := m.items[m.itemAt]
	if item.Parent != "" && m.folded[item.Parent] {
		t.Errorf("G landed on the folded %q", item.Label)
	}
	visible := m.visibleItems()
	if m.itemAt != visible[len(visible)-1] {
		t.Errorf("G landed on %q, not the last visible row", item.Label)
	}
}

// ------------------------------------------------------------ the tri-state --

// A parent carries its children, so unticking it cannot leave one behind for the
// install to find.
func TestUntickingAParentUnticksItsChildren(t *testing.T) {
	m := tap(openInstall(t), " ")
	ticked := chosenLabels(m)
	for _, label := range append([]string{"brew packages"}, tools...) {
		if ticked[label] {
			t.Errorf("%q is still ticked: %v", label, labelsOf(m.Chosen()))
		}
	}
	// And nothing else moved.
	if !ticked["zsh"] || !ticked["mcp servers"] {
		t.Errorf("it reached outside the group: %v", labelsOf(m.Chosen()))
	}
}

// A child updates its parent, which is the OR of them: a parent left unticked
// with a ticked child underneath would report a skipped phase and then install
// something.
func TestTickingOneChildTicksItsParent(t *testing.T) {
	// Untick the whole group, open it, tick one child back.
	m := tap(openInstall(t), " ", "right", "down", " ")
	ticked := chosenLabels(m)
	if !ticked["jq"] {
		t.Fatalf("the child is not ticked: %v", labelsOf(m.Chosen()))
	}
	if !ticked["brew packages"] {
		t.Errorf("the parent is not ticked with a ticked child under it: %v",
			labelsOf(m.Chosen()))
	}
	if ticked["fd"] {
		t.Errorf("it ticked more than the one: %v", labelsOf(m.Chosen()))
	}
}

// Partly ticked, and said with a glyph: "some of these" is the state a reader
// most needs to see, because it is the one they did not ask for directly.
func TestAPartlyTickedParentSaysSo(t *testing.T) {
	m := tap(openInstall(t), "right", "down", " ")
	row := rowFor(plain(m), "brew packages")
	if !strings.Contains(row, "[~]") {
		t.Errorf("the parent row is %q, want a partial box", row)
	}
}

// And space on one of those fills it, because that is what the hand means.
func TestSpaceOnAPartlyTickedParentFillsIt(t *testing.T) {
	m := tap(openInstall(t), "right", "down", " ", "up", " ")
	ticked := chosenLabels(m)
	for _, label := range append([]string{"brew packages"}, tools...) {
		if !ticked[label] {
			t.Errorf("%q is not ticked: %v", label, labelsOf(m.Chosen()))
		}
	}
}

// A fully ticked parent clears, which is the other half of the same key.
func TestSpaceOnAFullParentClearsIt(t *testing.T) {
	m := tap(openInstall(t), " ")
	if chosenLabels(m)["brew packages"] {
		t.Errorf("space did not clear a full parent: %v", labelsOf(m.Chosen()))
	}
}

// `a` and `n` reach the parents too, so one never disagrees with what is under
// it.
func TestAllAndNoneAgreeWithTheParents(t *testing.T) {
	none := tap(openInstall(t), "n")
	if len(none.Chosen()) != 0 {
		t.Errorf("n left %v", labelsOf(none.Chosen()))
	}
	all := tap(none, "a")
	if len(all.Chosen()) != len(components()) {
		t.Errorf("a left %d of %d: %v", len(all.Chosen()), len(components()),
			labelsOf(all.Chosen()))
	}
}

// The count is over the leaves. A parent is a summary of its children, and
// counting it as well would make "3 of 2" the reading for a fully ticked pair.
func TestTheCountIgnoresTheParents(t *testing.T) {
	body := plain(openInstall(t))
	want := "of " + itoa(leaves())
	if !strings.Contains(body, want) {
		t.Errorf("the footer does not count %d leaves:\n%s", leaves(), body)
	}
}

// Folding is display only. A hidden child is hidden, not unticked - anything
// else would make closing a group a way to silently change the outcome.
func TestFoldingDoesNotChangeTheSelection(t *testing.T) {
	open := tap(openInstall(t), "right")
	before := labelsOf(open.Chosen())
	after := labelsOf(tap(open, "left").Chosen())
	if strings.Join(before, ",") != strings.Join(after, ",") {
		t.Errorf("folding changed the selection\nbefore %v\nafter  %v", before, after)
	}
}

// ---------------------------------------------------------------- alignment --

// Every label in the list starts in the same column, whether its row is a
// parent, a child, or a plain row with nothing under it. Without the reserved
// fold column the plain rows read as mis-indented.
func TestEveryLabelStartsInTheSameColumn(t *testing.T) {
	body := plain(tap(openInstall(t), "right"))
	cols := map[int][]string{}
	for _, want := range []string{"brew packages", "jq", "fd", "mcp servers", "zsh"} {
		row := rowFor(body, want)
		if row == "" {
			t.Fatalf("%q is not on screen:\n%s", want, body)
		}
		at := colOf(row, want)
		cols[at] = append(cols[at], want)
	}
	if len(cols) != 1 {
		t.Errorf("labels start in %d different columns: %v\n%s", len(cols), cols, body)
	}
}

// A list with nothing to fold reserves no fold column, because two blank
// columns on every row of the removal list is two columns of nothing.
func TestAListWithoutParentsHasNoFoldColumn(t *testing.T) {
	m := openRemoval(t, removeModel())
	if anyParent(m.items) {
		t.Fatal("the removal fixture has parents, so this proves nothing")
	}
	body := plain(m)
	if strings.Contains(body, foldOpen) || strings.Contains(body, foldClosed) {
		t.Errorf("a fold marker on a list that does not fold:\n%s", body)
	}
	// Measured against the cursor marker rather than the frame, because the card
	// is centred and its absolute column moves with the terminal.
	row := rowFor(body, "jq")
	marker, box := colOf(row, "›"), colOf(row, "[ ]")
	if marker < 0 || box < 0 {
		t.Fatalf("no cursor row: %q", row)
	}
	if box-marker != 2 {
		t.Errorf("the box is %d columns after the cursor, want 2: %q", box-marker, row)
	}
}

// And its footer does not offer a key that would do nothing.
func TestAListWithoutParentsDoesNotOfferTheFoldKey(t *testing.T) {
	if body := footerOf(openRemoval(t, removeModel()).View()); strings.Contains(body, "open") {
		t.Errorf("the removal footer offers a fold:\n%s", body)
	}
}

// colOf is the column a substring starts at, or -1.
//
// Columns, not bytes: `›` and `▾` are three bytes each and one column wide, so
// strings.Index put the cursor row four bytes further right than the one under
// it and made an aligned list look ragged. The same mistake the frame itself has
// a rule about - every width in this program is a lipgloss.Width.
func colOf(row, want string) int {
	at := strings.Index(row, want)
	if at < 0 {
		return -1
	}
	return lipgloss.Width(row[:at])
}

// rowFor is the rendered line holding a label, or "" when it is not on screen.
//
// Matched on the label with a space either side of where it sits, so "jq" does
// not find itself inside "jquery" and "snip" does not find "opencode-snip".
func rowFor(body, label string) string {
	for _, line := range strings.Split(body, "\n") {
		at := strings.Index(line, label)
		if at < 0 {
			continue
		}
		before := at == 0 || line[at-1] == ' '
		end := at + len(label)
		after := end >= len(line) || line[end] == ' '
		if before && after {
			return line
		}
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}

// The fixture has to actually fold, or every test above is vacuous.
func TestTheFixtureFolds(t *testing.T) {
	if !anyParent(components()) {
		t.Fatal("the install fixture has no parents")
	}
	var children int
	for _, item := range components() {
		if item.Parent != "" {
			children++
		}
	}
	if children < 3 {
		t.Fatalf("the fixture has %d children; too few to be interesting", children)
	}
	if len(childrenOf(components(), "brew packages")) != len(tools) {
		t.Fatalf("the tool list changed; update `tools` in this file")
	}
	var _ app.Component
}

// The fold keys on rows that do not fold, and on a list that does not: all
// no-ops, and none of them may move the cursor or the selection.
func TestTheFoldKeysAreHarmlessWhereThereIsNothingToFold(t *testing.T) {
	// A plain top-level row: the last one, which nothing folds away.
	plainRow := tap(openInstall(t), "G")
	for _, k := range []string{"right", "left", "tab"} {
		next := tap(plainRow, k)
		if next.itemAt != plainRow.itemAt {
			t.Errorf("%q moved the cursor from %q to %q", k,
				plainRow.items[plainRow.itemAt].Label, next.items[next.itemAt].Label)
		}
		if len(next.Chosen()) != len(plainRow.Chosen()) {
			t.Errorf("%q changed the selection", k)
		}
	}

	// Right on a child: the group it is in is already open, so there is nothing
	// to do and nowhere better to be.
	child := tap(openInstall(t), "right", "down")
	if got := tap(child, "right"); got.itemAt != child.itemAt {
		t.Errorf("right on a child moved to %q", got.items[got.itemAt].Label)
	}

	// tab on a child closes the group it is in, like left.
	if got := tap(child, "tab"); got.items[got.itemAt].Label != "brew packages" {
		t.Errorf("tab on a child left the cursor on %q", got.items[got.itemAt].Label)
	}
}

// An empty list has nothing to fold, nothing to move over, and no arithmetic to
// divide by zero - which is the removal selector on a machine with nothing to
// remove.
func TestTheFoldKeysOnAnEmptyList(t *testing.T) {
	m := openRemoval(t, removeModel(func(c *Config) { c.Removable = nil }))
	if len(m.items) != 0 {
		t.Fatalf("the fixture is not empty: %v", labelsOf(m.Chosen()))
	}
	for _, k := range []string{"right", "left", "tab", "up", "down", "g", "G", " "} {
		next := tap(m, k)
		if next.itemAt != 0 {
			t.Errorf("%q put the cursor at %d on an empty list", k, next.itemAt)
		}
	}
}

// A parent whose children are gone is a parent no longer, so its row folds
// nothing and draws no marker.
func TestAParentWithoutChildrenIsJustARow(t *testing.T) {
	orphan := []app.Component{
		{Group: "Packages", Kind: app.KindTools, Label: "brew packages",
			Status: "0 of 0 present", Selected: true},
		{Group: "Configs", Kind: app.KindStow, Label: "zsh",
			Status: "linked", Done: true, Selected: true},
	}
	m := tap(New(Config{
		Components: orphan, Version: "v1.0.0", Width: 80, Height: 26,
		Renderer: lipgloss.NewRenderer(io.Discard), Run: noWork,
	}), "enter")

	if anyParent(m.items) {
		t.Fatal("a childless parent is being treated as one")
	}
	body := plain(m)
	if strings.Contains(body, foldOpen) || strings.Contains(body, foldClosed) {
		t.Errorf("a fold marker with nothing to fold:\n%s", body)
	}
	// And space still ticks it, as its own row rather than as a group.
	if chosenLabels(tap(m, " "))["brew packages"] {
		t.Errorf("space did not untick a childless parent")
	}
}

// The guard in moveItem: a cursor whose row was folded away by something other
// than a fold key - a re-scan replacing the list, say - lands on the first
// visible row rather than on nothing.
//
// Driven directly rather than through keys, because no key can produce it: the
// fold keys all take the cursor with them. It is a safety net, and a safety net
// with no test is a comment.
func TestMovingAfterTheCursorsRowVanishes(t *testing.T) {
	m := tap(openInstall(t), "right", "down")
	if m.items[m.itemAt].Parent == "" {
		t.Fatalf("the cursor is on %q, not a child", m.items[m.itemAt].Label)
	}
	// Fold the parent without moving the cursor.
	stranded := m.fold("brew packages", true)
	if !slices.Contains(stranded.visibleItems(), stranded.itemAt) {
		t.Log("the cursor is stranded, as intended")
	} else {
		t.Fatal("the cursor is still visible, so this proves nothing")
	}

	next := stranded.moveItem(1)
	if !slices.Contains(next.visibleItems(), next.itemAt) {
		t.Errorf("moving from a stranded cursor landed on %q, which is not drawn",
			next.items[next.itemAt].Label)
	}
}
