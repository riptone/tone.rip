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

func removableFixture(t *testing.T, installed ...string) (*App, *fakeRunner, *Recorder) {
	t.Helper()
	a, runner, rec := fixture(t, installed...)
	write(t, filepath.Join(a.Repo, "manifest.jsonc"), removableManifest)
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

	items, err := a.Removable()
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
	a.Include = []string{"jq"}

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
	a.Include = []string{"docker"}

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
	a.Include = []string{"zsh"}

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
	a.Include = []string{"brew", "stow"}

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
	a.Include = []string{"fd"}

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
	a.Include = []string{"jq", "fd"}
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
	a.Include = []string{"jq", "fd"}
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
	if !strings.Contains(err.Error(), "1 of 2") {
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
	a.Include = []string{"jq"}

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

	if err := a.Do(context.Background(), OpRemovePackages, []string{"jq"}, "v1.0.0"); err != nil {
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
