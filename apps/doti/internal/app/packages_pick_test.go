package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
	"github.com/riptone/tone.rip/apps/doti/internal/pkgs"
)

// Picking individual packages, which is what the selector's fold made possible.
//
// The gate used to be one label - `brew packages` - so the only answers were all
// and nothing. Every list the phase installs is narrowed on its own now, and the
// tests that matter are the ones about the lists *not* named: unticking one tool
// must not decline every GUI app as a side effect.

const pickManifest = `{
  "app": "dotfiles", "version": "9.0.0",
  "stow_packages": [{ "name": "zsh", "platforms": ["macos"] }],
  "stow_ignore": [],
  "tools": [
    { "cmd": "jq", "brew": "jq", "winget": "jqlang.jq" },
    { "cmd": "fd", "brew": "fd", "winget": "sharkdp.fd" },
    { "cmd": "rg", "brew": "ripgrep" },
    { "cmd": "code", "app": "Visual Studio Code" }
  ],
  "zsh_plugins": [{ "brew": "zsh-autosuggestions" }],
  "brew_casks": [
    { "brew": "ghostty", "platforms": ["macos"] },
    { "brew": "brave-browser", "platforms": ["macos"] }
  ],
  "winget_extras": ["Brave.Brave", "Microsoft.PowerShell"],
  "mcps": ["server-a", "server-b"]
}`

func pickFixture(t *testing.T, installed ...string) (*App, *fakeRunner, *Recorder) {
	t.Helper()
	a, runner, rec := fixture(t, append([]string{"brew", "npm"}, installed...)...)
	write(t, filepath.Join(a.Repo, "manifest.jsonc"), pickManifest)
	return a, runner, rec
}

// brewfile is the file `brew bundle` was handed, read back off disk.
//
// Read rather than reconstructed: the point is what brew actually got, and a
// test that re-renders the body would agree with the renderer by construction.
func brewfile(t *testing.T, runner *fakeRunner) string {
	t.Helper()
	for _, ran := range runner.ran {
		_, path, found := strings.Cut(ran, "--file=")
		if !found {
			continue
		}
		// The temp file is removed when InstallPackages returns, so the runner
		// keeps a copy - see onRun below.
		return runner.files[path]
	}
	t.Fatalf("brew bundle was never run: %v", runner.ran)
	return ""
}

// keepBundleFile snapshots whatever `brew bundle --file=` is handed, because the
// temp file is gone by the time the test looks.
func keepBundleFile(t *testing.T, runner *fakeRunner) {
	t.Helper()
	runner.files = map[string]string{}
	runner.onRun = func(name string, args []string) error {
		for _, arg := range args {
			if path, found := strings.CutPrefix(arg, "--file="); found {
				body, err := readFile(path)
				if err != nil {
					t.Errorf("reading the Brewfile handed to brew: %v", err)
					return nil
				}
				runner.files[path] = body
			}
		}
		return nil
	}
}

func TestEveryToolIsOfferedIndividually(t *testing.T) {
	a, _, _ := pickFixture(t, "jq")

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var parent *Component
	got := map[string]Component{}
	for i, item := range items {
		switch item.Kind {
		case KindTools:
			parent = &items[i]
		case KindTool:
			got[item.Label] = item
			if item.Parent != packagesLabel {
				t.Errorf("%q folds under %q", item.Label, item.Parent)
			}
		}
	}
	if parent == nil {
		t.Fatalf("no %q parent: %v", packagesLabel, labels(items))
	}
	// `code` has no brew name on macOS, so there is nothing to install and no
	// choice to offer - a box that does nothing is what this change removes.
	if _, offered := got["code"]; offered {
		t.Errorf("a tool with no package name was offered: %v", labels(items))
	}
	if len(got) != 3 {
		t.Errorf("offered %d tools, want 3: %v", len(got), labels(items))
	}
	if got["jq"].Status != "installed" || !got["jq"].Done {
		t.Errorf("jq reads %q (done=%v)", got["jq"].Status, got["jq"].Done)
	}
	if got["fd"].Status != "missing" || got["fd"].Done {
		t.Errorf("fd reads %q (done=%v)", got["fd"].Status, got["fd"].Done)
	}
	if parent.Status != "1 of 3 present" {
		t.Errorf("the parent reads %q", parent.Status)
	}
	for _, item := range got {
		if !item.Selected {
			t.Errorf("%q arrived unticked", item.Label)
		}
	}
}

