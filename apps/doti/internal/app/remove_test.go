package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// The one operation here that deletes software. The rules are the feature, so
// each one has a test that fails if it goes away.

// The fixture's manifest lists jq and ghostty as tools, and calls zsh required.
const removableManifest = `{
  "app": "dotfiles", "version": "9.0.0",
  "stow_packages": [{ "name": "zsh", "platforms": ["macos", "linux", "windows"] }],
  "stow_ignore": [],
  "tools": [
    { "cmd": "jq", "brew": "jq", "winget": "jqlang.jq" },
    { "cmd": "fd", "brew": "fd" },
    { "cmd": "zsh", "brew": "zsh" },
    { "cmd": "hand-rolled", "app": "HandRolled" }
  ],
  "health": { "extra_tools": { "macos": ["zsh", "brew", "stow"] } }
}`

// removableFixture puts the named tools on PATH *and* has brew own them, which
// is the ordinary case: doti installed them, so both facts are true.
//
// The interesting cases are the ones where they come apart, and those say so:
// see TestAToolBrewDoesNotOwnIsNotOffered.
func removableFixture(t *testing.T, installed ...string) (*App, *fakeRunner, *Recorder) {
	t.Helper()
	a, runner, rec := fixture(t, installed...)
	write(t, filepath.Join(a.Repo, "manifest.jsonc"), removableManifest)
	runner.owns(installed...)
	return a, runner, rec
}

func labels(items []Component) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Label)
	}
	return out
}

func TestRemovableOffersOnlyWhatItWillDelete(t *testing.T) {
	a, _, _ := removableFixture(t, "jq", "zsh", "hand-rolled")

	items, err := a.Removable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := labels(items)

	if !slicesContains(got, "jq") {
		t.Errorf("jq is installed and removable but is not offered: %v", got)
	}
	// Not installed, so there is nothing to remove.
	if slicesContains(got, "fd") {
		t.Errorf("fd is not installed and was offered: %v", got)
	}
	// The manifest calls it required: it is how the machine gets back to a
	// working state.
	if slicesContains(got, "zsh") {
		t.Errorf("a required tool was offered: %v", got)
	}
	// No brew or winget name, so it arrived some other way and removing it is
	// not this tool's business either.
	if slicesContains(got, "hand-rolled") {
		t.Errorf("a tool with no package name was offered: %v", got)
	}

	// And nothing arrives ticked: the safe action is to press enter.
	for _, item := range items {
		if item.Selected {
			t.Errorf("%q arrived ticked", item.Label)
		}
	}
}

// There is no spelling of this that means "all". An empty Include lists what it
// would be willing to remove, hands over the command that would do it, and
// removes nothing.
func TestAnEmptySelectionRemovesNothingAndSaysWhatItCould(t *testing.T) {
	a, runner, rec := removableFixture(t, "jq", "fd", "zsh")

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, ran := range runner.ran {
		if strings.Contains(ran, "uninstall") {
			t.Fatalf("an empty selection removed something: %s", ran)
		}
	}
	joined := strings.Join(rec.Texts(), "\n")
	for _, want := range []string{"name what to remove", "removable: ", "--tools "} {
		if !strings.Contains(joined, want) {
			t.Errorf("the report is missing %q: %v", want, rec.Texts())
		}
	}
	// The line it hands over has to be usable, which means naming the tools.
	if !strings.Contains(joined, "jq") || !strings.Contains(joined, "fd") {
		t.Errorf("the suggested command names nothing: %v", rec.Texts())
	}
}

func TestNothingLeftToRemoveSaysSo(t *testing.T) {
	a, _, rec := removableFixture(t)

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains("nothing this repository installed is still present") {
		t.Errorf("%v", rec.Texts())
	}
}

func TestANamedToolIsRemovedThroughThePackageManager(t *testing.T) {
	a, runner, rec := removableFixture(t, "jq", "fd", "zsh")
	a.Include = Refs([]string{"jq"})

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("brew uninstall jq") {
		t.Fatalf("ran: %v", runner.ran)
	}
	// The whole point of asking a package manager rather than deleting files is
	// that it knows what else would break. Overriding that is not an option
	// this offers.
	for _, ran := range runner.ran {
		for _, forbidden := range []string{"--ignore-dependencies", "--force", "-f"} {
			if strings.Contains(ran, forbidden) {
				t.Errorf("%s was passed: %s", forbidden, ran)
			}
		}
	}
	if !rec.Contains("removed jq") {
		t.Errorf("%v", rec.Texts())
	}
	// Only what was named.
	if runner.didRun("brew uninstall fd") {
		t.Errorf("something unnamed was removed: %v", runner.ran)
	}
}

