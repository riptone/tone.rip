package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// The paths that had no test because they touch the machine, reached through
// the fakes the rest of this package already has.

// What the selector offers, which is also what Include is matched against - two
// spellings of the same label is a checkbox that silently does nothing.
func TestMenuItemsDescribeTheMachine(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")

	items, err := a.MenuItems()
	if err != nil {
		t.Fatal(err)
	}

	byLabel := map[string]Component{}
	groups := map[string]bool{}
	for _, item := range items {
		byLabel[item.Label] = item
		groups[item.Group] = true
		if !item.Selected {
			t.Errorf("%q starts unticked; re-running a step is how drift gets repaired", item.Label)
		}
	}

	// The tool set, under the label the installer checks for.
	packages, ok := byLabel[packagesLabel]
	if !ok {
		t.Fatalf("no %q component: %v", packagesLabel, byLabel)
	}
	if !strings.Contains(packages.Status, "present") {
		t.Errorf("the packages status reads %q", packages.Status)
	}
	if packages.Group != "Packages" {
		t.Errorf("packages are grouped under %q", packages.Group)
	}

	// Every stow package, with what the machine currently has.
	for _, name := range []string{"zsh", "ghostty"} {
		item, ok := byLabel[name]
		if !ok {
			t.Fatalf("no component for the %q package", name)
		}
		if item.Group != "Configs" {
			t.Errorf("%s is grouped under %q", name, item.Group)
		}
		if item.Status != "not linked" || item.Done {
			t.Errorf("%s reads %q (done=%v) before anything is linked", name, item.Status, item.Done)
		}
	}
	if !groups["Packages"] || !groups["Configs"] {
		t.Errorf("groups = %v", groups)
	}
}

// After linking, the same components say so - which is what makes the selector
// a description of the machine rather than a list of names.
func TestMenuItemsFollowTheMachine(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
	if err := a.Link(); err != nil {
		t.Fatal(err)
	}

	items, err := a.MenuItems()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Group != "Configs" {
			continue
		}
		if item.Status != "linked" || !item.Done {
			t.Errorf("%s reads %q (done=%v) after linking", item.Label, item.Status, item.Done)
		}
	}
}

// Only is deliberately ignored: the selector is how you choose, so narrowing
// the list it offers would be answering the question twice.
func TestMenuItemsIgnoreOnlyAndInclude(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
	a.Only = "zsh"
	a.Include = []string{"zsh"}

	items, err := a.MenuItems()
	if err != nil {
		t.Fatal(err)
	}
	var configs int
	for _, item := range items {
		if item.Group == "Configs" {
			configs++
		}
	}
	if configs != 2 {
		t.Errorf("the selector offered %d config components, want both", configs)
	}
}

// It re-runs the installer rather than reimplementing the download: that script
// already resolves the release, verifies the checksum and asks the binary its
// own version before trusting it.
func TestSelfUpdateRunsTheInstaller(t *testing.T) {
	a, runner, rec := fixture(t)
	runner.cmds["curl"] = true

	if err := a.SelfUpdate(context.Background(), "v0.1.0"); err != nil {
		t.Fatal(err)
	}
	var piped string
	for _, ran := range runner.ran {
		if strings.Contains(ran, "curl") {
			piped = ran
		}
	}
	if piped == "" {
		t.Fatalf("the installer was never fetched: %v", runner.ran)
	}
	for _, want := range []string{installerURL, "--no-install", "bash"} {
		if !strings.Contains(piped, want) {
			t.Errorf("the command is missing %q: %s", want, piped)
		}
	}
	if !rec.Contains("installed: v0.1.0") {
		t.Errorf("it should say what is running now: %v", rec.Texts())
	}
}

func TestSelfUpdateNeedsCurlAndSaysSo(t *testing.T) {
	a, runner, _ := fixture(t)
	runner.cmds["curl"] = false

	err := a.SelfUpdate(context.Background(), "v0.1.0")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "curl") {
		t.Errorf("the error does not name what is missing: %v", err)
	}
}

// -n does not replace the binary it is running.
func TestSelfUpdateDoesNothingOnADryRun(t *testing.T) {
	a, runner, rec := fixture(t)
	runner.cmds["curl"] = true
	a.DryRun = true

	if err := a.SelfUpdate(context.Background(), "v0.1.0"); err != nil {
		t.Fatal(err)
	}
	for _, ran := range runner.ran {
		if strings.Contains(ran, "curl") {
			t.Errorf("a dry run fetched the installer: %s", ran)
		}
	}
	if !rec.Contains("would re-run the installer to fetch the newest release") {
		t.Errorf("it should say what it would do: %v", rec.Texts())
	}
}

