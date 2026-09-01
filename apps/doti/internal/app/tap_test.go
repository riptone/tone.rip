package app

import (
	"context"
	"path/filepath"
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
