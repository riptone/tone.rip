package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// A manifest that names formulae tap-qualified.
//
// opencode is the real case: its own docs recommend
// `brew install anomalyco/tap/opencode` over homebrew-core's copy, on the
// grounds that the tap tracks releases and core lags. `brew install` and
// `brew uninstall` take that spelling. `brew list` does not give it back - it
// prints the Cellar's short names - so every ownership lookup in this package
// reads one spelling while the manifest holds the other.
//
// bun is here as the control: core carries it, so it is named short, and the
// same code path has to leave it alone.
const tapManifest = `{
  "app": "dotfiles", "version": "9.0.0",
  "stow_packages": [{ "name": "zsh", "platforms": ["macos", "linux", "windows"] }],
  "stow_ignore": [],
  "tools": [
    { "cmd": "opencode", "brew": "anomalyco/tap/opencode", "winget": "SST.opencode" },
    { "cmd": "bun", "brew": "bun", "winget": "Oven-sh.Bun" }
  ],
  "brew_casks": [{ "brew": "someone/tap/font-x", "platforms": ["macos"] }],
  "health": { "extra_tools": { "macos": ["zsh", "brew", "stow"] } }
}`

func tapFixture(t *testing.T) (*App, *fakeRunner, *Recorder) {
	t.Helper()
	a, runner, rec := fixture(t, "opencode", "bun")
	write(t, filepath.Join(a.Repo, "manifest.jsonc"), tapManifest)
	// What brew actually prints, tap or no tap: the Cellar's short names.
	runner.owns("opencode", "bun")
	runner.out["brew list --cask -1"] = "font-x\n"
	return a, runner, rec
}

// Before Formula existed this failed on opencode and passed on bun, which is
// exactly the shape of the jq bug arrived at from the other side: brew owns the
// keg, the manifest spells it with a tap, and the removal selector could never
// see it.
func TestATapQualifiedToolIsOfferedForRemoval(t *testing.T) {
	a, _, _ := tapFixture(t)

	items, err := a.Removable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := labels(items)
	for _, want := range []string{"opencode", "bun"} {
		if !slicesContains(got, want) {
			t.Errorf("brew owns %s and it is not offered: %v", want, got)
		}
	}
}

// The label is the command; the package manager gets the manifest's spelling.
// `brew uninstall opencode` would be ambiguous at best and wrong at worst -
// core has a formula by that name too.
func TestRemovingATapQualifiedToolHandsBrewTheQualifiedName(t *testing.T) {
	a, runner, rec := tapFixture(t)
	a.Include = Refs([]string{"opencode"})

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("brew uninstall anomalyco/tap/opencode") {
		t.Fatalf("ran: %v", runner.ran)
	}
	if !rec.Contains("removed opencode") {
		t.Errorf("%v", rec.Texts())
	}
}

// A tap-qualified tool brew does not own is still left alone. The fix widens
// what matches, not what gets deleted.
func TestATapQualifiedToolBrewDoesNotOwnIsNotOffered(t *testing.T) {
	a, runner, _ := tapFixture(t)
	// On PATH, and brew owns nothing by that name - the npm-installed case.
	runner.owns("bun")

	items, err := a.Removable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := labels(items); slicesContains(got, "opencode") {
		t.Errorf("a tool brew does not own was offered: %v", got)
	}
}

// The install selector reads the same inventory, so a tap-qualified cask that
// is installed has to read as installed rather than sitting on the Adopt list
// forever.
func TestATapQualifiedCaskReadsAsInstalled(t *testing.T) {
	a, _, _ := tapFixture(t)

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, item := range items {
		if item.Kind != KindCask || item.Label != "someone/tap/font-x" {
			continue
		}
		found = true
		if item.Status != "installed" || !item.Done {
			t.Errorf("an installed cask reads %q done=%v", item.Status, item.Done)
		}
	}
	if !found {
		t.Fatalf("the cask was never offered: %v", labels(items))
	}
	// And the parent's count agrees with the child.
	for _, item := range items {
		if item.Kind == KindCasks && item.Status != "1 of 1 present" {
			t.Errorf("the casks parent says %q", item.Status)
		}
	}
}

