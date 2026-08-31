package health

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// fakeDetector answers without touching PATH or /Applications.
type fakeDetector struct {
	cmds map[string]bool
	apps map[string]bool
}

func (f fakeDetector) Look(name string) bool   { return f.cmds[name] }
func (f fakeDetector) HasApp(name string) bool { return f.apps[name] }

const src = `{
  "app": "dotfiles",
  "tools": [{ "cmd": "jq", "brew": "jq" }, { "cmd": "stow", "brew": "stow" }],
  "health": {
    "extra_tools": { "macos": ["zsh", "jq"] },
    "links": { "macos": {
      "~/.zshrc": "zsh/.zshrc",
      "~/.config/ghostty": "ghostty/.config/ghostty"
    } }
  }
}`

// build lays out a repo and a home, and links whatever the caller asks for.
func build(t *testing.T) (repo, home string) {
	t.Helper()
	root := t.TempDir()
	repo, home = filepath.Join(root, "repo"), filepath.Join(root, "home")
	for _, path := range []string{
		filepath.Join(repo, "zsh"),
		filepath.Join(repo, "ghostty", ".config", "ghostty"),
		home,
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=nvim\n")
	write(t, filepath.Join(repo, "ghostty", ".config", "ghostty", "config"), "theme=dark\n")
	return repo, home
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func opts(t *testing.T, repo, home string, installed ...string) Options {
	t.Helper()
	m, err := manifest.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, name := range installed {
		have[name] = true
	}
	return Options{
		Manifest: m, Platform: manifest.MacOS, Repo: repo, Home: home,
		Detect: fakeDetector{cmds: have},
	}
}

func find(t *testing.T, r Report, name string) Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no finding for %q in %+v", name, r.Findings)
	return Finding{}
}

func TestAToolNotOnPathIsReported(t *testing.T) {
	repo, home := build(t)
	r := Check(opts(t, repo, home, "jq"))
	if !find(t, r, "jq").OK {
		t.Error("jq is installed and should pass")
	}
	missing := find(t, r, "stow")
	if missing.OK || missing.Detail != "not on PATH" {
		t.Fatalf("stow finding = %+v", missing)
	}
}

// jq is in both tools[] and health.extra_tools. Checking it twice would make
// the totals lie.
func TestAToolInBothListsIsCheckedOnce(t *testing.T) {
	repo, home := build(t)
	r := Check(opts(t, repo, home, "jq", "zsh", "stow"))
	var seen int
	for _, f := range r.Findings {
		if f.Name == "jq" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("jq checked %d times, want 1", seen)
	}
}

func TestAMissingLinkIsReported(t *testing.T) {
	repo, home := build(t)
	r := Check(opts(t, repo, home))
	if f := find(t, r, "~/.zshrc"); f.OK || f.Detail != "missing" {
		t.Fatalf("finding = %+v", f)
	}
}

func TestACorrectLinkPasses(t *testing.T) {
	repo, home := build(t)
	if err := os.Symlink(filepath.Join(repo, "zsh", ".zshrc"),
		filepath.Join(home, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	if f := find(t, Check(opts(t, repo, home)), "~/.zshrc"); !f.OK {
		t.Fatalf("finding = %+v", f)
	}
}

// The case a leaf-only check gets wrong. ~/.config/ghostty is not itself a
// symlink here - ~/.config is, because the whole subtree was folded. Reading
// the link at the leaf would call this broken when it is exactly right.
func TestALinkReachedThroughAFoldedParentPasses(t *testing.T) {
	repo, home := build(t)
	if err := os.Symlink(filepath.Join(repo, "ghostty", ".config"),
		filepath.Join(home, ".config")); err != nil {
		t.Fatal(err)
	}
	if f := find(t, Check(opts(t, repo, home)), "~/.config/ghostty"); !f.OK {
		t.Fatalf("a folded link should pass, got %+v", f)
	}
}

func TestALinkPointingSomewhereElseIsReported(t *testing.T) {
	repo, home := build(t)
	elsewhere := filepath.Join(t.TempDir(), "other")
	write(t, elsewhere, "someone else's\n")
	if err := os.Symlink(elsewhere, filepath.Join(home, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	f := find(t, Check(opts(t, repo, home)), "~/.zshrc")
	if f.OK {
		t.Fatal("a link into another checkout should not pass")
	}
	if f.Detail == "" {
		t.Error("the finding should say where it points")
	}
}

// A copy resolves to the same *content* but is drift: the repo moves on and
// the machine does not follow, silently.
func TestACopyIsNotAcceptedAsALink(t *testing.T) {
	repo, home := build(t)
	write(t, filepath.Join(home, ".zshrc"), "export EDITOR=nvim\n")
	f := find(t, Check(opts(t, repo, home)), "~/.zshrc")
	if f.OK {
		t.Fatal("a real copy should not pass as a link")
	}
	if f.Detail != "is a copy, not a link" {
		t.Fatalf("detail = %q", f.Detail)
	}
}

func TestReportSummarises(t *testing.T) {
	repo, home := build(t)
	r := Check(opts(t, repo, home, "jq", "stow", "zsh"))
	passed, total := r.Counts()
	if total != 5 {
		t.Fatalf("checked %d things, want 5 (2 tools + 1 extra + 2 links)", total)
	}
	if passed != 3 {
		t.Fatalf("passed = %d, want 3 tools", passed)
	}
	if r.OK() {
		t.Error("two links are missing; OK() should be false")
	}
	if len(r.Missing()) != 2 {
		t.Fatalf("Missing() = %+v", r.Missing())
	}
}

func TestFindingsAreOrderedDeterministically(t *testing.T) {
	repo, home := build(t)
	first := Check(opts(t, repo, home))
	for range 5 {
		next := Check(opts(t, repo, home))
		for i := range first.Findings {
			if first.Findings[i].Name != next.Findings[i].Name {
				t.Fatal("link order is not stable across runs")
			}
		}
	}
}
