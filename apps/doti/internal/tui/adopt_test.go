package tui

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
)

// Adopt's selector, whose whole description is "install only what is missing"
// and which was byte-for-byte the Install one.

func adoptAt(t *testing.T) int {
	t.Helper()
	for i, entry := range menu {
		if entry.action == ActionAdopt {
			return i
		}
	}
	t.Fatal("no adopt in the menu")
	return 0
}

func openAdopt(t *testing.T, m Model) Model {
	t.Helper()
	m.menuAt = adoptAt(t)
	next := tap(m, "enter")
	if next.screen != ScreenSelect {
		t.Fatalf("adopt did not open a selector: screen %v", next.screen)
	}
	return next
}

// gappy is a machine that has some of what the manifest declares: one tool of
// two, and one config of two.
func gappy() []app.Component {
	return []app.Component{
		{Group: "Packages", Kind: app.KindTools, Label: "brew packages",
			Status: "1 of 2 present", Selected: true},
		{Group: "Packages", Kind: app.KindTool, Parent: "brew packages",
			Label: "jq", Status: "installed", Done: true, Selected: true},
		{Group: "Packages", Kind: app.KindTool, Parent: "brew packages",
			Label: "fd", Status: "missing", Selected: true},
		{Group: "Packages", Kind: app.KindCasks, Label: "brew casks",
			Status: "1 of 1 present", Done: true, Selected: true},
		{Group: "Packages", Kind: app.KindCask, Parent: "brew casks",
			Label: "ghostty", Status: "installed", Done: true, Selected: true},
		{Group: "Configs", Kind: app.KindStow, Label: "zsh",
			Status: "linked", Done: true, Selected: true},
		{Group: "Configs", Kind: app.KindStow, Label: "git",
			Status: "not linked", Selected: true},
		{Group: "Secrets", Kind: app.KindSecret, Label: "mssql-envs",
			Status: "rendered", Done: true, Selected: true},
	}
}

func gappyModel(components []app.Component) Model {
	return New(Config{
		Components: components, Version: "v1.0.0", Width: 80, Height: 30,
		Renderer: lipgloss.NewRenderer(io.Discard), Run: noWork,
	})
}

// The complaint, as a test: the two screens were the same bytes.
func TestAdoptAndInstallAreNotTheSameScreen(t *testing.T) {
	base := gappyModel(gappy())
	install := tap(base, "enter")
	adopt := openAdopt(t, base)

	if plain(install) == plain(adopt) {
		t.Fatalf("adopt renders the install screen:\n%s", plain(adopt))
	}
	if len(adopt.items) >= len(install.items) {
		t.Errorf("adopt offered %d of %d components", len(adopt.items), len(install.items))
	}
}

func TestAdoptOffersOnlyWhatIsMissing(t *testing.T) {
	m := openAdopt(t, gappyModel(gappy()))
	got := map[string]bool{}
	for _, item := range m.items {
		got[item.Label] = true
	}
	// The two gaps, and the group the first one is in.
	for _, want := range []string{"brew packages", "fd", "git"} {
		if !got[want] {
			t.Errorf("%q is missing from the adopt list: %v", want, labelsOf(m.Chosen()))
		}
	}
	// Everything the machine already has, including the group whose every
	// member it has.
	for _, absent := range []string{"jq", "brew casks", "ghostty", "zsh", "mssql-envs"} {
		if got[absent] {
			t.Errorf("%q is already in place and was offered: %v", absent,
				labelsOf(m.Chosen()))
		}
	}
}

// A group keeps its row exactly when something under it is still missing - its
// own Done is a summary, and the children are the truth.
func TestAdoptKeepsAGroupWithAGapAndDropsAFullOne(t *testing.T) {
	body := plain(openAdopt(t, gappyModel(gappy())))
	if rowFor(body, "brew packages") == "" {
		t.Errorf("the group with a gap was dropped:\n%s", body)
	}
	if rowFor(body, "brew casks") != "" {
		t.Errorf("a group the machine has all of was kept:\n%s", body)
	}
}

// Open, because a list of what is left is short by construction and folding it
// would hide the one thing the reader came for.
func TestAdoptOpensItsGroups(t *testing.T) {
	m := openAdopt(t, gappyModel(gappy()))
	if rowFor(plain(m), "fd") == "" {
		t.Errorf("the missing tool is folded away:\n%s", plain(m))
	}
	if !strings.Contains(plain(m), foldOpen) {
		t.Errorf("no open marker:\n%s", plain(m))
	}
	// And the install list is still folded, so this is a difference rather than
	// a change of default.
	if rowFor(plain(tap(gappyModel(gappy()), "enter")), "fd") != "" {
		t.Error("the install list opened too")
	}
}

