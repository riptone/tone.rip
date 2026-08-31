// Package stow links a package's tree into $HOME.
//
// It reimplements the subset of GNU Stow this project uses, for one reason:
// stow does not run on Windows, so scripts/Main.ps1 already reimplemented it
// there. Two implementations of one algorithm, in two languages, is the thing
// this binary exists to delete - and doing it here also drops `stow` from the
// package list entirely.
//
// The layout convention is unchanged. A package directory mirrors $HOME, so
// `zsh/.zshrc` becomes `~/.zshrc` and `ghostty/.config/ghostty/config`
// becomes `~/.config/ghostty/config`.
//
// **Folding** is preserved too, because it is what keeps $HOME tidy: if
// `~/.config/ghostty` does not exist, the directory itself is linked rather
// than each file inside it, so one symlink covers the package instead of one
// per file. Descent only happens where a real directory already exists and
// has to be shared - `~/.config` holds several packages' subdirectories, so
// it stays a real directory.
package stow

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Kind is what an operation does to one path.
type Kind string

const (
	// Link creates a symlink; nothing is in the way.
	Link Kind = "link"
	// Relink backs up whatever is at the target, then links. Covers a real
	// file, a real directory where the package has a file, and a stale
	// symlink pointing at a different checkout.
	Relink Kind = "relink"
	// Skip means the correct symlink is already there.
	Skip Kind = "skip"
	// Ignore means a stow_ignore pattern matched.
	Ignore Kind = "ignore"
	// Unfold turns a folded directory symlink back into a real directory
	// holding links to the original package's children, so a second package
	// can share it. Source is the directory the link used to point at.
	//
	// Without this, folding is actively destructive: on an empty $HOME the
	// first package to want ~/.config links the whole directory, and every
	// package after it sees a symlink pointing somewhere else, backs it up
	// and replaces it. The last one wins and the rest silently vanish.
	Unfold Kind = "unfold"
)

// Op is one planned change.
type Op struct {
	Kind   Kind
	Target string // absolute path under $HOME
	Source string // absolute path inside the repo
	// Reason explains a Relink or an Ignore, for the human reading a plan.
	Reason string
}

// Ignorer decides which basenames never get linked.
type Ignorer struct {
	patterns []*regexp.Regexp
}

