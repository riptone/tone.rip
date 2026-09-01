package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// freshFixture is a machine with no checkout, and a clone that produces one.
func freshFixture(t *testing.T) (*App, *fakeRunner, *Recorder) {
	t.Helper()
	a, runner, rec := fixture(t, "brew", "git", "jq", "ghostty", "zsh")
	a.Repo = filepath.Join(t.TempDir(), "fresh")
	runner.onRun = func(name string, args []string) error {
		if name == "git" && len(args) > 0 && args[0] == "clone" {
			target := args[len(args)-1]
			write(t, filepath.Join(target, "manifest.jsonc"), fixtureManifest)
			write(t, filepath.Join(target, "zsh", ".zshrc"), "x\n")
			write(t, filepath.Join(target, "ghostty", ".config", "ghostty", "config"), "x\n")
		}
		return nil
	}
	return a, runner, rec
}

// The selector has to have something to draw before the manifest exists, or the
// window has no honest choice but to act on the reader's behalf - which is what
// it used to do.
func TestMenuItemsBeforeTheCloneOffersTheCheckout(t *testing.T) {
	a, _, _ := freshFixture(t)

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatalf("a machine with no checkout cannot be described: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("offered %d rows, want 1: %v", len(items), labels(items))
	}
	item := items[0]
	if item.Kind != KindRepo || item.Status != "not cloned" || !item.Selected {
		t.Errorf("the row reads %+v", item)
	}
	// $HOME written as ~, because it has to fit the narrowest card drawn.
	if strings.HasPrefix(item.Label, a.Home) {
		t.Errorf("the label is not shortened: %q", item.Label)
	}
}

// And nothing is removable: no manifest, so nothing is known to have been
// installed from one.
func TestNothingIsRemovableBeforeTheClone(t *testing.T) {
	a, _, _ := freshFixture(t)

	items, err := a.Removable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("offered %v", labels(items))
	}
}

// The trap this arrangement sets: the reader ticks the only box there is, and
// that selection then narrows every list read out of the manifest that did not
// exist when they ticked it - an install that does nothing but clone.
func TestASelectionMadeBeforeTheCloneDoesNotNarrowTheInstall(t *testing.T) {
	a, runner, rec := freshFixture(t)
	a.Include = []Ref{{Kind: KindRepo, Label: a.shortRepo()}}

	if err := a.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("git clone --depth 1") {
		t.Fatalf("no clone: %v", runner.ran)
	}
	// The configs phase ran over the real packages rather than over nothing.
	joined := strings.Join(rec.Texts(), "\n")
	if !strings.Contains(joined, "zsh") {
		t.Errorf("the clone happened and nothing else did: %v", rec.Texts())
	}
	if a.Include != nil {
		t.Errorf("the pre-clone selection survived: %v", a.Include)
	}
}

// A selection made *after* a clone is a real one and has to survive.
func TestASelectionOnAClonedMachineIsKept(t *testing.T) {
	a, _, _ := fixture(t, "brew", "git", "jq", "zsh")
	a.Include = Refs([]string{"zsh"})

	if err := a.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(a.Include) != 1 {
		t.Errorf("a real selection was dropped: %v", a.Include)
	}
}
