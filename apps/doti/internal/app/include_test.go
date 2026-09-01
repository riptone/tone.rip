package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// names is the package names in a plan, for a readable failure.
func names(pkgs []manifest.StowPackage) []string {
	out := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		out = append(out, pkg.Name)
	}
	return out
}

func asUnknown(err error, target **UnknownOperationError) bool {
	return errors.As(err, target)
}

// The window's checkboxes used to do nothing: the selector collected what had
// been ticked and the code that ran the operation never asked. These are the
// four places that now do.

func TestIncludeNarrowsTheConfigPackages(t *testing.T) {
	a, _, _ := fixture(t)

	all, err := a.Packages()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("the fixture should offer two packages, got %d", len(all))
	}

	a.Include = Refs([]string{"zsh"})
	some, err := a.Packages()
	if err != nil {
		t.Fatal(err)
	}
	if len(some) != 1 || some[0].Name != "zsh" {
		t.Fatalf("Include gave %v, want zsh alone", names(some))
	}
}

// The common case, and the one every command-line invocation is in.
func TestAnEmptyIncludeIsEverything(t *testing.T) {
	a, _, _ := fixture(t)
	a.Include = nil
	all, err := a.Packages()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("an empty Include gave %v, want both", names(all))
	}
}

// The tool set is one component under one label, and the installer checks the
// same string the selector offers - a constant, because two spellings is a
// checkbox that silently does nothing.
func TestIncludeCanLeaveThePackagesOut(t *testing.T) {
	a, _, rec := fixture(t)
	a.Include = Refs([]string{"zsh"})
	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains(packagesLabel + " (not selected)") {
		t.Errorf("the skip was not reported: %v", rec.Texts())
	}
	if rec.Marked(MarkChange) != 0 {
		t.Error("something was installed anyway")
	}
}

func TestIncludeGatesTheExtras(t *testing.T) {
	a, _, _ := fixture(t)
	// The fixture declares no extras, so a missing one is false either way;
	// what matters is that Include is consulted before the manifest is.
	a.Include = Refs([]string{"nothing-like-this"})
	if a.WantsExtra("nerd-font") {
		t.Error("an unticked extra was still wanted")
	}
}

func TestUnlinkHonoursInclude(t *testing.T) {
	a, _, rec := fixture(t)
	if err := a.Link(); err != nil {
		t.Fatal(err)
	}
	rec.Records = nil

	a.Include = Refs([]string{"zsh"})
	if err := a.Unlink(false); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rec.Texts(), "\n")
	if !strings.Contains(joined, "zsh") {
		t.Errorf("zsh was not unlinked: %v", rec.Texts())
	}
	if strings.Contains(joined, "ghostty") {
		t.Errorf("an unticked package was unlinked anyway: %v", rec.Texts())
	}
}

// Do is the one place a name becomes a call, so it is also the one place
// Include is set - rather than nine signatures carrying it.
func TestDoAppliesIncludeAndRefusesAnUnknownOperation(t *testing.T) {
	a, _, _ := fixture(t)
	if err := a.Do(context.Background(), OpCheck, Refs([]string{"zsh"}), "v1.0.0"); err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(a.Include) != 1 || a.Include[0].Label != "zsh" {
		t.Errorf("Do did not apply Include: %v", a.Include)
	}

	err := a.Do(context.Background(), Operation("dance"), nil, "v1.0.0")
	var unknown *UnknownOperationError
	if err == nil || !asUnknown(err, &unknown) {
		t.Fatalf("err = %v, want an UnknownOperationError", err)
	}
	if !strings.Contains(err.Error(), "dance") {
		t.Errorf("the error does not name the operation: %v", err)
	}
}

// Preview is install with nothing written - two names for one path, rather
// than a second path reporting what the first would do.
func TestPreviewIsADryRunInstall(t *testing.T) {
	a, _, _ := fixture(t)
	if err := a.Do(context.Background(), OpPreview, nil, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if !a.DryRun {
		t.Error("preview did not set DryRun")
	}
}

// Scan hands the system links to the health check. They are Windows-only, so
// this forces the platform rather than skipping - the mapping is the thing
// worth pinning, and it is the same code on every machine.
func TestScanHandsTheSystemLinksToTheHealthCheck(t *testing.T) {
	a, _, _ := fixture(t)
	a.Platform = manifest.Windows
	t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "Local"))
	t.Setenv("USERPROFILE", filepath.Join(t.TempDir(), "User"))

	links := a.SystemLinks()
	if len(links) != 2 {
		t.Fatalf("the fixture environment should offer two system links, got %d", len(links))
	}

	report, err := a.Scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range links {
		var found bool
		for _, finding := range report.Findings {
			if finding.Name == link.Name {
				found = true
				if finding.OK {
					t.Errorf("%s passed, but nothing was ever linked", link.Name)
				}
			}
		}
		if !found {
			t.Errorf("%s is installed but not checked - drift in it would be invisible", link.Name)
		}
	}
}

