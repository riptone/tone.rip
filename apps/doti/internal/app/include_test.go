package app

import (
	"context"
	"errors"
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

	a.Include = []string{"zsh"}
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
	a.Include = []string{"zsh"}
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
	a.Include = []string{"nothing-like-this"}
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

	a.Include = []string{"zsh"}
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
	if err := a.Do(context.Background(), OpCheck, []string{"zsh"}, "v1.0.0"); err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(a.Include) != 1 || a.Include[0] != "zsh" {
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

		items, err := a.MenuItems()
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
				if !strings.Contains(item.Status, "declared") {
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
		a.Include = []string{"zsh"}

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
		a.Include = []string{mcpLabel}

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
		items, err := a.MenuItems()
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
		items, err := a.MenuItems()
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
		a.Include = []string{"powershell-profile"}

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
		items, err := a.MenuItems()
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
