package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The MCP servers, from both ends: an install that honours the checkbox, and a
// removal that exists at all - `npm uninstall -g` was the one thing an install
// did that nothing could undo.

// errAny is a failure whose text does not matter, only that it happened.
var errAny = errors.New("npm exploded")

const mcpManifest = `{
  "app": "dotfiles", "version": "9.0.0",
  "stow_packages": [{ "name": "zsh", "platforms": ["macos"] }],
  "stow_ignore": [],
  "tools": [{ "cmd": "jq", "brew": "jq" }],
  "mcps": ["@modelcontextprotocol/server-a", "@modelcontextprotocol/server-b"]
}`

// mcpFixture declares two MCP servers and installs the named ones into a fake
// global npm root.
func mcpFixture(t *testing.T, installed ...string) (*App, *fakeRunner, *Recorder) {
	t.Helper()
	a, runner, rec := fixture(t, "brew", "npm", "jq", "zsh")
	write(t, filepath.Join(a.Repo, "manifest.jsonc"), mcpManifest)

	root := t.TempDir()
	runner.out["npm root -g"] = root
	for _, pkg := range installed {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(pkg)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return a, runner, rec
}

func TestRemovableOffersTheInstalledMcpServers(t *testing.T) {
	a, _, _ := mcpFixture(t, "@modelcontextprotocol/server-a")

	items, err := a.Removable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found *Component
	for i, item := range items {
		if item.Label == "@modelcontextprotocol/server-a" {
			found = &items[i]
		}
		if item.Label == "@modelcontextprotocol/server-b" {
			t.Errorf("an MCP server that is not installed was offered: %v", labels(items))
		}
	}
	if found == nil {
		t.Fatalf("the installed MCP server was not offered: %v", labels(items))
	}
	if found.Kind != KindMcp {
		t.Errorf("kind = %q", found.Kind)
	}
	if found.Group != "MCP servers" {
		t.Errorf("group = %q", found.Group)
	}
	if found.Selected {
		t.Error("arrived ticked: the safe action is to press enter")
	}
}

func TestANamedMcpServerIsRemovedThroughNpm(t *testing.T) {
	a, runner, rec := mcpFixture(t,
		"@modelcontextprotocol/server-a", "@modelcontextprotocol/server-b")
	a.Include = Refs([]string{"@modelcontextprotocol/server-a"})

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("npm uninstall -g @modelcontextprotocol/server-a") {
		t.Fatalf("ran: %v", runner.ran)
	}
	if !rec.Contains("removed @modelcontextprotocol/server-a") {
		t.Errorf("%v", rec.Texts())
	}
	// Only what was named, here as everywhere else.
	if runner.didRun("npm uninstall -g @modelcontextprotocol/server-b") {
		t.Errorf("something unnamed was removed: %v", runner.ran)
	}
}

func TestAnMcpServerThatIsNotInstalledIsReportedNotRemoved(t *testing.T) {
	a, runner, rec := mcpFixture(t)
	a.Include = Refs([]string{"@modelcontextprotocol/server-a"})

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains("@modelcontextprotocol/server-a (not installed)") {
		t.Errorf("%v", rec.Texts())
	}
	if len(runner.ran) != 0 {
		t.Errorf("ran: %v", runner.ran)
	}
}

func TestADryRunRemovesNoMcpServer(t *testing.T) {
	a, runner, rec := mcpFixture(t, "@modelcontextprotocol/server-a")
	a.Include = Refs([]string{"@modelcontextprotocol/server-a"})
	a.DryRun = true

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.ran) != 0 {
		t.Errorf("a dry run ran: %v", runner.ran)
	}
	if !rec.Contains("would remove @modelcontextprotocol/server-a (npm -g)") {
		t.Errorf("%v", rec.Texts())
	}
}