// And they cost nothing where they do not exist.
func TestScanAddsNoSystemLinksOffWindows(t *testing.T) {
	a, _, _ := fixture(t)
	if got := a.SystemLinks(); got != nil {
		t.Errorf("macOS offered %d system links", len(got))
	}
	report, err := a.Scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range report.Findings {
		if finding.Name == "windows-terminal" || finding.Name == "powershell-profile" {
			t.Errorf("a Windows link was checked on macOS: %+v", finding)
		}
	}
}

// The one thing an install did that the selector never offered: untick every
// box and seven MCP servers were still installed, because the phase that
// installs them was never asked.
func TestIncludeGatesTheMcpServers(t *testing.T) {
	mcps := `"mcps": ["@modelcontextprotocol/server-a"],`

	t.Run("offered as a component", func(t *testing.T) {
		a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh", "npm")
		write(t, filepath.Join(a.Repo, "manifest.jsonc"),
			strings.Replace(fixtureManifest, `"health"`, mcps+`
			 "health"`, 1))

		items, err := a.MenuItems(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, item := range items {
			if item.Label == mcpLabel {
				found = true
				if item.Group != "Packages" {
					t.Errorf("grouped under %q", item.Group)
				}
				// A real count, where this used to say "N declared" because
				// `npm ls -g` was too slow to run on a menu open.
				if item.Status != "0 of 1 present" {
					t.Errorf("status = %q", item.Status)
				}
				if !item.Selected {
					t.Error("arrived unticked")
				}
			}
		}
		if !found {
			t.Errorf("no %q component: %v", mcpLabel, items)
		}
	})

	t.Run("unticked installs nothing", func(t *testing.T) {
		a, runner, rec := fixture(t, "brew", "jq", "ghostty", "zsh", "npm")
		write(t, filepath.Join(a.Repo, "manifest.jsonc"),
			strings.Replace(fixtureManifest, `"health"`, mcps+`
			 "health"`, 1))
		a.Include = Refs([]string{"zsh"})

		if err := a.Install(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !rec.Contains(mcpLabel + " (not selected)") {
			t.Errorf("the skip was not reported: %v", rec.Texts())
		}
		for _, ran := range runner.ran {
			if strings.HasPrefix(ran, "npm install") {
				t.Errorf("an unticked component installed %s", ran)
			}
		}
	})

	t.Run("ticked installs them", func(t *testing.T) {
		a, runner, _ := fixture(t, "brew", "jq", "ghostty", "zsh", "npm")
		write(t, filepath.Join(a.Repo, "manifest.jsonc"),
			strings.Replace(fixtureManifest, `"health"`, mcps+`
			 "health"`, 1))
		a.Include = Refs([]string{mcpLabel})

		if err := a.Install(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !runner.didRun("npm install -g @modelcontextprotocol/server-a") {
			t.Fatalf("ran: %v", runner.ran)
		}
	})

	// And a manifest with none offers no component rather than an empty one.
	t.Run("none declared offers nothing", func(t *testing.T) {
		a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
		items, err := a.MenuItems(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.Label == mcpLabel {
				t.Errorf("offered %q with none declared", mcpLabel)
			}
		}
	})
}

// system_components is a manifest list exactly like extras, and extras honoured
// the selector while these did not - so unticking the Windows Terminal settings
// still replaced them.
func TestIncludeGatesTheSystemLinks(t *testing.T) {
	const declared = `"system_components": [
	   {"name":"windows-terminal","platforms":["windows"]},
	   {"name":"powershell-profile","platforms":["windows"]}
	 ],`

	setup := func(t *testing.T) (*App, *Recorder) {
		t.Helper()
		a, _, rec := fixture(t)
		a.Platform = manifest.Windows
		t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "Local"))
		t.Setenv("USERPROFILE", filepath.Join(t.TempDir(), "User"))
		write(t, filepath.Join(a.Repo, "manifest.jsonc"),
			strings.Replace(fixtureManifest, `"health"`, declared+`
			 "health"`, 1))
		for _, rel := range []string{
			filepath.Join("win", "terminal", "settings.json"),
			filepath.Join("win", "powershell", "profile.ps1"),
		} {
			write(t, filepath.Join(a.Repo, rel), "{}\n")
		}
		return a, rec
	}

	t.Run("offered as components", func(t *testing.T) {
		a, _ := setup(t)
		items, err := a.MenuItems(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		offered := map[string]bool{}
		for _, item := range items {
			offered[item.Label] = true
		}
		for _, want := range []string{"windows-terminal", "powershell-profile"} {
			if !offered[want] {
				t.Errorf("%q is installed but not offered: %v", want, labels(items))
			}
		}
	})

	t.Run("unticked links nothing", func(t *testing.T) {
		a, rec := setup(t)
		a.Include = Refs([]string{"powershell-profile"})

		if err := a.installSystemLinks(); err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(rec.Texts(), "\n")
		if !strings.Contains(joined, "powershell-profile") {
			t.Errorf("the ticked link was skipped: %v", rec.Texts())
		}
		if strings.Contains(joined, "windows-terminal") {
			t.Errorf("an unticked link was installed anyway: %v", rec.Texts())
		}
	})

	// Off Windows there are none to offer, so the selector does not grow a
	// group of things nobody can act on.
	t.Run("none offered off windows", func(t *testing.T) {
		a, _, _ := fixture(t)
		items, err := a.MenuItems(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.Label == "windows-terminal" {
				t.Errorf("a Windows link was offered on %s", a.Platform)
			}
		}
	})
}

// ~/.gitconfig.local was the last thing an install wrote that the selector
// never offered. The argument for leaving it out - one derived line is not a
// decision - was true about the line and wrong about the file: it still appears
// in somebody's home directory, and "the list is everything an install does" is
// worth more than only listing declared things.
func TestIncludeGatesTheGitLocalFile(t *testing.T) {
	path := func(a *App) string { return filepath.Join(a.Home, ".gitconfig.local") }

	t.Run("offered as a component", func(t *testing.T) {
		a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")

		items, err := a.MenuItems(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.Label != gitLocalName {
				continue
			}
			if item.Group != "Configs" || item.Kind != KindGitLocal {
				t.Errorf("group=%q kind=%q", item.Group, item.Kind)
			}
			if item.Status != "not written" || item.Done {
				t.Errorf("status=%q done=%v before anything is written", item.Status, item.Done)
			}
			if !item.Selected {
				t.Error("arrived unticked")
			}
			return
		}
		t.Errorf("no %q component: %v", gitLocalName, items)
	})

	t.Run("unticked writes nothing", func(t *testing.T) {
		a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
		a.Include = Refs([]string{"zsh"})

		if err := a.writeGitLocal(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path(a)); !os.IsNotExist(err) {
			t.Error("an unticked component wrote its file")
		}
		if !rec.Contains(gitLocalName + " (not selected)") {
			t.Errorf("a skipped step said nothing: %v", rec.Texts())
		}
	})

	t.Run("ticked writes it", func(t *testing.T) {
		a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
		a.Include = Refs([]string{gitLocalName})

		if err := a.writeGitLocal(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path(a)); err != nil {
			t.Errorf("a ticked component wrote nothing: %v", err)
		}
	})

	t.Run("an empty selection is everything, as on the command line", func(t *testing.T) {
		a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")

		if err := a.writeGitLocal(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path(a)); err != nil {
			t.Errorf("`doti install` did not write it: %v", err)
		}
	})

	t.Run("the component says when it is written", func(t *testing.T) {
		a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
		if err := a.writeGitLocal(); err != nil {
			t.Fatal(err)
		}

		items, err := a.MenuItems(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.Label == gitLocalName {
				if item.Status != "written" || !item.Done {
					t.Errorf("status=%q done=%v", item.Status, item.Done)
				}
				return
			}
		}
		t.Error("no component")
	})

	// The third state, and the one worth showing: ticking the box does nothing
	// because the secrets phase owns the file, and the reader deserves to know
	// that before they tick it.
	t.Run("the component says when a secret owns it", func(t *testing.T) {
		a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
		write(t, filepath.Join(a.Repo, "manifest.jsonc"),
			strings.Replace(fixtureManifest, `"health"`,
				`"secrets": [{"name":"gitconfig-local","mode":"file","item":"x",
				  "target":"~/.gitconfig.local"}], "health"`, 1))

		items, err := a.MenuItems(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.Label == gitLocalName {
				if item.Status != "rendered from a secret" || !item.Done {
					t.Errorf("status=%q done=%v", item.Status, item.Done)
				}
				return
			}
		}
		t.Error("no component")
	})
}

// One row about one file. system_components declares `gitconfig-local` on all
// three platforms, and SystemLinks() has never returned anything by that name -
// so the manifest's declaration is the selector's entry for it, and adding a
// second row of my own would have put two checkboxes on one file.
func TestTheGitLocalComponentIsNotDuplicated(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
	write(t, filepath.Join(a.Repo, "manifest.jsonc"),
		strings.Replace(fixtureManifest, `"health"`,
			`"system_components": [
			   {"name":"gitconfig-local","platforms":["macos","linux","windows"]},
			   {"name":"windows-terminal","platforms":["windows"]}
			 ], "health"`, 1))

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found []Component
	for _, item := range items {
		if item.Label == gitLocalName {
			found = append(found, item)
		}
		if item.Label == "windows-terminal" {
			t.Error("a windows-only system component was offered on macOS")
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d rows about ~/.gitconfig.local: %v", len(found), found)
	}
	// And it carries writeGitLocal's state, not the "system link" the others
	// get - because it is not one.
	if found[0].Kind != KindGitLocal || found[0].Status != "not written" {
		t.Errorf("kind=%q status=%q", found[0].Kind, found[0].Status)
	}
}

// And it is offered whether or not the manifest declares it, because an install
// writes it either way. A checkbox that exists only for some manifests is a step
// that silently stops happening for the others.
func TestTheGitLocalComponentIsOfferedWithoutADeclaration(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for _, item := range items {
		if item.Kind == KindGitLocal {
			count++
		}
	}
	if count != 1 {
		t.Errorf("%d git-local components with nothing declared", count)
	}
}

// Every phase says when it was skipped. The configs one used to fall through
// silently, so an install with them unticked printed a `configs` heading with
// nothing under it - which reads as a step that broke rather than one nobody
// asked for.
func TestASkippedPhaseSaysSo(t *testing.T) {
	for _, tc := range []struct {
		name    string
		include []Ref
		want    string
	}{
		{"no configs", []Ref{{Kind: KindSecret, Label: "nothing-like-this"}},
			"no configs selected"},
		{"no packages", []Ref{{Kind: KindStow, Label: "zsh"}},
			packagesLabel + " (not selected)"},
		{"no git-local", []Ref{{Kind: KindStow, Label: "zsh"}},
			gitLocalName + " (not selected)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
			a.Include = tc.include

			if err := a.Install(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !rec.Contains(tc.want) {
				t.Errorf("missing %q: %v", tc.want, rec.Texts())
			}
		})
	}
}

// And a phase that ran says nothing of the sort.
func TestAPhaseThatRanDoesNotSayItWasSkipped(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")

	if err := a.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rec.Texts(), "\n")
	for _, absent := range []string{"no configs selected", "(not selected)"} {
		if strings.Contains(joined, absent) {
			t.Errorf("an unnarrowed install said %q: %v", absent, rec.Texts())
		}
	}
}

// The manifest may declare any extra; only the ones with code behind them are
// offered. install.go used to ask for "nerd-font" by name while the selector
// drew a row for every declared extra, so a second one would have been ticked
// and then ignored - a checkbox that does nothing, which is the thing every
// other gate here removed.
func TestOnlyExtrasWithCodeBehindThemAreOffered(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
	a.Platform = manifest.Linux
	write(t, filepath.Join(a.Repo, "manifest.jsonc"),
		strings.Replace(fixtureManifest, `"health"`,
			`"extras": [
			   {"name":"nerd-font","platforms":["linux"]},
			   {"name":"something-nobody-implemented","platforms":["linux"]}
			 ], "health"`, 1))

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var offered []string
	for _, item := range items {
		if item.Kind == KindExtra {
			offered = append(offered, item.Label)
		}
	}
	if strings.Join(offered, ",") != "nerd-font" {
		t.Errorf("offered %v, want the nerd font alone", offered)
	}
	// And the one that is offered has a real state, so Adopt can drop it: it
	// used to read "not checked" forever, which made it the one row a list of
	// "what the machine is missing" could never lose.
	for _, item := range items {
		if item.Kind != KindExtra {
			continue
		}
		if item.Status == "not checked" {
			t.Errorf("%q still has no state", item.Label)
		}
		if item.Status != "missing" || item.Done {
			t.Errorf("status=%q done=%v with no font installed", item.Status, item.Done)
		}
	}
}

// And once the faces are there it says so, and Adopt drops it.
func TestTheNerdFontExtraSaysWhenItIsInstalled(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
	a.Platform = manifest.Linux
	write(t, filepath.Join(a.Repo, "manifest.jsonc"),
		strings.Replace(fixtureManifest, `"health"`,
			`"extras": [{"name":"nerd-font","platforms":["linux"]}], "health"`, 1))

	dir := filepath.Join(a.Home, ".local", "share", "fonts", "JetBrainsMonoNerdFont")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, face := range []string{"JetBrainsMonoNerdFont-Regular.ttf", "JetBrainsMonoNerdFont-Bold.ttf"} {
		write(t, filepath.Join(dir, face), "not really a font")
	}

	items, err := a.MenuItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Kind != KindExtra {
			continue
		}
		if item.Status != "2 faces installed" || !item.Done {
			t.Errorf("status=%q done=%v", item.Status, item.Done)
		}
		return
	}
	t.Error("no extra offered")
}