// A tool this repository never installed is not this repository's to remove.
func TestAToolTheManifestDoesNotListIsRefused(t *testing.T) {
	a, runner, rec := removableFixture(t, "jq")
	a.Include = Refs([]string{"docker"})

	err := a.RemovePackages(context.Background())
	if err == nil {
		t.Fatal("want an error: a typo removing nothing reads as 'it was already gone'")
	}
	if !strings.Contains(err.Error(), "docker") {
		t.Errorf("the error does not name it: %v", err)
	}
	if !rec.Contains("docker is not a tool this manifest installs") {
		t.Errorf("%v", rec.Texts())
	}
	for _, ran := range runner.ran {
		if strings.Contains(ran, "uninstall") {
			t.Errorf("something was removed anyway: %s", ran)
		}
	}
}

// A command that can remove its own package manager is a foot-gun with a name.
func TestARequiredToolIsRefusedEvenWhenNamed(t *testing.T) {
	a, runner, rec := removableFixture(t, "jq", "zsh")
	a.Include = Refs([]string{"zsh"})

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatalf("refusing is not a failure: %v", err)
	}
	if !rec.Contains("zsh is required by the manifest and will not be removed") {
		t.Errorf("%v", rec.Texts())
	}
	if runner.didRun("brew uninstall zsh") {
		t.Errorf("a required tool was removed: %v", runner.ran)
	}
}

// health.extra_tools is a different list from tools, so a required tool can be
// absent from the tools list entirely - which is the real manifest's shape for
// `brew` and `stow`. Asking "is it a tool we install?" first refused those for
// the wrong reason: "not a tool this manifest installs", about a tool the
// manifest names two lines further down.
func TestARequiredToolAbsentFromTheToolsListIsStillRefusedAsRequired(t *testing.T) {
	a, runner, rec := removableFixture(t, "jq", "brew", "stow")
	a.Include = Refs([]string{"brew", "stow"})

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatalf("refusing is not a failure: %v", err)
	}
	joined := strings.Join(rec.Texts(), "\n")
	for _, name := range []string{"brew", "stow"} {
		if !strings.Contains(joined, name+" is required by the manifest") {
			t.Errorf("%s was refused for the wrong reason: %v", name, rec.Texts())
		}
		if strings.Contains(joined, name+" is not a tool this manifest installs") {
			t.Errorf("%s was called unknown: %v", name, rec.Texts())
		}
	}
	if len(runner.ran) != 0 {
		t.Errorf("ran: %v", runner.ran)
	}
}

// Naming something that is already gone is not an error - it is the state you
// were asking for.
func TestAToolThatIsAlreadyGoneIsReportedNotRemoved(t *testing.T) {
	a, runner, rec := removableFixture(t, "jq")
	a.Include = Refs([]string{"fd"})

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains("fd (not installed)") {
		t.Errorf("%v", rec.Texts())
	}
	if len(runner.ran) != 0 {
		t.Errorf("ran: %v", runner.ran)
	}
}

func TestADryRunRemovesNothing(t *testing.T) {
	a, runner, rec := removableFixture(t, "jq", "fd")
	a.Include = Refs([]string{"jq", "fd"})
	a.DryRun = true

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.ran) != 0 {
		t.Errorf("a dry run ran: %v", runner.ran)
	}
	if !rec.Contains("would remove jq (jq)") {
		t.Errorf("%v", rec.Texts())
	}
}

// Homebrew refusing because something depends on a formula is the correct
// answer. It is reported, the rest of the list still runs, and the exit is
// honest about it.
func TestAPackageManagerRefusalIsReportedAndReturned(t *testing.T) {
	a, runner, rec := removableFixture(t, "jq", "fd")
	a.Include = Refs([]string{"jq", "fd"})
	runner.onRun = func(name string, args []string) error {
		if name == "brew" && slicesContains(args, "jq") {
			return errors.New("Refusing to uninstall because it is required by fd")
		}
		return nil
	}

	err := a.RemovePackages(context.Background())
	if err == nil {
		t.Fatal("a refusal should reach the exit code")
	}
	if !strings.Contains(err.Error(), "1 of the 2 named") {
		t.Errorf("the error does not say how much failed: %v", err)
	}
	if !rec.Contains("jq: Refusing to uninstall because it is required by fd") {
		t.Errorf("the package manager's own message is the useful part: %v", rec.Texts())
	}
	// The rest of the list still ran.
	if !runner.didRun("brew uninstall fd") {
		t.Errorf("one refusal stopped the others: %v", runner.ran)
	}
}