// statusOf finds one tool's row in the selector.
func statusOf(t *testing.T, items []Component, kind Kind, label string) Component {
	t.Helper()
	for _, item := range items {
		if item.Kind == kind && item.Label == label {
			return item
		}
	}
	t.Fatalf("%s %q is not in the selector: %v", kind, label, labels(items))
	return Component{}
}

// The install selector and the removal selector have to tell the same story.
//
// They did not: macOS ships /usr/bin/jq, bun and opencode arrive from their own
// install scripts, and all three read a flat "installed" next to a removal list
// three rows shorter with nothing anywhere to explain the difference.
func TestAToolFromSomewhereElseSaysSoOnTheInstallSelector(t *testing.T) {
	a, runner, _ := tapFixture(t)
	// Both on PATH; brew owns only one of them.
	runner.owns("bun")

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	foreign := statusOf(t, items, KindTool, "opencode")
	if foreign.Status != "installed (not by brew)" {
		t.Errorf("a tool brew does not own reads %q", foreign.Status)
	}
	if !foreign.Foreign {
		t.Error("it is not marked foreign, so the parent cannot count it")
	}
	// Still Done: the machine has it, and an install must not reinstall over a
	// working binary from a different source.
	if !foreign.Done {
		t.Error("a tool the machine has reads as not done")
	}
	if owned := statusOf(t, items, KindTool, "bun"); owned.Status != "installed" || owned.Foreign {
		t.Errorf("a tool brew does own reads %q foreign=%v", owned.Status, owned.Foreign)
	}

	// And the parent says it, because the parent is what a folded group shows.
	parent := statusOf(t, items, KindTools, packagesLabel)
	if !strings.Contains(parent.Status, "1 from elsewhere") {
		t.Errorf("the parent says %q", parent.Status)
	}
}

// Nothing foreign, nothing said. The count is only worth a reader's attention
// when it is not zero.
func TestTheParentSaysNothingWhenEveryToolIsThePackageManagers(t *testing.T) {
	a, _, _ := tapFixture(t)

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	parent := statusOf(t, items, KindTools, packagesLabel)
	if strings.Contains(parent.Status, "elsewhere") {
		t.Errorf("the parent says %q on a machine where brew owns everything", parent.Status)
	}
}

// And the run says it, in the same words the selector's parent row uses.
//
// This is the half that was actually reported: `doti install` finished, said
// every tool was present, and had installed nothing - which is true and reads
// as "I installed everything". Two of the tools had come from their own install
// scripts and were never brew's to begin with.
func TestTheRunSaysWhichToolsCameFromElsewhere(t *testing.T) {
	a, runner, rec := tapFixture(t)
	runner.owns("bun")

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains("2 of 2 tools present, 1 from elsewhere") {
		t.Errorf("the tally does not mention it: %v", rec.Texts())
	}
	if !rec.Contains("from elsewhere, left alone: opencode") {
		t.Errorf("the run does not name it: %v", rec.Texts())
	}
	// Left alone means left alone: nothing was reinstalled over it.
	for _, ran := range runner.ran {
		if strings.Contains(ran, "bundle") {
			t.Errorf("a machine with nothing missing ran an install: %s", ran)
		}
	}
}

// An inventory that will not answer is not a reason to refuse to install
// anything. Removable takes the opposite view of the same error, and should.
func TestABrokenInventoryDoesNotStopAnInstall(t *testing.T) {
	a, runner, rec := tapFixture(t)
	runner.fail = map[string]error{"brew list": errors.New("Error: Not a git repository")}

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatalf("an unreadable inventory failed the install: %v", err)
	}
	joined := strings.Join(rec.Texts(), "\n")
	if strings.Contains(joined, "from elsewhere") {
		t.Errorf("a claim was made with no inventory to back it: %v", rec.Texts())
	}
}
