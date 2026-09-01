package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// A manifest that routes one tool through bun.
//
// The real case: winget's opencode sat on 1.18.21 while every other channel
// shipped 1.18.25, so the entry names the brew tap and `opencode-ai` and no
// winget id at all. bun is declared before it, which the manifest's own
// ordering rule requires and this fixture has to honour or Manifest() refuses
// the file.
const bunManifest = `{
  "app": "dotfiles", "version": "9.0.0",
  "stow_packages": [{ "name": "zsh", "platforms": ["macos", "linux", "windows"] }],
  "stow_ignore": [],
  "tools": [
    { "cmd": "bun",      "brew": "bun", "winget": "Oven-sh.Bun" },
    { "cmd": "fd",       "brew": "fd",  "winget": "sharkdp.fd" },
    { "cmd": "opencode", "brew": "anomalyco/tap/opencode", "bun": "opencode-ai" }
  ],
  "health": { "extra_tools": { "macos": ["zsh", "brew", "stow"] } }
}`

// bunFixture is a Windows machine, with bun's global directory under the
// fixture home.
//
// BUN_INSTALL is set explicitly rather than left to the default, because it is
// set for real in this repository's own shell: a test that read it would be
// asking about the developer's machine.
func bunFixture(t *testing.T, installed ...string) (*App, *fakeRunner, *Recorder) {
	t.Helper()
	a, runner, rec := fixture(t, installed...)
	a.Platform = manifest.Windows
	write(t, filepath.Join(a.Repo, "manifest.jsonc"), bunManifest)
	t.Setenv("BUN_INSTALL", filepath.Join(a.Home, ".bun"))
	runner.files = map[string]string{}
	return a, runner, rec
}