func TestWindowsRemovesThroughWinget(t *testing.T) {
	a, runner, _ := removableFixture(t, "jq")
	a.Platform = manifest.Windows
	a.Include = Refs([]string{"jq"})
	runner.ownsWinget("jqlang.jq")

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	var ran string
	for _, call := range runner.ran {
		if strings.HasPrefix(call, "winget") {
			ran = call
		}
	}
	if ran == "" {
		t.Fatalf("winget was never run: %v", runner.ran)
	}
	for _, want := range []string{"uninstall", "--id", "jqlang.jq", "--exact"} {
		if !strings.Contains(ran, want) {
			t.Errorf("the command is missing %q: %s", want, ran)
		}
	}
}

// Do is the one place a name becomes a call, and a removal reaches it like
// anything else - so the window and the command line cannot diverge.
func TestDoReachesTheRemoval(t *testing.T) {
	a, runner, _ := removableFixture(t, "jq")

	if err := a.Do(context.Background(), OpRemovePackages, Refs([]string{"jq"}), "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("brew uninstall jq") {
		t.Fatalf("ran: %v", runner.ran)
	}
}

func TestSplitList(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"jq", []string{"jq"}},
		{"jq,fd", []string{"jq", "fd"}},
		{" jq , fd ", []string{"jq", "fd"}},
		{"jq,,fd,", []string{"jq", "fd"}},
		{",", nil},
	} {
		got := SplitList(tc.in)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("SplitList(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func slicesContains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

// The bug that made this whole file wrong for one tool.
//
// macOS ships /usr/bin/jq. Somebody ran the removal, brew uninstalled its jq,
// and the selector went on offering jq as "installed" - every session, across a
// reboot - because the list was built from `command -v` and `command -v` was
// telling the truth. Presence is the right question for an install and the
// wrong one for a removal.
func TestAToolBrewDoesNotOwnIsNotOffered(t *testing.T) {
	a, runner, _ := removableFixture(t, "jq", "fd")
	// jq is on PATH - /usr/bin/jq - and brew has never heard of it.
	runner.owns("fd")

	items, err := a.Removable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := labels(items)
	if slicesContains(got, "jq") {
		t.Errorf("a jq brew does not own was offered for removal: %v", got)
	}
	if !slicesContains(got, "fd") {
		t.Errorf("fd is brew's and was not offered: %v", got)
	}
}

// And naming it explicitly says which of the two facts is the reason.
//
// "jq (not installed)" was the old answer, and it is a lie about a binary the
// reader can run. The distinction is the whole point of the fix, so it is in the
// message.
func TestNamingAToolBrewDoesNotOwnSaysWhy(t *testing.T) {
	a, runner, rec := removableFixture(t, "jq")
	runner.owns()
	a.Include = Refs([]string{"jq"})

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatalf("leaving it alone is not a failure: %v", err)
	}
	if !rec.Contains("jq is installed, but not by brew - left alone") {
		t.Errorf("%v", rec.Texts())
	}
	if runner.didRun("brew uninstall jq") {
		t.Errorf("brew was asked to uninstall something it does not own: %v", runner.ran)
	}
}

// A tool that is genuinely absent still gets the short answer, because there is
// nothing to explain.
func TestAnAbsentToolIsNotDescribedAsForeign(t *testing.T) {
	a, _, rec := removableFixture(t, "jq")
	a.Include = Refs([]string{"fd"})

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains("fd (not installed)") {
		t.Errorf("%v", rec.Texts())
	}
	if strings.Contains(strings.Join(rec.Texts(), "\n"), "but not by brew") {
		t.Errorf("an absent tool was described as foreign: %v", rec.Texts())
	}
}

// Asking is not doing. The inventory calls are questions, and a run that
// removes nothing must still have run nothing.
func TestTheInventoryIsAskedNotRun(t *testing.T) {
	a, runner, _ := removableFixture(t, "jq")

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.ran) != 0 {
		t.Errorf("an empty selection ran something: %v", runner.ran)
	}
	if len(runner.asked) == 0 {
		t.Error("brew was never asked what it owns, so the list cannot be right")
	}
}