// npm's own refusal is the useful part, and it reaches the exit code like a
// brew refusal does.
func TestAnNpmRefusalIsReportedAndReturned(t *testing.T) {
	a, runner, rec := mcpFixture(t, "@modelcontextprotocol/server-a")
	a.Include = Refs([]string{"@modelcontextprotocol/server-a"})
	runner.onRun = func(name string, args []string) error {
		if name == "npm" {
			return errors.New("EACCES: permission denied")
		}
		return nil
	}

	err := a.RemovePackages(context.Background())
	if err == nil {
		t.Fatal("a refusal should reach the exit code")
	}
	if !rec.Contains("@modelcontextprotocol/server-a: EACCES: permission denied") {
		t.Errorf("%v", rec.Texts())
	}
}

// A tool and an MCP server in one selection, which is what ticking two boxes on
// the removal screen produces.
func TestAToolAndAnMcpServerComeOffTogether(t *testing.T) {
	a, runner, _ := mcpFixture(t, "@modelcontextprotocol/server-a")
	runner.owns("jq")
	a.Include = Refs([]string{"jq", "@modelcontextprotocol/server-a"})

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("brew uninstall jq") {
		t.Errorf("the tool was not removed: %v", runner.ran)
	}
	if !runner.didRun("npm uninstall -g @modelcontextprotocol/server-a") {
		t.Errorf("the MCP server was not removed: %v", runner.ran)
	}
}

// The selector's count is real now. It used to say "N declared", because
// `npm ls -g` was too slow to run on a menu open - which meant the one number
// on the screen described the manifest rather than the machine.
func TestTheMcpComponentCountsWhatIsInstalled(t *testing.T) {
	a, _, _ := mcpFixture(t, "@modelcontextprotocol/server-a")

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Label != mcpLabel {
			continue
		}
		if item.Status != "1 of 2 present" {
			t.Errorf("status = %q", item.Status)
		}
		if item.Done {
			t.Error("done with one of two installed")
		}
		if item.Kind != KindMcps {
			t.Errorf("kind = %q", item.Kind)
		}
		return
	}
	t.Errorf("no %q component: %v", mcpLabel, labels(items))
}

func TestTheMcpComponentIsDoneWhenAllArePresent(t *testing.T) {
	a, _, _ := mcpFixture(t,
		"@modelcontextprotocol/server-a", "@modelcontextprotocol/server-b")

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Label == mcpLabel {
			if item.Status != "2 of 2 present" || !item.Done {
				t.Errorf("status = %q done = %v", item.Status, item.Done)
			}
			return
		}
	}
	t.Error("no mcp component")
}

// Without npm there is nothing to ask and nothing to remove, and neither is an
// error: the servers are not load-bearing for a working shell.
func TestMcpRemovalWithoutNpm(t *testing.T) {
	a, runner, rec := mcpFixture(t, "@modelcontextprotocol/server-a")
	delete(runner.cmds, "npm")
	a.Include = Refs([]string{"@modelcontextprotocol/server-a"})

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatalf("no npm is not a failure: %v", err)
	}
	if strings.Contains(strings.Join(runner.ran, " "), "npm") {
		t.Errorf("npm was run without being there: %v", runner.ran)
	}
	// Nothing was offered either, because npm is where the answer comes from.
	if rec.Contains("removed @modelcontextprotocol/server-a") {
		t.Errorf("%v", rec.Texts())
	}
}

// `npm install -g` on a package that is already there takes about two seconds to
// decide it has nothing to do, so an install on a set-up machine spent fifteen
// of them reinstalling seven packages it had.
func TestAlreadyInstalledMcpServersAreNotReinstalled(t *testing.T) {
	a, runner, rec := mcpFixture(t, "@modelcontextprotocol/server-a")

	m, err := a.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	a.installMcps(context.Background(), m.Mcps)

	if runner.didRun("npm install -g @modelcontextprotocol/server-a") {
		t.Errorf("a package npm already had was reinstalled: %v", runner.ran)
	}
	if !runner.didRun("npm install -g @modelcontextprotocol/server-b") {
		t.Errorf("the missing one was not installed: %v", runner.ran)
	}
	// Counted over what was selected, so the number is about a set the reader
	// asked about.
	if !rec.Contains(mcpLabel + ": 1 of 2 selected already present") {
		t.Errorf("%v", rec.Texts())
	}
}