func TestTheCasksAndPluginsAreOfferedIndividually(t *testing.T) {
	a, _, _ := pickFixture(t)

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byKind := map[Kind][]string{}
	for _, item := range items {
		byKind[item.Kind] = append(byKind[item.Kind], item.Label)
	}
	for kind, want := range map[Kind][]string{
		KindCasks:   {casksLabel},
		KindCask:    {"ghostty", "brave-browser"},
		KindPlugins: {pluginsLabel},
		KindPlugin:  {"zsh-autosuggestions"},
		KindMcps:    {mcpLabel},
		KindMcp:     {"server-a", "server-b"},
	} {
		if strings.Join(byKind[kind], ",") != strings.Join(want, ",") {
			t.Errorf("%s = %v, want %v", kind, byKind[kind], want)
		}
	}
	// Not on a Mac's list: winget is the other platform's answer.
	if len(byKind[KindWingetExtra]) != 0 {
		t.Errorf("winget extras on macOS: %v", byKind[KindWingetExtra])
	}
}

func TestWindowsOffersTheWingetExtrasInsteadOfCasks(t *testing.T) {
	a, _, _ := pickFixture(t)
	a.Platform = manifest.Windows

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byKind := map[Kind][]string{}
	for _, item := range items {
		byKind[item.Kind] = append(byKind[item.Kind], item.Label)
	}
	if strings.Join(byKind[KindWingetExtra], ",") != "Brave.Brave,Microsoft.PowerShell" {
		t.Errorf("winget extras = %v", byKind[KindWingetExtra])
	}
	for _, absent := range []Kind{KindCask, KindCasks, KindPlugin, KindPlugins} {
		if len(byKind[absent]) != 0 {
			t.Errorf("%s on Windows: %v", absent, byKind[absent])
		}
	}
	// `code` has a winget id in this manifest? It does not - but rg has no
	// winget id either, so the tool list narrows to the two that do.
	if strings.Join(byKind[KindTool], ",") != "jq,fd" {
		t.Errorf("tools = %v, want the two with a winget id", byKind[KindTool])
	}
}

// The point of the whole exercise: one tool, and only that tool.
func TestOnlyTheTickedToolsReachTheBundle(t *testing.T) {
	a, runner, _ := pickFixture(t)
	keepBundleFile(t, runner)
	a.Include = []Ref{
		{Kind: KindTools, Label: packagesLabel},
		{Kind: KindTool, Label: "fd"},
	}

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	body := brewfile(t, runner)
	if !strings.Contains(body, `brew "fd"`) {
		t.Errorf("the ticked tool is missing:\n%s", body)
	}
	for _, absent := range []string{`brew "jq"`, `brew "ripgrep"`} {
		if strings.Contains(body, absent) {
			t.Errorf("an unticked tool is in the bundle (%s):\n%s", absent, body)
		}
	}
}