// Said once, so nobody has to wonder whether something went missing from the
// list.
func TestAdoptSaysWhyItsListIsShorter(t *testing.T) {
	if body := plain(openAdopt(t, gappyModel(gappy()))); !strings.Contains(
		body, "only what the machine is missing") {
		t.Errorf("the screen does not say what it is showing:\n%s", body)
	}
}

// A machine with nothing missing gets a sentence rather than an empty box.
func TestAdoptOnAMachineWithNothingMissing(t *testing.T) {
	full := gappy()
	for i := range full {
		full[i].Status, full[i].Done = "installed", true
	}
	m := openAdopt(t, gappyModel(full))
	if len(m.items) != 0 {
		t.Fatalf("offered %v on a machine that has everything", labelsOf(m.Chosen()))
	}
	body := plain(m)
	if !strings.Contains(body, "Nothing to adopt") {
		t.Errorf("the screen does not say so:\n%s", body)
	}

	// And enter says something that can actually be acted on. It used to say
	// "press a for all" - advice that does nothing on a list with no rows.
	pressed := tap(m, "enter")
	if pressed.screen != ScreenSelect {
		t.Errorf("it ran with nothing to do: screen %v", pressed.screen)
	}
	if !strings.Contains(plain(pressed), "nothing here to do") {
		t.Errorf("the notice does not fit an empty list:\n%s", plain(pressed))
	}
	if strings.Contains(plain(pressed), "press a for all") {
		t.Errorf("it suggested a key that does nothing:\n%s", plain(pressed))
	}
}

// And only the ticked gaps reach the operation, qualified as always.
func TestAdoptPassesOnlyTheGaps(t *testing.T) {
	m := openAdopt(t, gappyModel(gappy()))
	got := map[app.Kind][]string{}
	for _, ref := range m.Chosen() {
		got[ref.Kind] = append(got[ref.Kind], ref.Label)
	}
	if strings.Join(got[app.KindTool], ",") != "fd" {
		t.Errorf("tools = %v", got[app.KindTool])
	}
	if strings.Join(got[app.KindStow], ",") != "git" {
		t.Errorf("stow = %v", got[app.KindStow])
	}
	if len(got[app.KindCask]) != 0 || len(got[app.KindSecret]) != 0 {
		t.Errorf("it passed things already in place: %+v", got)
	}
}

// ------------------------------------------------------------ missingOnly --

func TestMissingOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []app.Component
		want []string
	}{
		{
			name: "a group with a gap keeps the group and the gap",
			in: []app.Component{
				{Kind: app.KindTools, Label: "p"},
				{Kind: app.KindTool, Parent: "p", Label: "have", Done: true},
				{Kind: app.KindTool, Parent: "p", Label: "want"},
			},
			want: []string{"p", "want"},
		},
		{
			name: "a group with no gap goes entirely",
			in: []app.Component{
				{Kind: app.KindTools, Label: "p", Done: true},
				{Kind: app.KindTool, Parent: "p", Label: "have", Done: true},
			},
			want: nil,
		},
		{
			// The children are the truth. A parent whose count disagrees with
			// them is still kept, because something under it is missing.
			name: "a group marked done with a missing child is kept",
			in: []app.Component{
				{Kind: app.KindTools, Label: "p", Done: true},
				{Kind: app.KindTool, Parent: "p", Label: "want"},
			},
			want: []string{"p", "want"},
		},
		{
			name: "a plain row is judged on its own state",
			in: []app.Component{
				{Kind: app.KindStow, Label: "linked", Done: true},
				{Kind: app.KindStow, Label: "unlinked"},
			},
			want: []string{"unlinked"},
		},
		{
			// Nothing else to judge it on, so it is a row like any other.
			name: "a childless group is judged on its own state",
			in: []app.Component{
				{Kind: app.KindTools, Label: "empty-done", Done: true},
				{Kind: app.KindMcps, Label: "empty-missing"},
			},
			want: []string{"empty-missing"},
		},
		{name: "nothing at all", in: nil, want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, item := range missingOnly(tc.in) {
				got = append(got, item.Label)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// The order the components arrived in survives, because a parent has to stay
// immediately in front of its children or the fold draws under the wrong one.
func TestMissingOnlyKeepsParentsInFrontOfTheirChildren(t *testing.T) {
	got := missingOnly(gappy())
	for i, item := range got {
		if item.Parent == "" {
			continue
		}
		if i == 0 || got[i-1].Label != item.Parent {
			t.Errorf("%q follows %q, not its parent %q", item.Label,
				func() string {
					if i == 0 {
						return "nothing"
					}
					return got[i-1].Label
				}(), item.Parent)
		}
	}
}