func TestNothingToInstallSkipsNpmEntirely(t *testing.T) {
	a, runner, rec := mcpFixture(t,
		"@modelcontextprotocol/server-a", "@modelcontextprotocol/server-b")

	m, err := a.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	a.installMcps(context.Background(), m.Mcps)

	for _, ran := range runner.ran {
		if strings.HasPrefix(ran, "npm install") {
			t.Errorf("npm was run with nothing to do: %s", ran)
		}
	}
	if !rec.Contains(mcpLabel + ": 2 selected, all present") {
		t.Errorf("%v", rec.Texts())
	}
}

// The upgrade an install used to perform as a side effect now has a home, or
// dropping the reinstall would be a regression rather than a saving.
func TestUpdateUpgradesTheMcpServers(t *testing.T) {
	a, runner, rec := mcpFixture(t, "@modelcontextprotocol/server-a")

	if err := a.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("npm update -g @modelcontextprotocol/server-a @modelcontextprotocol/server-b") {
		t.Fatalf("ran: %v", runner.ran)
	}
	// Named, never bare: `npm update -g` on its own would move every global
	// package on the machine, and the ones this repository did not install are
	// not its to touch.
	for _, ran := range runner.ran {
		if ran == "npm update -g" {
			t.Errorf("it upgraded every global package: %s", ran)
		}
	}
	if !rec.Contains("2 MCP servers up to date") {
		t.Errorf("%v", rec.Texts())
	}
	// And brew still ran, because npm is best-effort and must not stop it.
	if !runner.didRun("brew upgrade") {
		t.Errorf("the npm step displaced brew: %v", runner.ran)
	}
}

// Best-effort: a failed npm is a warning, and `brew upgrade` still happens.
func TestAFailedMcpUpgradeDoesNotStopBrew(t *testing.T) {
	a, runner, rec := mcpFixture(t, "@modelcontextprotocol/server-a")
	runner.fail = map[string]error{"npm update": errAny}

	if err := a.Update(context.Background()); err != nil {
		t.Fatalf("npm is best-effort: %v", err)
	}
	if !runner.didRun("brew upgrade") {
		t.Errorf("a failed npm stopped brew: %v", runner.ran)
	}
	if !rec.Contains("npm update failed: " + errAny.Error()) {
		t.Errorf("%v", rec.Texts())
	}
}

// Update is wholesale, including the npm third of it.
//
// `brew upgrade` and `winget upgrade --all` cannot be pointed at a subset of a
// manifest, so the menu gives Update no selector - and the npm half used to
// carry an Include gate that nothing could ever set, which made it unreachable
// and therefore untestable. Asserting the wholesale behaviour is the honest
// version: a selection cannot reach here, so it must not appear to.
func TestUpdateIsWholesale(t *testing.T) {
	a, runner, _ := mcpFixture(t, "@modelcontextprotocol/server-a")
	// Set anyway, to prove nothing reads it.
	a.Include = []Ref{{Kind: KindStow, Label: "zsh"}}

	if err := a.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("npm update -g @modelcontextprotocol/server-a @modelcontextprotocol/server-b") {
		t.Errorf("ran: %v", runner.ran)
	}
	// And brew, which never had a gate to disagree with.
	if !runner.didRun("brew upgrade") {
		t.Errorf("ran: %v", runner.ran)
	}
}

// The count is over what was selected. "7 already present" when two of seven
// were ticked was a number about a set nobody had asked about.
func TestTheMcpCountIsOverTheSelection(t *testing.T) {
	a, _, rec := mcpFixture(t,
		"@modelcontextprotocol/server-a", "@modelcontextprotocol/server-b")
	a.Include = []Ref{
		{Kind: KindMcps, Label: mcpLabel},
		{Kind: KindMcp, Label: "@modelcontextprotocol/server-a"},
	}

	m, err := a.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	a.installMcps(context.Background(), m.Mcps)

	if !rec.Contains(mcpLabel + ": 1 selected, all present") {
		t.Errorf("%v", rec.Texts())
	}
	joined := strings.Join(rec.Texts(), "\n")
	if strings.Contains(joined, "2 selected") || strings.Contains(joined, "of 2") {
		t.Errorf("it counted the servers nobody ticked: %v", rec.Texts())
	}
}