// Windows reads the same inventory through `winget export`, which writes the
// file this repository already knows how to render.
func TestWingetOwnershipIsReadFromTheExport(t *testing.T) {
	a, runner, _ := removableFixture(t, "jq")
	a.Platform = manifest.Windows
	runner.ownsWinget("jqlang.jq")

	items, err := a.Removable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slicesContains(labels(items), "jq") {
		t.Errorf("winget owns jqlang.jq and jq was not offered: %v", labels(items))
	}

	a.Platform = manifest.Windows
	runner.ownsWinget("Microsoft.Something.Else")
	// The inventory is cached per invocation, and the window drops it before
	// every re-scan - see App.Forget and cmd/doti's inventoryScanner. Doing the
	// same here is what makes this a second look rather than the same one.
	a.Forget()
	items, err = a.Removable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if slicesContains(labels(items), "jq") {
		t.Errorf("winget does not own jqlang.jq and jq was offered: %v", labels(items))
	}
}

// A machine with no brew has an empty removal list rather than an error: there
// is nothing this tool installed, which is true.
func TestNoPackageManagerMeansNothingToRemove(t *testing.T) {
	a, _, _ := fixture(t, "jq")
	write(t, filepath.Join(a.Repo, "manifest.jsonc"), removableManifest)

	items, err := a.Removable(context.Background())
	if err != nil {
		t.Fatalf("no brew is not a failure: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("offered %v with no package manager", labels(items))
	}
}

// The whole round trip, which is what was actually reported: uninstall a tool,
// come back to the list, and it is gone.
//
// The re-scan was already there; what was wrong was the question it asked. This
// drives a runner whose `brew uninstall` really does change what `brew list`
// says next, so the loop is closed end to end rather than at either half.
func TestARemovedToolLeavesTheList(t *testing.T) {
	a, runner, _ := removableFixture(t, "jq", "fd")
	owned := map[string]bool{"jq": true, "fd": true}
	runner.onRun = func(name string, args []string) error {
		if name != "brew" || len(args) < 2 || args[0] != "uninstall" {
			return nil
		}
		delete(owned, args[1])
		remaining := make([]string, 0, len(owned))
		for formula := range owned {
			remaining = append(remaining, formula)
		}
		runner.out["brew list --formula -1"] = strings.Join(remaining, "\n")
		return nil
	}

	before, err := a.Removable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slicesContains(labels(before), "fd") {
		t.Fatalf("fd was not there to begin with: %v", labels(before))
	}

	a.Include = Refs([]string{"fd"})
	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}

	// A fresh look, which is what re-entering the section does: the window
	// re-scans on a copy that has dropped the cached manifest and inventory.
	a.Include = nil
	a.Forget()
	after, err := a.Removable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if slicesContains(labels(after), "fd") {
		t.Errorf("fd is still on the list after being removed: %v", labels(after))
	}
	// And the rest of the list is untouched, so this is not passing by emptying
	// everything.
	if !slicesContains(labels(after), "jq") {
		t.Errorf("jq went missing too: %v", labels(after))
	}
	// PATH still has it - the fake `brew uninstall` cannot unlink a binary -
	// which is exactly the state that used to keep it on the list forever.
	if !runner.cmds["fd"] {
		t.Fatal("the fixture no longer reproduces the interesting case")
	}
}

// The closing tally says which set its denominator is.
//
// "2 of 2 did not come off" is circular, and "1 of 4" over a list holding a
// deliberate refusal and a tool that was already gone invites the reader to
// think three succeeded. It is a fraction of what they typed, and it says so.
func TestTheFailureTallyNamesItsDenominator(t *testing.T) {
	a, runner, _ := removableFixture(t, "jq", "fd")
	// zsh is required, gh is not installed, and jq's removal fails.
	a.Include = Refs([]string{"zsh", "gh", "jq"})
	runner.onRun = func(name string, args []string) error {
		if name == "brew" && slicesContains(args, "jq") {
			return errors.New("Refusing to uninstall")
		}
		return nil
	}

	err := a.RemovePackages(context.Background())
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "2 of the 3 named") {
		t.Errorf("the tally does not say what it counts: %v", err)
	}
}