// NewIgnorer compiles the manifest's stow_ignore list.
//
// The patterns are basename regexes and are anchored here rather than in the
// manifest, matching what the shell installer passed to `stow --ignore`. An
// unanchored `node_modules` would otherwise also match `my_node_modules_dir`.
func NewIgnorer(patterns []string) (*Ignorer, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile("^(?:" + p + ")$")
		if err != nil {
			return nil, fmt.Errorf("stow_ignore pattern %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}
	return &Ignorer{patterns: compiled}, nil
}

// Match reports whether a basename should be skipped.
func (i *Ignorer) Match(name string) bool {
	if i == nil {
		return false
	}
	for _, re := range i.patterns {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// Plan works out what linking one package would do, without changing
// anything. Splitting this from Apply is what makes --dry-run, --check and
// the conflict report one code path instead of three.
func Plan(packageDir, home string, ignore *Ignorer) ([]Op, error) {
	// Absolute, or the symlinks below dangle. os.Symlink stores the source
	// verbatim, and a relative one is resolved against the *link's* own
	// directory - $HOME - not the working directory it was computed in. So
	// `--repo dotfiles` produced ~/.zshrc -> dotfiles/zsh/.zshrc, which
	// points at nothing and reports success.
	if !filepath.IsAbs(packageDir) {
		return nil, fmt.Errorf(
			"stow package path %q must be absolute (a relative link would "+
				"resolve against $HOME and dangle)", packageDir)
	}
	info, err := os.Stat(packageDir)
	if err != nil {
		return nil, fmt.Errorf("stow package %s: %w", packageDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("stow package %s is not a directory", packageDir)
	}
	var ops []Op
	if err := plan(packageDir, packageDir, home, ignore, &ops, ""); err != nil {
		return nil, err
	}
	// Deterministic order: a plan is shown to a human and compared in tests,
	// and directory iteration order is not guaranteed across filesystems.
	sort.Slice(ops, func(a, b int) bool { return ops[a].Target < ops[b].Target })
	return ops, nil
}

// plan walks one directory of the package.
//
// unfoldedFrom is set when this directory does not exist on disk yet because
// an ancestor Unfold will create it. In that case the "existing" state of a
// child is not what is at the target now - it is the link the unfold will
// put there, derived from the directory the fold used to point at.
func plan(root, dir, home string, ignore *Ignorer, ops *[]Op, unfoldedFrom string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", dir, err)
	}

	for _, entry := range entries {
		source := filepath.Join(dir, entry.Name())
		rel, err := filepath.Rel(root, source)
		if err != nil {
			return err
		}
		target := filepath.Join(home, rel)

		if ignore.Match(entry.Name()) {
			*ops = append(*ops, Op{
				Kind: Ignore, Target: target, Source: source,
				Reason: "matches stow_ignore",
			})
			continue
		}

		existing, linkDest, err := inspect(target, unfoldedFrom, entry.Name())
		if err != nil {
			return err
		}

		if existing == nil {
			// Nothing in the way: link here and do not descend. This is the
			// fold - one symlink for the whole subtree.
			*ops = append(*ops, Op{Kind: Link, Target: target, Source: source})
			continue
		}

		if linkDest != "" {
			if linkDest == source || sameFile(linkDest, source) {
				*ops = append(*ops, Op{Kind: Skip, Target: target, Source: source})
				continue
			}
			// Two packages want the same directory. Unfold it rather than
			// letting this one replace the other's link.
			if entry.IsDir() && isDir(linkDest) {
				*ops = append(*ops, Op{
					Kind: Unfold, Target: target, Source: linkDest,
					Reason: "shared with " + linkDest,
				})
				if err := plan(root, source, home, ignore, ops, linkDest); err != nil {
					return err
				}
				continue
			}
			*ops = append(*ops, Op{
				Kind: Relink, Target: target, Source: source,
				Reason: "symlink points at " + linkDest,
			})
			continue
		}

		// A real directory on both sides has to be shared - ~/.config holds
		// subdirectories from several packages, so it cannot become a link
		// to any one of them.
		if existing.IsDir() && entry.IsDir() {
			if err := plan(root, source, home, ignore, ops, ""); err != nil {
				return err
			}
			continue
		}

		*ops = append(*ops, Op{
			Kind: Relink, Target: target, Source: source,
			Reason: describe(existing),
		})
	}
	return nil
}

// inspect reports what is (or will be) at target. A non-empty linkDest means
// a symlink. unfoldedFrom simulates the not-yet-created contents of a
// directory an earlier Unfold op will materialise.
func inspect(target, unfoldedFrom, name string) (fs.FileInfo, string, error) {
	if unfoldedFrom != "" {
		virtual := filepath.Join(unfoldedFrom, name)
		info, err := os.Lstat(virtual)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, "", nil
			}
			return nil, "", fmt.Errorf("inspecting %s: %w", virtual, err)
		}
		// The unfold will place a symlink to this path here.
		return info, virtual, nil
	}

	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("inspecting %s: %w", target, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return info, "", nil
	}
	dest, err := os.Readlink(target)
	if err != nil {
		return nil, "", fmt.Errorf("reading link %s: %w", target, err)
	}
	return info, resolveLink(target, dest), nil
}

// resolveLink makes a symlink's destination absolute, read the way the kernel
// reads it: relative to the directory holding the link.
//
// Readlink returns the link's literal text, and GNU Stow writes *relative*
// links - `../dotfiles/ghostty/.config/ghostty`. Two things go wrong if that
// text is used as-is. It never equals the absolute source, so a link that is
// already correct is planned as a Relink and its backup churns on every run.
// Worse, it reaches isDir as a relative path, which os.Stat resolves against
// this process's working directory - making the fold-or-unfold decision
// depend on where doti happened to be started from.
func resolveLink(target, dest string) string {
	if filepath.IsAbs(dest) {
		return filepath.Clean(dest)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(target), dest))
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// sameFile is the second chance for the equality above: a repository reached
// through a symlinked ancestor (/tmp -> /private/tmp on macOS is the one
// everybody meets) resolves to a different string for the same file.
func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

func describe(info fs.FileInfo) string {
	if info.IsDir() {
		return "a real directory is in the way"
	}
	return "a real file is in the way"
}

// Apply carries out a plan.
//
// backupDir receives anything displaced. Nothing is ever deleted: the shell
// installer's contract was that a pre-existing file is recoverable, and a
// tool that silently discards a hand-written config people have had for years
// is not one worth trusting on a new machine.
func Apply(ops []Op, backupDir, home string, dryRun bool) error {
	for _, op := range ops {
		switch op.Kind {
		case Skip, Ignore:
			continue
		case Unfold:
			if err := unfold(op, dryRun); err != nil {
				return err
			}
			continue
		case Relink:
			if err := backup(op.Target, backupDir, home, dryRun); err != nil {
				return err
			}
		}
		if dryRun {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(op.Target), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(op.Target), err)
		}
		if err := os.Symlink(op.Source, op.Target); err != nil {
			return fmt.Errorf("linking %s: %w", op.Target, err)
		}
	}
	return nil
}

// unfold replaces a folded directory symlink with a real directory holding
// one link per child of the directory it used to point at.
//
// Nothing is backed up here and nothing needs to be: every path this touches
// is a symlink this tool created, and the files behind them do not move.
func unfold(op Op, dryRun bool) error {
	if dryRun {
		return nil
	}
	children, err := os.ReadDir(op.Source)
	if err != nil {
		return fmt.Errorf("unfolding %s: reading %s: %w", op.Target, op.Source, err)
	}
	if err := os.Remove(op.Target); err != nil {
		return fmt.Errorf("unfolding %s: %w", op.Target, err)
	}
	if err := os.MkdirAll(op.Target, 0o755); err != nil {
		return fmt.Errorf("unfolding %s: %w", op.Target, err)
	}
	for _, child := range children {
		from := filepath.Join(op.Source, child.Name())
		to := filepath.Join(op.Target, child.Name())
		if err := os.Symlink(from, to); err != nil {
			return fmt.Errorf("unfolding %s: linking %s: %w", op.Target, to, err)
		}
	}
	return nil
}

// backup moves a displaced path under backupDir.
//
// Stored relative to $HOME, which is what makes Restore a straight mapping
// back: an absolute-from-root layout would put the file at
// ~/.dotfiles-backups/<run>/Users/you/.zshrc and leave the restorer guessing
// which prefix to strip.
func backup(target, backupDir, home string, dryRun bool) error {
	if dryRun {
		return nil
	}
	rel, err := filepath.Rel(home, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("refusing to back up %s: it is not under %s", target, home)
	}
	dest := filepath.Join(backupDir, rel)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("creating backup directory: %w", err)
	}
	if err := os.Rename(target, dest); err != nil {
		return fmt.Errorf("backing up %s: %w", target, err)
	}
	return nil
}

