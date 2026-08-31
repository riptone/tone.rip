package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T) *Manifest {
	t.Helper()
	m, err := Load(filepath.Join("testdata", "manifest.jsonc"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return m
}

func TestItReadsCommentsAndTheWholeShape(t *testing.T) {
	m := load(t)
	if m.App != "dotfiles" || m.Version != "9.0.0" {
		t.Fatalf("app/version = %q/%q", m.App, m.Version)
	}
	if len(m.StowPackages) != 2 || m.StowPackages[0].Name != "stow" {
		t.Fatalf("stow_packages = %+v", m.StowPackages)
	}
	if len(m.Tools) != 2 || m.Tools[0].Winget != "jqlang.jq" {
		t.Fatalf("tools = %+v", m.Tools)
	}
	if m.Health == nil || m.Health.Links[MacOS]["~/.zshrc"] != "zsh/.zshrc" {
		t.Fatalf("health = %+v", m.Health)
	}
}

// The reason this package takes a JWCC parser instead of stripping comments.
// A scanner that does not track string state truncates this value at the
// `//` in the URL, and the failure is silent - the manifest still parses,
// the command is just wrong.
func TestASlashSlashInsideAStringSurvives(t *testing.T) {
	m := load(t)
	if m.CLI == nil {
		t.Fatal("cli section missing")
	}
	const want = "git clone https://github.com/riptone/dotfiles.git ~/dotfiles && cd ~/dotfiles && doti --all"
	if m.CLI.FreshMachine != want {
		t.Fatalf("fresh_machine was mangled:\n got %q\nwant %q", m.CLI.FreshMachine, want)
	}
}

func TestSecretsParseInBothModes(t *testing.T) {
	m := load(t)
	if len(m.Secrets) != 2 {
		t.Fatalf("want 2 secrets, got %d", len(m.Secrets))
	}

	file := m.Secrets[0]
	if file.Mode != ModeFile || file.Item != "dotfiles/mssql-envs" {
		t.Fatalf("file secret = %+v", file)
	}
	if got := file.FieldOrDefault(); got != "notes" {
		t.Fatalf("field = %q", got)
	}

	tmpl := m.Secrets[1]
	if tmpl.Mode != ModeTemplate || tmpl.Template != "git/.gitconfig.local.tmpl" {
		t.Fatalf("template secret = %+v", tmpl)
	}
	// Unset field falls back to password, which is the useful default for a
	// value pulled out of a login item.
	if got := tmpl.Values["email"].FieldOrDefault(); got != "username" {
		t.Fatalf("email field = %q", got)
	}
	if got := (ValueRef{Item: "x"}).FieldOrDefault(); got != "password" {
		t.Fatalf("default field = %q", got)
	}
}

func TestPlatformFilter(t *testing.T) {
	m := load(t)
	if !m.Secrets[0].WantsPlatform(Windows) {
		t.Error("mssql-envs should apply on windows")
	}
	// An empty platform list means everywhere, not nowhere.
	if !m.Secrets[1].WantsPlatform(Windows) {
		t.Error("a secret with no platforms should apply everywhere")
	}
}

func TestUnknownKeysAreRejected(t *testing.T) {
	// A typo'd key is otherwise a silent no-op, which for a file that decides
	// what gets installed is worse than a hard failure.
	_, err := Parse([]byte(`{"app":"d","stow_packagez":[]}`))
	if err == nil {
		t.Fatal("want an error for an unknown key")
	}
	if !strings.Contains(err.Error(), "stow_packagez") {
		t.Fatalf("error should name the bad key, got: %v", err)
	}
}

func TestValidationRejectsBadSecrets(t *testing.T) {
	cases := map[string]string{
		"unknown mode": `{"app":"d","secrets":[
			{"name":"a","mode":"magic","target":"~/x"}]}`,
		"file without item": `{"app":"d","secrets":[
			{"name":"a","mode":"file","target":"~/x"}]}`,
		"template without values": `{"app":"d","secrets":[
			{"name":"a","mode":"template","template":"t","target":"~/x"}]}`,
		"template value without item": `{"app":"d","secrets":[
			{"name":"a","mode":"template","template":"t","target":"~/x",
			 "values":{"k":{"field":"password"}}}]}`,
		"mode confusion": `{"app":"d","secrets":[
			{"name":"a","mode":"file","item":"i","target":"~/x","template":"t"}]}`,
		"duplicate name": `{"app":"d","secrets":[
			{"name":"a","mode":"file","item":"i","target":"~/x"},
			{"name":"a","mode":"file","item":"j","target":"~/y"}]}`,
		"unknown platform": `{"app":"d","secrets":[
			{"name":"a","mode":"file","item":"i","target":"~/x",
			 "platforms":["plan9"]}]}`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(src)); err == nil {
				t.Fatal("want a validation error")
			}
		})
	}
}

// A relative target resolves against the working directory. Run the installer
// from the repo - which is the documented way to run it - and that writes the
// secret *into the repo*, which is the exact outcome this design prevents.
func TestARelativeSecretTargetIsRefused(t *testing.T) {
	_, err := Parse([]byte(`{"app":"d","secrets":[
		{"name":"a","mode":"file","item":"i","target":"opencode/creds.json"}]}`))
	if err == nil {
		t.Fatal("want an error for a repo-relative target")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error should explain, got: %v", err)
	}
}

// Drift guard: when the real dotfiles checkout is present, it must still
// parse. Skipped rather than failed when it is absent, so CI stays hermetic -
// the clone is gitignored and lives outside this module.
func TestTheRealManifestStillParses(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "dotfiles", "manifest.jsonc")
	if _, err := os.Stat(path); err != nil {
		t.Skip("no dotfiles checkout alongside the monorepo; skipping drift check")
	}
	m, err := Load(path)
	if err != nil {
		t.Fatalf("the real manifest.jsonc no longer parses: %v", err)
	}
	if len(m.StowPackages) == 0 {
		t.Error("real manifest parsed but has no stow packages")
	}
}
