package pkgs

import (
	"context"
	"encoding/json"
	"runtime"
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

// fakeRunner answers without touching PATH, /Applications or a package
// manager.
type fakeRunner struct {
	cmds map[string]bool
	apps map[string]bool
	// out is what Output answers, keyed by the whole invocation.
	out map[string]string
}

func (f fakeRunner) Run(context.Context, string, ...string) error { return nil }

func (f fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	return []byte(f.out[name+" "+strings.Join(args, " ")]), nil
}

func (f fakeRunner) Look(name string) bool   { return f.cmds[name] }
func (f fakeRunner) HasApp(name string) bool { return f.apps[name] }

func TestInspectSplitsPresentFromMissing(t *testing.T) {
	status := Inspect(load(t), fakeRunner{cmds: map[string]bool{"jq": true, "code": true}})

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
	status := Inspect(load(t), fakeRunner{cmds: map[string]bool{
		"jq": true, "stow": true, "code": true,
	}})
	if len(status.Missing) != 0 {
		t.Fatalf("nothing should be missing, got %v", status.Missing)
	}
}

// A GUI app puts nothing on PATH, so a tool that names its bundle must be
// found by it. Without this, Ghostty is reported missing on a machine where
// it is running.
func TestInspectFindsAToolByItsApplicationBundle(t *testing.T) {
	m, err := manifest.Parse([]byte(`{
	  "app": "d",
	  "tools": [{ "cmd": "ghostty", "brew": "ghostty", "app": "Ghostty" }]
	}`))
	if err != nil {
		t.Fatal(err)
	}

	missing := Inspect(m, fakeRunner{})
	if len(missing.Missing) != 1 {
		t.Fatalf("with neither, it should be missing: %+v", missing)
	}

	viaApp := Inspect(m, fakeRunner{apps: map[string]bool{"Ghostty": true}})
	if len(viaApp.Present) != 1 {
		t.Fatalf("the bundle should count as present: %+v", viaApp)
	}

	viaPath := Inspect(m, fakeRunner{cmds: map[string]bool{"ghostty": true}})
	if len(viaPath.Present) != 1 {
		t.Fatalf("PATH should still count: %+v", viaPath)
	}
}

// HasApp is macOS-only: on Linux and Windows the command *is* the
// application, and an app-bundle path means nothing.
func TestHasAppIsMacOnlyAndIgnoresAnEmptyName(t *testing.T) {
	runner := ExecRunner{}
	if runner.HasApp("") {
		t.Error("an empty bundle name must never match")
	}
	got := runner.HasApp("DefinitelyNotInstalled")
	if got {
		t.Error("a bundle that is not there must not match")
	}
	if runtime.GOOS != "darwin" && runner.HasApp("Ghostty") {
		t.Error("HasApp should be false off macOS")
	}
}

// Captured output is what keeps the display readable, and what makes a
// failure diagnosable after the fact. Both halves are asserted: the tail ends
// up in the error, and it is capped.
func TestAFailedCommandCarriesItsOutput(t *testing.T) {
	err := ExecRunner{}.Run(context.Background(), "sh", "-c",
		"echo something-went-wrong >&2; exit 3")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "something-went-wrong") {
		t.Fatalf("the error should carry the output: %v", err)
	}
	if !strings.Contains(err.Error(), "sh -c") {
		t.Fatalf("the error should name the invocation: %v", err)
	}
}

func TestASucceedingCommandIsSilent(t *testing.T) {
	var out strings.Builder
	if err := (ExecRunner{Out: &out}).Run(context.Background(), "sh", "-c", "echo hello"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "hello") {
		t.Errorf("streamed output should reach Out, got %q", out.String())
	}
}

// `brew bundle` on a fresh machine emits megabytes. An error message is not a
// log file.
func TestCapturedOutputIsCapped(t *testing.T) {
	err := ExecRunner{}.Run(context.Background(), "sh", "-c",
		"for i in $(seq 1 20000); do echo padding-line-$i; done; exit 1")
	if err == nil {
		t.Fatal("want an error")
	}
	// The tail plus the invocation and indentation; generous, but far below
	// the megabytes the command produced.
	if len(err.Error()) > 3*tailBytes {
		t.Fatalf("error is %d bytes, want the tail only", len(err.Error()))
	}
	// The tail, not the head: the failure is at the end.
	if !strings.Contains(err.Error(), "padding-line-20000") {
		t.Error("the error should carry the end of the output")
	}
	if strings.Contains(err.Error(), "padding-line-1\n") {
		t.Error("the error should not carry the start of the output")
	}
}

func TestALookupFailureNamesTheBinary(t *testing.T) {
	err := ExecRunner{}.Run(context.Background(), "definitely-not-a-command-xyz")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-command-xyz") {
		t.Fatalf("error = %v", err)
	}
}