// The trap this design exists to avoid: declining one tool must not decline
// every GUI app and both zsh plugins along with it. There used to be two
// renderers, and picking the tools-only one because a tool was unticked would
// have done exactly that.
func TestUntickingAToolKeepsTheCasksAndPlugins(t *testing.T) {
	a, runner, _ := pickFixture(t)
	keepBundleFile(t, runner)
	a.Include = []Ref{
		{Kind: KindTools, Label: packagesLabel},
		{Kind: KindTool, Label: "fd"},
		{Kind: KindCasks, Label: casksLabel},
		{Kind: KindPlugins, Label: pluginsLabel},
	}

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	body := brewfile(t, runner)
	for _, want := range []string{
		`cask "ghostty" if OS.mac?`,
		`cask "brave-browser" if OS.mac?`,
		`brew "zsh-autosuggestions"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s went missing because a tool was unticked:\n%s", want, body)
		}
	}
}

// And the other direction: unticking one cask keeps the tools.
func TestUntickingACaskKeepsEverythingElse(t *testing.T) {
	a, runner, _ := pickFixture(t)
	keepBundleFile(t, runner)
	a.Include = []Ref{
		{Kind: KindTools, Label: packagesLabel},
		{Kind: KindCasks, Label: casksLabel},
		{Kind: KindCask, Label: "ghostty"},
		{Kind: KindPlugins, Label: pluginsLabel},
	}

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	body := brewfile(t, runner)
	if !strings.Contains(body, `cask "ghostty"`) {
		t.Errorf("the ticked cask is missing:\n%s", body)
	}
	if strings.Contains(body, "brave-browser") {
		t.Errorf("an unticked cask is in the bundle:\n%s", body)
	}
	for _, want := range []string{`brew "jq"`, `brew "fd"`, `brew "ripgrep"`} {
		if !strings.Contains(body, want) {
			t.Errorf("%s went missing because a cask was unticked:\n%s", want, body)
		}
	}
}

// Naming a parent and nothing under it means all of it. That is what a caller
// who does not know about the children looks like, and reading it as "none"
// would turn `Include: {brew packages}` into an install of nothing.
func TestNamingOnlyTheParentMeansAllOfIt(t *testing.T) {
	a, runner, _ := pickFixture(t)
	keepBundleFile(t, runner)
	a.Include = []Ref{{Kind: KindTools, Label: packagesLabel}}

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	body := brewfile(t, runner)
	for _, want := range []string{`brew "jq"`, `brew "fd"`, `brew "ripgrep"`} {
		if !strings.Contains(body, want) {
			t.Errorf("%s is missing from a whole-group selection:\n%s", want, body)
		}
	}
}

// Nothing under any of this phase's lists is how the selector spells "skip the
// packages".
func TestNoPackagesTickedSkipsThePhase(t *testing.T) {
	a, runner, rec := pickFixture(t)
	a.Include = []Ref{{Kind: KindStow, Label: "zsh"}}

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.ran) != 0 {
		t.Errorf("ran: %v", runner.ran)
	}
	if !rec.Contains(packagesLabel + " (not selected)") {
		t.Errorf("%v", rec.Texts())
	}
}

// Only the ticked MCP servers, one npm call each.
func TestOnlyTheTickedMcpServersAreInstalled(t *testing.T) {
	a, runner, _ := pickFixture(t)
	a.Include = []Ref{
		{Kind: KindMcps, Label: mcpLabel},
		{Kind: KindMcp, Label: "server-b"},
	}

	m, err := a.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	a.installMcps(context.Background(), m.Mcps)

	if !runner.didRun("npm install -g server-b") {
		t.Errorf("the ticked server was not installed: %v", runner.ran)
	}
	if runner.didRun("npm install -g server-a") {
		t.Errorf("an unticked server was installed: %v", runner.ran)
	}
}

// A dry run says how much of each list it would touch, rather than naming
// twenty-three packages on one line nobody reads to the end.
func TestADryRunCountsWhatItWouldInstall(t *testing.T) {
	a, _, rec := pickFixture(t)
	a.DryRun = true

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rec.Texts(), "\n")
	for _, want := range []string{"3 tool(s)", "2 cask(s)", "1 zsh plugin(s)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the dry run does not say %q: %v", want, rec.Texts())
		}
	}
}

// Windows narrows its own two lists into the import file.
func TestOnlyTheTickedWingetPackagesAreImported(t *testing.T) {
	a, runner, _ := pickFixture(t)
	a.Platform = manifest.Windows
	runner.cmds["winget"] = true
	runner.files = map[string]string{}
	runner.onRun = func(name string, args []string) error {
		for i, arg := range args {
			if arg == "-i" && i+1 < len(args) {
				body, err := readFile(args[i+1])
				if err != nil {
					t.Errorf("reading the import file: %v", err)
					return nil
				}
				runner.files["import"] = body
			}
		}
		return nil
	}
	a.Include = []Ref{
		{Kind: KindTools, Label: packagesLabel},
		{Kind: KindTool, Label: "jq"},
		{Kind: KindWingetExtras, Label: wingetExtrasLabel},
		{Kind: KindWingetExtra, Label: "Brave.Brave"},
	}

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	body := runner.files["import"]
	if !strings.Contains(body, "jqlang.jq") || !strings.Contains(body, "Brave.Brave") {
		t.Errorf("the ticked packages are missing:\n%s", body)
	}
	for _, absent := range []string{"sharkdp.fd", "Microsoft.PowerShell"} {
		if strings.Contains(body, absent) {
			t.Errorf("%s was imported anyway:\n%s", absent, body)
		}
	}
}

// --tools names tools and nothing else, which is older than the selector and
// still right: installing one missing tool should not also pull in every cask
// and zsh plugin the full bundle carries.
func TestTheToolsFlagStillExcludesTheCasksAndPlugins(t *testing.T) {
	a, runner, _ := pickFixture(t)
	keepBundleFile(t, runner)
	a.Tools = "fd"

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	body := brewfile(t, runner)
	if !strings.Contains(body, `brew "fd"`) {
		t.Errorf("the named tool is missing:\n%s", body)
	}
	for _, absent := range []string{"ghostty", "brave-browser", "zsh-autosuggestions", `brew "jq"`} {
		if strings.Contains(body, absent) {
			t.Errorf("--tools pulled in %s:\n%s", absent, body)
		}
	}
}

// And a selection supersedes it, being the more specific answer to the same
// question.
func TestASelectionSupersedesTheToolsFlagForTheOtherLists(t *testing.T) {
	a, runner, _ := pickFixture(t)
	keepBundleFile(t, runner)
	a.Tools = "fd"
	a.Include = []Ref{
		{Kind: KindTools, Label: packagesLabel},
		{Kind: KindTool, Label: "fd"},
		{Kind: KindCasks, Label: casksLabel},
		{Kind: KindCask, Label: "ghostty"},
	}

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	body := brewfile(t, runner)
	if !strings.Contains(body, `cask "ghostty"`) {
		t.Errorf("the ticked cask was dropped by --tools:\n%s", body)
	}
	if strings.Contains(body, "brave-browser") {
		t.Errorf("an unticked cask came along:\n%s", body)
	}
}

// With nothing narrowed at all, the bundle is byte-for-byte the file the whole
// manifest renders. `doti install` cannot have changed shape because the window
// gained checkboxes.
func TestAnUnnarrowedInstallRendersTheWholeManifest(t *testing.T) {
	a, runner, _ := pickFixture(t)
	keepBundleFile(t, runner)

	m, err := a.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := brewfile(t, runner), pkgs.Brewfile(m); got != want {
		t.Errorf("the generated bundle differs from the manifest's\n--- got\n%s\n--- want\n%s",
			got, want)
	}
}

// The count is about the machine, not about the selection.
//
// It used to say "all N tools present" whenever nothing *selected* was missing,
// so ticking one present tool on a machine missing another reported every tool
// as present. The reader wants to know where they stand before they read what
// this run would do about it, and a count that changes meaning with the
// selection cannot tell them.
func TestThePresentCountDescribesTheMachineNotTheSelection(t *testing.T) {
	// jq is there; fd and rg are not. `code` is declared with no brew name, so
	// it is not one of the three this platform can install and is not counted.
	a, _, rec := pickFixture(t, "jq")
	a.DryRun = true
	a.Include = []Ref{
		{Kind: KindTools, Label: packagesLabel},
		{Kind: KindTool, Label: "jq"},
	}

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rec.Texts(), "\n")
	if !strings.Contains(joined, "1 of 3 tools present") {
		t.Errorf("the count does not describe the machine: %v", rec.Texts())
	}
	for _, lie := range []string{"all 3 tools present", "all 1 tools present"} {
		if strings.Contains(joined, lie) {
			t.Errorf("%q: %v", lie, rec.Texts())
		}
	}
	// And nothing selected is missing, so there is nothing it would install -
	// only the bundle it would render.
	if strings.Contains(joined, "would install") {
		t.Errorf("it claims it would install something present: %v", rec.Texts())
	}
	if !strings.Contains(joined, "the bundle covers 1 tool(s)") {
		t.Errorf("%v", rec.Texts())
	}
}

// The casks and the plugins get a real present/missing answer, not just a
// declared count.
//
// It mattered beyond tidiness: the Adopt selector shows what is *left*, and a
// row with no present/missing answer can only ever be shown - so every GUI app
// and both plugins appeared on a list of "what is missing" on a machine that had
// them all.
func TestTheCasksAndPluginsCarryTheirRealState(t *testing.T) {
	a, runner, _ := pickFixture(t)
	runner.owns("zsh-autosuggestions", "ghostty")

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Component{}
	for _, item := range items {
		got[item.Label] = item
	}
	for label, want := range map[string]struct {
		status string
		done   bool
	}{
		"ghostty":             {"installed", true},
		"brave-browser":       {"missing", false},
		"zsh-autosuggestions": {"installed", true},
		casksLabel:            {"1 of 2 present", false},
		pluginsLabel:          {"1 of 1 present", true},
	} {
		if got[label].Status != want.status || got[label].Done != want.done {
			t.Errorf("%s reads %q (done=%v), want %q (done=%v)",
				label, got[label].Status, got[label].Done, want.status, want.done)
		}
	}
}

// PATH is the wrong question for a cask in both directions: it puts nothing on
// PATH, and a font is not a command at all.
func TestACaskIsNotJudgedByPath(t *testing.T) {
	a, runner, _ := pickFixture(t, "ghostty")
	// On PATH, and brew has never heard of it.
	runner.owns()

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Kind == KindCask && item.Label == "ghostty" && item.Done {
			t.Error("a cask brew does not own reads as installed")
		}
	}
}

// The inventory is asked once per invocation, not once per caller: one screen
// asks from two places, and on Windows the question is `winget export`.
func TestTheInventoryIsAskedOncePerInvocation(t *testing.T) {
	a, runner, _ := pickFixture(t)
	runner.owns("ghostty")

	if _, err := a.MenuItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Removable(context.Background()); err != nil {
		t.Fatal(err)
	}
	var asks int
	for _, asked := range runner.asked {
		if strings.HasPrefix(asked, "brew list") {
			asks++
		}
	}
	// Formulae and casks: two calls, one inventory.
	if asks != 2 {
		t.Errorf("brew list ran %d times: %v", asks, runner.asked)
	}
	// And npm once, for the same reason: both lists ask about the same set.
	var npmAsks int
	for _, asked := range runner.asked {
		if asked == "npm root -g" {
			npmAsks++
		}
	}
	if npmAsks != 1 {
		t.Errorf("npm root -g ran %d times: %v", npmAsks, runner.asked)
	}

	// And a re-scan drops it, because the window re-reads the machine after
	// every run - see App.Forget.
	a.Forget()
	if _, err := a.MenuItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	asks = 0
	for _, asked := range runner.asked {
		if strings.HasPrefix(asked, "brew list") {
			asks++
		}
	}
	if asks != 4 {
		t.Errorf("Forget did not drop the inventory: %v", runner.asked)
	}
}

// The run and the screen that launched it report the same number.
//
// They did not: `code` is declared with a winget id and no brew formula, so on a
// Mac the run said "16 of 16 tools present" one line above "the bundle covers 15
// tool(s)" while the selector said "15 of 15". Three numbers, one fact.
func TestTheRunAndTheSelectorCountTheSameTools(t *testing.T) {
	a, _, rec := pickFixture(t, "jq")
	a.DryRun = true

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var fromSelector string
	for _, item := range items {
		if item.Kind == KindTools {
			fromSelector = item.Status
		}
	}
	if fromSelector == "" {
		t.Fatal("no tools component")
	}

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rec.Texts(), "\n")
	// "1 of 3 present" on the selector, "1 of 3 tools present" in the run.
	if !strings.Contains(joined, strings.Replace(fromSelector, "present", "tools present", 1)) {
		t.Errorf("the selector says %q and the run says %v", fromSelector, rec.Texts())
	}
	// And the bundle covers exactly that many, so the two lines cannot read as
	// contradicting each other.
	if !strings.Contains(joined, "the bundle covers 3 tool(s)") {
		t.Errorf("%v", rec.Texts())
	}
}