// Unlink removes the symlinks a package owns, leaving anything it does not.
//
// Ownership is decided by where the link points, not by where it sits: a
// symlink at a path this package would have used, but pointing into a
// different checkout, belongs to that checkout and is left alone.
func Unlink(packageDir, home string, ignore *Ignorer, dryRun bool) ([]string, error) {
	ops, err := Plan(packageDir, home, ignore)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, op := range ops {
		if op.Kind != Skip {
			continue
		}
		removed = append(removed, op.Target)
		if dryRun {
			continue
		}
		if err := os.Remove(op.Target); err != nil {
			return removed, fmt.Errorf("removing %s: %w", op.Target, err)
		}
	}
	return removed, nil
}

// Count summarises a plan for a one-line report.
func Count(ops []Op) map[Kind]int {
	counts := map[Kind]int{}
	for _, op := range ops {
		counts[op.Kind]++
	}
	return counts
}

// Backups returns the backup directories under root, newest first.
//
// Each run of Apply writes into its own timestamped directory, so "restore"
// means "put the newest one back" rather than merging several runs together.
// The names are RFC-3339-ish and sort lexicographically in time order, which
// is the whole reason for that format.
func Backups(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", root, err)
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(root, entry.Name()))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	return dirs, nil
}

// Restore puts a backup directory's contents back into home.
//
// Whatever is at each target is removed first - by this point that is a
// symlink this tool created, and the file behind it stays in the repo. The
// backup is *moved* rather than copied, so a restore cannot silently leave a
// second copy behind to confuse the next one.
func Restore(backupDir, home string, dryRun bool) ([]string, error) {
	var restored []string
	err := filepath.Walk(backupDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(backupDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(home, rel)
		restored = append(restored, target)
		if dryRun {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(target), err)
		}
		// Remove rather than overwrite: a symlink is the expected occupant
		// here, and os.Rename onto one replaces the *link*, not its target -
		// which is right, but only if the link is gone first on the
		// filesystems where Rename refuses.
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clearing %s: %w", target, err)
		}
		if err := os.Rename(path, target); err != nil {
			return fmt.Errorf("restoring %s: %w", target, err)
		}
		return nil
	})
	if err != nil {
		return restored, err
	}
	return restored, nil
}
