package stow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildPackage lays out a stow package and returns (packageDir, home).
func buildPackage(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	pkg := filepath.Join(root, "pkg")
	home := filepath.Join(root, "home")
	for _, dir := range []string{pkg, home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for rel, body := range files {
		path := filepath.Join(pkg, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return pkg, home
}

func planFor(t *testing.T, pkg, home string, ignore *Ignorer) map[string]Op {
	t.Helper()
	ops, err := Plan(pkg, home, ignore)
	if err != nil {
		t.Fatal(err)
	}
	byTarget := make(map[string]Op, len(ops))
	for _, op := range ops {
		byTarget[filepath.Base(op.Target)] = op
	}
	return byTarget
}

// The fold: an absent target gets one symlink for the whole subtree rather
// than a symlink per file inside it.
func TestAnAbsentDirectoryIsLinkedWhole(t *testing.T) {
	pkg, home := buildPackage(t, map[string]string{
		".config/ghostty/config": "theme = dark\n",
	})
	ops, err := Plan(pkg, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("want one folded op, got %d: %+v", len(ops), ops)
	}
	if ops[0].Kind != Link || filepath.Base(ops[0].Target) != ".config" {
		t.Fatalf("want a fold at .config, got %+v", ops[0])
	}
}

// The counterpart: ~/.config already exists and is shared between packages,
// so it must stay a real directory and the plan descends into it.
func TestAnExistingDirectoryIsDescendedNotReplaced(t *testing.T) {
	pkg, home := buildPackage(t, map[string]string{
		".config/ghostty/config": "theme = dark\n",
	})
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	ops := planFor(t, pkg, home, nil)
	op, ok := ops["ghostty"]
	if !ok {
		t.Fatalf("expected to descend into .config, got %+v", ops)
	}
	if op.Kind != Link {
		t.Fatalf("want ghostty linked, got %+v", op)
	}
	if _, exists := ops[".config"]; exists {
		t.Error(".config should have been descended into, not linked over")
	}
}

func TestApplyCreatesWorkingSymlinks(t *testing.T) {
	pkg, home := buildPackage(t, map[string]string{".zshrc": "export EDITOR=nvim\n"})
	ops, err := Plan(pkg, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(ops, filepath.Join(home, ".backups"), home, false); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "export EDITOR=nvim\n" {
		t.Fatalf("linked content = %q", body)
	}
	info, err := os.Lstat(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("target should be a symlink, not a copy")
	}
}

// The contract the shell installer advertised: whatever was there is
// recoverable. A tool that silently discards a config someone has had for
// years is not one to trust on a new machine.
func TestAnExistingFileIsBackedUpNotDestroyed(t *testing.T) {
	pkg, home := buildPackage(t, map[string]string{".zshrc": "new\n"})
	existing := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(existing, []byte("precious\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ops := planFor(t, pkg, home, nil)
	if ops[".zshrc"].Kind != Relink {
		t.Fatalf("want relink, got %+v", ops[".zshrc"])
	}

	backups := filepath.Join(home, ".backups")
	plan, _ := Plan(pkg, home, nil)
	if err := Apply(plan, backups, home, false); err != nil {
		t.Fatal(err)
	}

	linked, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(linked) != "new\n" {
		t.Fatalf("target = %q, want the package's copy", linked)
	}

	var found string
	_ = filepath.Walk(backups, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Base(p) == ".zshrc" {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Fatal("the displaced file was not backed up")
	}
	saved, err := os.ReadFile(found)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != "precious\n" {
		t.Fatalf("backup = %q, want the original", saved)
	}
}

func TestAnAlreadyCorrectLinkIsSkipped(t *testing.T) {
	pkg, home := buildPackage(t, map[string]string{".zshrc": "x\n"})
	plan, _ := Plan(pkg, home, nil)
	if err := Apply(plan, filepath.Join(home, ".b"), home, false); err != nil {
		t.Fatal(err)
	}
	again := planFor(t, pkg, home, nil)
	if again[".zshrc"].Kind != Skip {
		t.Fatalf("re-planning should skip, got %+v", again[".zshrc"])
	}
}

// A link at one of our paths but pointing into a different checkout belongs
// to that checkout. It is displaced, but only via the backup path.
func TestAStaleLinkFromAnotherCheckoutIsRelinked(t *testing.T) {
	pkg, home := buildPackage(t, map[string]string{".zshrc": "ours\n"})
	elsewhere := filepath.Join(t.TempDir(), "other-checkout-zshrc")
	if err := os.WriteFile(elsewhere, []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(home, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	op := planFor(t, pkg, home, nil)[".zshrc"]
	if op.Kind != Relink {
		t.Fatalf("want relink, got %+v", op)
	}
	if op.Reason == "" {
		t.Error("a relink should say why")
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	pkg, home := buildPackage(t, map[string]string{".zshrc": "x\n"})
	plan, _ := Plan(pkg, home, nil)
	if err := Apply(plan, filepath.Join(home, ".b"), home, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
		t.Fatal("dry run created a link")
	}
}

func TestIgnorePatternsAreAnchoredToTheWholeBasename(t *testing.T) {
	ignore, err := NewIgnorer([]string{`\.DS_Store`, `node_modules`, `package(-lock)?\.json`})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".DS_Store", "node_modules", "package.json", "package-lock.json"} {
		if !ignore.Match(name) {
			t.Errorf("%q should be ignored", name)
		}
	}
	// Unanchored, "node_modules" would swallow this too - and it is a real
	// config directory name, not build output.
	for _, name := range []string{"my_node_modules_notes", ".zshrc", "packages.json"} {
		if ignore.Match(name) {
			t.Errorf("%q should not be ignored", name)
		}
	}
	if (*Ignorer)(nil).Match("anything") {
		t.Error("a nil ignorer should ignore nothing")
	}
}

func TestABadIgnorePatternIsReported(t *testing.T) {
	if _, err := NewIgnorer([]string{"("}); err == nil {
		t.Fatal("want an error for an uncompilable pattern")
	}
}

func TestIgnoredEntriesAreNeverLinked(t *testing.T) {
	pkg, home := buildPackage(t, map[string]string{
		".zshrc":    "keep\n",
		".DS_Store": "junk\n",
	})
	ignore, err := NewIgnorer([]string{`\.DS_Store`})
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := Plan(pkg, home, ignore)
	if err := Apply(plan, filepath.Join(home, ".b"), home, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".DS_Store")); !os.IsNotExist(err) {
		t.Fatal("an ignored entry was linked into $HOME")
	}
	if _, err := os.Lstat(filepath.Join(home, ".zshrc")); err != nil {
		t.Fatal("a non-ignored entry was not linked")
	}
}

func TestUnlinkRemovesOnlyWhatThisPackageOwns(t *testing.T) {
	pkg, home := buildPackage(t, map[string]string{".zshrc": "x\n"})
	plan, _ := Plan(pkg, home, nil)
	if err := Apply(plan, filepath.Join(home, ".b"), home, false); err != nil {
		t.Fatal(err)
	}

	// A file this package does not own, sitting where it would never link.
	foreign := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(foreign, []byte("not ours\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := Unlink(pkg, home, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 {
		t.Fatalf("want one removal, got %v", removed)
	}
	if _, err := os.Lstat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
		t.Error("our link should be gone")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Error("a file we do not own was removed")
	}
}

func TestPlanRejectsAMissingPackage(t *testing.T) {
	if _, err := Plan(filepath.Join(t.TempDir(), "absent"), t.TempDir(), nil); err == nil {
		t.Fatal("want an error for a missing package directory")
	}
}

func TestCountSummarisesAPlan(t *testing.T) {
	counts := Count([]Op{{Kind: Link}, {Kind: Link}, {Kind: Skip}})
	if counts[Link] != 2 || counts[Skip] != 1 {
		t.Fatalf("counts = %v", counts)
	}
}

// buildTwo lays out two packages that both want ~/.config, plus an empty
// $HOME - the exact shape that made the first implementation destructive.
func buildTwo(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(pkg, rel, body string) string {
		dir := filepath.Join(root, pkg)
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	ghostty := write("ghostty", ".config/ghostty/config", "theme = dark\n")
	starship := write("starship", ".config/starship.toml", "add_newline = false\n")
	return ghostty, starship, home
}

func linkPkg(t *testing.T, pkg, home string) {
	t.Helper()
	ops, err := Plan(pkg, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(ops, filepath.Join(home, ".backups"), home, false); err != nil {
		t.Fatal(err)
	}
}

// The regression this package's Unfold exists for.
//
// On an empty $HOME the first package folds ~/.config into a single symlink.
// Without unfolding, the second package sees a symlink pointing somewhere
// else, backs it up and replaces it - so the first package's configs vanish
// and the last one linked wins. Verified against the real dotfiles repo:
// ghostty, opencode and ripgrep all disappeared behind starship.
func TestTwoPackagesCanShareAFoldedDirectory(t *testing.T) {
	ghostty, starship, home := buildTwo(t)
	linkPkg(t, ghostty, home)
	linkPkg(t, starship, home)

	for _, rel := range []string{
		filepath.Join(".config", "ghostty", "config"),
		filepath.Join(".config", "starship.toml"),
	} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Errorf("%s went missing: %v", rel, err)
		}
	}

	info, err := os.Lstat(filepath.Join(home, ".config"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("~/.config should have been unfolded into a real directory")
	}
}

// Unfolding must not look like data loss: everything it touches is a symlink
// this tool created, so a fresh install has nothing to back up.
func TestSharingAFoldedDirectoryBacksUpNothing(t *testing.T) {
	ghostty, starship, home := buildTwo(t)
	linkPkg(t, ghostty, home)
	linkPkg(t, starship, home)

	entries, err := os.ReadDir(filepath.Join(home, ".backups"))
	if err == nil && len(entries) > 0 {
		t.Fatalf("a fresh install backed up %d path(s); unfolding should not displace anything", len(entries))
	}
}

func TestTheUnfoldIsPlannedBeforeItsChildren(t *testing.T) {
	ghostty, starship, home := buildTwo(t)
	linkPkg(t, ghostty, home)

	ops, err := Plan(starship, home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) < 2 {
		t.Fatalf("want an unfold plus a child link, got %+v", ops)
	}
	if ops[0].Kind != Unfold {
		t.Fatalf("first op should be the unfold, got %+v", ops[0])
	}
	// Apply walks the plan in order, so a child linked before its parent
	// directory exists would fail.
	if ops[1].Kind != Link {
		t.Fatalf("second op should link the child, got %+v", ops[1])
	}
}

func TestRelinkingAfterAnUnfoldIsStillIdempotent(t *testing.T) {
	ghostty, starship, home := buildTwo(t)
	linkPkg(t, ghostty, home)
	linkPkg(t, starship, home)

	for _, pkg := range []string{ghostty, starship} {
		ops, err := Plan(pkg, home, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range ops {
			if op.Kind != Skip {
				t.Errorf("re-planning %s should be all skips, got %+v",
					filepath.Base(pkg), op)
			}
		}
	}
}

func TestBackupsAreNewestFirst(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"2026-01-01T00-00-00Z", "2026-06-01T00-00-00Z", "2025-01-01T00-00-00Z"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A stray file must not be offered as a backup to restore from.
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	dirs, err := Backups(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 3 {
		t.Fatalf("got %d backups, want 3: %v", len(dirs), dirs)
	}
	if filepath.Base(dirs[0]) != "2026-06-01T00-00-00Z" {
		t.Fatalf("newest = %s", filepath.Base(dirs[0]))
	}
}

func TestBackupsIsEmptyWhenNothingWasEverBackedUp(t *testing.T) {
	dirs, err := Backups(filepath.Join(t.TempDir(), "never-created"))
	if err != nil {
		t.Fatalf("a missing backup root is not an error: %v", err)
	}
	if len(dirs) != 0 {
		t.Fatalf("got %v", dirs)
	}
}

// The round trip the documented `--uninstall --restore` promises.
func TestRestorePutsTheOriginalBack(t *testing.T) {
	pkg, home := buildPackage(t, map[string]string{".zshrc": "ours\n"})
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("precious\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	backups := filepath.Join(home, ".dotfiles-backups", "2026-06-01T00-00-00Z")
	plan, _ := Plan(pkg, home, nil)
	if err := Apply(plan, backups, home, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Unlink(pkg, home, nil, false); err != nil {
		t.Fatal(err)
	}

	restored, err := Restore(backups, home, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 {
		t.Fatalf("restored = %v", restored)
	}
	body, err := os.ReadFile(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "precious\n" {
		t.Fatalf("restored content = %q, want the original", body)
	}
	// Moved, not copied: a leftover would be restored again next time and
	// silently overwrite whatever the user had done since.
	if _, err := os.Stat(filepath.Join(backups, ".zshrc")); !os.IsNotExist(err) {
		t.Error("the backup copy should be gone after a restore")
	}
}

// Restore has to displace the symlink that replaced the original, not write
// through it into the repo.
func TestRestoreReplacesTheSymlinkRatherThanWritingThroughIt(t *testing.T) {
	pkg, home := buildPackage(t, map[string]string{".zshrc": "ours\n"})
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("precious\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(home, ".b", "run")
	plan, _ := Plan(pkg, home, nil)
	if err := Apply(plan, backups, home, false); err != nil {
		t.Fatal(err)
	}

	if _, err := Restore(backups, home, false); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(filepath.Join(pkg, ".zshrc")); string(body) != "ours\n" {
		t.Fatalf("the repo copy was overwritten: %q", body)
	}
	info, err := os.Lstat(filepath.Join(home, ".zshrc"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("the target should be a real file again, not a link")
	}
}

func TestRestoreDryRunChangesNothing(t *testing.T) {
	pkg, home := buildPackage(t, map[string]string{".zshrc": "ours\n"})
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("precious\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(home, ".b", "run")
	plan, _ := Plan(pkg, home, nil)
	if err := Apply(plan, backups, home, false); err != nil {
		t.Fatal(err)
	}

	restored, err := Restore(backups, home, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 {
		t.Fatalf("a dry run should still report what it would do, got %v", restored)
	}
	if _, err := os.Stat(filepath.Join(backups, ".zshrc")); err != nil {
		t.Error("a dry run moved the backup")
	}
}

// os.Symlink stores the source verbatim, and a relative one is resolved
// against the *link's* directory - $HOME - not the working directory it was
// computed in. `doti link --repo dotfiles` therefore produced
// ~/.zshrc -> dotfiles/zsh/.zshrc: a dangling link, reported as a success.
func TestPlanRefusesARelativePackagePath(t *testing.T) {
	_, err := Plan(filepath.Join("relative", "pkg"), t.TempDir(), nil)
	if err == nil {
		t.Fatal("want an error for a relative package path")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("the error should explain why, got: %v", err)
	}
}