// bunHas puts a package in bun's global directory, which is the only place a
// bun install leaves a trace `winget export` can never see.
func bunHas(t *testing.T, a *App, packages ...string) {
	t.Helper()
	for _, pkg := range packages {
		dir := filepath.Join(a.Home, ".bun", "install", "global", "node_modules",
			filepath.FromSlash(pkg))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// The install half: bun gets the package, and the winget import never hears
// about it - there is no identifier to import.
func TestWindowsInstallsABunToolWithBunAndLeavesItOutOfTheImport(t *testing.T) {
	a, runner, _ := bunFixture(t, "winget", "bun", "fd")
	runner.ownsWinget("Oven-sh.Bun", "sharkdp.fd")
	runner.onRun = func(_ string, args []string) error {
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

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("bun install -g opencode-ai") {
		t.Errorf("bun was never asked to install it: %v", runner.ran)
	}
	if body := runner.files["import"]; strings.Contains(body, "opencode") {
		t.Errorf("a bun package reached the winget import:\n%s", body)
	}
	// The winget tools still go through winget.
	if body := runner.files["import"]; !strings.Contains(body, "sharkdp.fd") {
		t.Errorf("the winget tools went missing:\n%s", body)
	}
}

// Install is not upgrade. `bun install -g` on a package that is already there
// re-resolves it to the latest published version, which is exactly what
// `brew bundle --no-upgrade` exists to not do on the other platform.
func TestAnAlreadyInstalledBunToolIsLeftAlone(t *testing.T) {
	a, runner, _ := bunFixture(t, "winget", "bun", "fd", "opencode")
	runner.ownsWinget("Oven-sh.Bun", "sharkdp.fd")

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, ran := range runner.ran {
		if strings.HasPrefix(ran, "bun install") {
			t.Errorf("an install upgraded a present tool: %s", ran)
		}
	}
}

// A selection that ticked the bun tool and not bun itself. The manifest's
// ordering rule makes this unreachable for a whole run, so it has to be
// reported rather than assumed away.
func TestABunToolWithoutBunIsAnError(t *testing.T) {
	a, runner, _ := bunFixture(t, "winget", "fd")
	runner.ownsWinget("Oven-sh.Bun", "sharkdp.fd")

	err := a.InstallPackages(context.Background())
	if err == nil {
		t.Fatal("a tool the manifest asked for and did not get reported success")
	}
	if !strings.Contains(err.Error(), "bun is not installed") {
		t.Errorf("the error does not say what is wrong: %v", err)
	}
}

// On macOS the same entry uses the brew tap. The bun field is the fallback for
// a platform with no name of its own, not a replacement for one.
func TestOnMacOSTheSameToolComesFromBrew(t *testing.T) {
	a, runner, _ := bunFixture(t, "brew", "bun", "fd")
	a.Platform = manifest.MacOS
	runner.onRun = func(_ string, args []string) error {
		for _, arg := range args {
			if path, found := strings.CutPrefix(arg, "--file="); found {
				body, err := readFile(path)
				if err != nil {
					t.Errorf("reading the Brewfile: %v", err)
					return nil
				}
				runner.files["brewfile"] = body
			}
		}
		return nil
	}

	if err := a.InstallPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if body := runner.files["brewfile"]; !strings.Contains(body, `brew "anomalyco/tap/opencode"`) {
		t.Errorf("the tap formula is not in the Brewfile:\n%s", body)
	}
	for _, ran := range runner.ran {
		if strings.HasPrefix(ran, "bun install") {
			t.Errorf("bun was used on a platform with a brew name: %s", ran)
		}
	}
}

// The removal half. `winget export` knows nothing about a bun global, so
// without asking bun's own directory this tool was invisible to the selector
// for as long as it existed.
func TestABunToolIsOfferedForRemovalWhenBunHasIt(t *testing.T) {
	a, runner, _ := bunFixture(t, "winget", "bun", "opencode")
	runner.ownsWinget("Oven-sh.Bun")
	bunHas(t, a, "opencode-ai")

	items, err := a.Removable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := labels(items); !slicesContains(got, "opencode") {
		t.Errorf("bun has it and it is not offered: %v", got)
	}
}

// And not when bun does not have it - opencode on PATH from the install script
// is not bun's, and not doti's to delete.
func TestABunToolBunDoesNotOwnIsNotOffered(t *testing.T) {
	a, runner, _ := bunFixture(t, "winget", "bun", "opencode")
	runner.ownsWinget("Oven-sh.Bun")

	items, err := a.Removable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := labels(items); slicesContains(got, "opencode") {
		t.Errorf("a tool bun did not install was offered: %v", got)
	}
}

func TestRemovingABunToolGoesThroughBun(t *testing.T) {
	a, runner, rec := bunFixture(t, "winget", "bun", "opencode")
	runner.ownsWinget("Oven-sh.Bun")
	bunHas(t, a, "opencode-ai")
	a.Include = Refs([]string{"opencode"})

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("bun remove -g opencode-ai") {
		t.Fatalf("ran: %v", runner.ran)
	}
	if !rec.Contains("removed opencode") {
		t.Errorf("%v", rec.Texts())
	}
	// Not through winget, which has no id for it and would have failed.
	for _, ran := range runner.ran {
		if strings.HasPrefix(ran, "winget uninstall") {
			t.Errorf("winget was asked to remove a bun package: %s", ran)
		}
	}
}

// Naming it explicitly says which manager would have had to install it. "not
// by winget" would be true of a thing that was never going to install it.
func TestNamingABunToolBunDoesNotOwnSaysBun(t *testing.T) {
	a, runner, rec := bunFixture(t, "winget", "bun", "opencode")
	runner.ownsWinget("Oven-sh.Bun")
	a.Include = Refs([]string{"opencode"})

	if err := a.RemovePackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains("opencode is installed, but not by bun - left alone") {
		t.Errorf("%v", rec.Texts())
	}
}

// `winget upgrade --all` does not reach a bun global, so update has to.
func TestUpdateUpgradesTheBunTools(t *testing.T) {
	a, runner, _ := bunFixture(t, "winget", "bun", "opencode")

	if err := a.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("bun install -g opencode-ai") {
		t.Fatalf("ran: %v", runner.ran)
	}
}
