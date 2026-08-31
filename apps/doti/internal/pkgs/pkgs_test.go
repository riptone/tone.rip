package pkgs

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

const fixture = `{
  "app": "dotfiles",
  "tools": [
    { "cmd": "jq",   "brew": "jq",       "winget": "jqlang.jq" },
    { "cmd": "stow", "brew": "stow" },
    { "cmd": "code", "winget": "Microsoft.VisualStudioCode" }
  ],
  "zsh_plugins": [{ "brew": "zsh-autosuggestions" }],
  "brew_casks": [
    { "brew": "ghostty",       "platforms": ["macos"] },
    { "brew": "cross-platform" }
  ],
  "winget_extras": ["Brave.Brave", "jqlang.jq"]
}`

func load(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBrewfileListsFormulaePluginsAndCasks(t *testing.T) {
	got := Brewfile(load(t))
	for _, want := range []string{
		`brew "jq"`,
		`brew "stow"`,
		`brew "zsh-autosuggestions"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Brewfile missing %s\n%s", want, got)
		}
	}
	// A tool with no brew entry (code is winget-only) must not appear.
	if strings.Contains(got, `brew "code"`) {
		t.Error("a winget-only tool leaked into the Brewfile")
	}
}

// Guarded rather than omitted: one generated file has to be valid on Linux
// too, and `brew bundle` is what decides. Omitting them would work, but it
// would also mean a Linux machine and a macOS machine install from different
// files, which is the drift this generation exists to prevent.
func TestMacOnlyCasksAreGuardedNotDropped(t *testing.T) {
	got := Brewfile(load(t))
	if !strings.Contains(got, `cask "ghostty" if OS.mac?`) {
		t.Errorf("macOS cask should be guarded:\n%s", got)
	}
	if !strings.Contains(got, "cask \"cross-platform\"\n") {
		t.Errorf("an unrestricted cask should not be guarded:\n%s", got)
	}
}

func TestWingetListIsToolsThenExtrasWithoutDuplicates(t *testing.T) {
	out, err := WingetPackages(load(t))
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Schema  string `json:"$schema"`
		Sources []struct {
			Packages []struct {
				PackageIdentifier string
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if parsed.Schema == "" || len(parsed.Sources) != 1 {
		t.Fatalf("unexpected shape: %s", out)
	}

	var ids []string
	for _, p := range parsed.Sources[0].Packages {
		ids = append(ids, p.PackageIdentifier)
	}
	want := []string{"jqlang.jq", "Microsoft.VisualStudioCode", "Brave.Brave"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
}

// `jqlang.jq` is in both tools[] and winget_extras[]. winget import fails on
// a duplicated identifier, and the shell installer deduplicated for exactly
// this reason.
func TestADuplicateExtraIsNotEmittedTwice(t *testing.T) {
	out, err := WingetPackages(load(t))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out, `"jqlang.jq"`); n != 1 {
		t.Fatalf("jqlang.jq appears %d times, want 1:\n%s", n, out)
	}
}

func TestInspectSplitsPresentFromMissing(t *testing.T) {
	installed := map[string]bool{"jq": true, "code": true}
	status := Inspect(load(t), func(cmd string) bool { return installed[cmd] })

	if len(status.Present) != 2 || len(status.Missing) != 1 {
		t.Fatalf("present=%v missing=%v", status.Present, status.Missing)
	}
	if status.Missing[0].Cmd != "stow" {
		t.Fatalf("missing = %v, want stow", status.Missing)
	}
}

// Detection is by command name, not by asking brew. A tool installed some
// other way still answers "can I run this", which is what makes --adopt
// usable on a machine someone has had for years.
func TestInspectDoesNotCareHowSomethingWasInstalled(t *testing.T) {
	status := Inspect(load(t), func(string) bool { return true })
	if len(status.Missing) != 0 {
		t.Fatalf("nothing should be missing, got %v", status.Missing)
	}
}