// The URL is a constant, so this is belt and braces - but the argument is one
// flag away from being supplied, and a URL that closes the quote would be
// command injection into a shell this code spawns.
func TestShellQuoteClosesNothing(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain", `'plain'`},
		{"it's", `'it'\''s'`},
		{`'; rm -rf /; '`, `''\''; rm -rf /; '\'''`},
	} {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// New resolves the repository to an absolute path, because the linker stores
// whatever path it is given inside the symlinks it creates.
func TestNewResolvesTheRepositoryPath(t *testing.T) {
	instance, err := New("dotfiles", &Recorder{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(instance.Repo) {
		t.Errorf("Repo = %q, want an absolute path", instance.Repo)
	}
	if instance.Home == "" {
		t.Error("no home directory resolved")
	}
	if instance.Platform == "" {
		t.Error("no platform resolved")
	}
}

func TestCurrentPlatformIsOneThisToolKnows(t *testing.T) {
	got, err := CurrentPlatform()
	if err != nil {
		t.Fatal(err)
	}
	switch got {
	case manifest.MacOS, manifest.Linux, manifest.Windows:
	default:
		t.Errorf("CurrentPlatform = %q", got)
	}
}

// The links whose targets live outside $HOME. Windows-only, so the platform is
// forced - the code is the same everywhere and the symlinks are real.
func TestSystemLinksArePlacedAndDisplaceWhatIsThere(t *testing.T) {
	a, _, rec := fixture(t)
	a.Platform = manifest.Windows

	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("USERPROFILE", t.TempDir())

	// The manifest has to declare them, or nothing is installed.
	write(t, filepath.Join(a.Repo, "manifest.jsonc"),
		strings.Replace(fixtureManifest, `"health"`,
			`"system_components": [
			   {"name":"windows-terminal","platforms":["windows"]},
			   {"name":"powershell-profile","platforms":["windows"]}
			 ],
			 "health"`, 1))
	for _, rel := range []string{
		filepath.Join("win", "terminal", "settings.json"),
		filepath.Join("win", "powershell", "profile.ps1"),
	} {
		write(t, filepath.Join(a.Repo, rel), "{}\n")
	}

	// Something already in the way, which must be moved rather than lost.
	existing := filepath.Join(local, "Packages",
		"Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json")
	write(t, existing, "the settings I had before\n")

	if err := a.installSystemLinks(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(existing)
	if err != nil {
		t.Fatalf("nothing was linked: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the target is not a symlink")
	}
	body, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(body)) != "{}" {
		t.Errorf("the link reaches %q", body)
	}
	// Nothing is ever deleted: what was in the way is under the backups, where
	// a person can find it without guessing.
	backup := filepath.Join(a.Home, BackupsDir, "system", "settings.json.displaced")
	saved, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("the file that was in the way is gone: %v", err)
	}
	if strings.TrimSpace(string(saved)) != "the settings I had before" {
		t.Errorf("the backup holds %q", saved)
	}
	if !rec.Contains("backed up " + existing) {
		t.Errorf("the backup was not reported: %v", rec.Texts())
	}
	if !rec.Contains("windows-terminal") {
		t.Errorf("the link was not reported: %v", rec.Texts())
	}
}

// A manifest that does not declare them installs none, on any platform.
func TestUndeclaredSystemLinksAreNotInstalled(t *testing.T) {
	a, _, rec := fixture(t)
	a.Platform = manifest.Windows
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	if err := a.installSystemLinks(); err != nil {
		t.Fatal(err)
	}
	if rec.Contains("windows-terminal") {
		t.Errorf("an undeclared component was installed: %v", rec.Texts())
	}
}

// Best-effort by design: a missing npm or one failed package is a warning, not
// a failed install. None of them is load-bearing for a working shell.
func TestTheMcpServersAreBestEffort(t *testing.T) {
	mcps := `"mcps": ["@modelcontextprotocol/server-a", "@modelcontextprotocol/server-b"],`

	t.Run("all of them install", func(t *testing.T) {
		a, runner, rec := fixture(t, "brew", "jq", "ghostty", "zsh", "npm")
		write(t, filepath.Join(a.Repo, "manifest.jsonc"),
			strings.Replace(fixtureManifest, `"health"`, mcps+`
			 "health"`, 1))

		if err := a.Install(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !rec.Contains("2 MCP servers installed") {
			t.Errorf("not reported: %v", rec.Texts())
		}
		if !runner.didRun("npm install -g @modelcontextprotocol/server-a") {
			t.Errorf("ran: %v", runner.ran)
		}
	})

	t.Run("one of them fails", func(t *testing.T) {
		a, runner, rec := fixture(t, "brew", "jq", "ghostty", "zsh", "npm")
		runner.onRun = func(name string, args []string) error {
			if name == "npm" && slices.Contains(args, "@modelcontextprotocol/server-b") {
				return errors.New("npm ERR! 404 Not Found")
			}
			return nil
		}
		write(t, filepath.Join(a.Repo, "manifest.jsonc"),
			strings.Replace(fixtureManifest, `"health"`, mcps+`
			 "health"`, 1))

		if err := a.Install(context.Background()); err != nil {
			t.Fatalf("one failed MCP server must not fail the install: %v", err)
		}
		if !rec.Contains("1 of 2 MCP servers did not install: @modelcontextprotocol/server-b") {
			t.Errorf("the failure was not named: %v", rec.Texts())
		}
		if rec.Marked(MarkWarn) == 0 {
			t.Error("a failed server should be a warning")
		}
	})

	t.Run("npm is not installed", func(t *testing.T) {
		a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
		write(t, filepath.Join(a.Repo, "manifest.jsonc"),
			strings.Replace(fixtureManifest, `"health"`, mcps+`
			 "health"`, 1))

		if err := a.Install(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !rec.Contains("npm is not installed, so no MCP servers") {
			t.Errorf("it should say why nothing happened: %v", rec.Texts())
		}
	})

	t.Run("a dry run installs nothing", func(t *testing.T) {
		a, runner, rec := fixture(t, "brew", "jq", "ghostty", "zsh", "npm")
		a.DryRun = true
		write(t, filepath.Join(a.Repo, "manifest.jsonc"),
			strings.Replace(fixtureManifest, `"health"`, mcps+`
			 "health"`, 1))

		if err := a.Install(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !rec.Contains("would npm install -g 2 package(s)") {
			t.Errorf("it should say what it would do: %v", rec.Texts())
		}
		for _, ran := range runner.ran {
			if strings.HasPrefix(ran, "npm install") {
				t.Errorf("a dry run installed something: %s", ran)
			}
		}
	})
}

// Windows installs through winget, from a generated file rather than a command
// line - the tool list is long enough to exceed what a shell will take.
func TestWingetImportsAGeneratedFile(t *testing.T) {
	a, runner, rec := fixture(t)
	a.Platform = manifest.Windows

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	var imported string
	for _, ran := range runner.ran {
		if strings.HasPrefix(ran, "winget import") {
			imported = ran
		}
	}
	if imported == "" {
		t.Fatalf("winget was never run: %v", runner.ran)
	}
	for _, want := range []string{"-i", "--accept-package-agreements", "--accept-source-agreements"} {
		if !strings.Contains(imported, want) {
			t.Errorf("the command is missing %q: %s", want, imported)
		}
	}
	if !rec.Contains("installed the missing tools") {
		t.Errorf("not reported: %v", rec.Texts())
	}
}

func TestAFailedWingetImportIsReportedAndReturned(t *testing.T) {
	a, runner, rec := fixture(t)
	a.Platform = manifest.Windows
	runner.onRun = func(name string, _ []string) error {
		if name == "winget" {
			return errors.New("winget exited 1")
		}
		return nil
	}

	if err := a.InstallPackages(context.Background()); err == nil {
		t.Fatal("a failed import should be returned")
	}
	if !rec.Contains("winget import failed") {
		t.Errorf("not reported: %v", rec.Texts())
	}
}

// A re-scan after a sync has to see the manifest the sync pulled.
func TestForgetMakesTheNextReadSeeTheFileOnDisk(t *testing.T) {
	a, _, _ := fixture(t)

	first, err := a.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.StowPackages) != 2 {
		t.Fatalf("the fixture should carry two packages, got %d", len(first.StowPackages))
	}

	// What a `git pull` does.
	write(t, filepath.Join(a.Repo, "manifest.jsonc"),
		strings.Replace(fixtureManifest,
			`{ "name": "ghostty", "platforms": ["macos", "linux"] }`, "", 1))

	// Cached, so the change is invisible - which is right for one run.
	cached, err := a.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(cached.StowPackages) != 2 {
		t.Errorf("the cache was bypassed: %d packages", len(cached.StowPackages))
	}

	a.Forget()
	fresh, err := a.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.StowPackages) != 1 {
		t.Errorf("after Forget the manifest still has %d packages", len(fresh.StowPackages))
	}
}
