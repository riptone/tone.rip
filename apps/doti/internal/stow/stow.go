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
			if linkDest == source {
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
	return info, dest, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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
func Apply(ops []Op, backupDir string, dryRun bool) error {
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
			if err := backup(op.Target, backupDir, dryRun); err != nil {
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

// backup moves a displaced path under backupDir, preserving its position
// relative to $HOME so a restore is a straight copy back.
func backup(target, backupDir string, dryRun bool) error {
	if dryRun {
		return nil
	}
	rel := strings.TrimPrefix(target, string(filepath.Separator))
	if vol := filepath.VolumeName(rel); vol != "" {
		rel = strings.TrimPrefix(rel[len(vol):], string(filepath.Separator))
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
