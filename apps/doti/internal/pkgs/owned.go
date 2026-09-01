// Asking a package manager what it installed, which is a different question
// from asking whether a command exists.
//
// Inspect answers "can I run this", by name on PATH, and that is the right
// question for an install: a tool that arrived some other way still works, and
// pretending otherwise is what would make `adopt` reinstall half a machine.
//
// It is the wrong question for a removal, and the difference is not theoretical.
// macOS ships /usr/bin/jq. `brew uninstall jq` had already run, brew owned
// nothing called jq, and `command -v jq` still answered - so the removal
// selector kept offering jq as "installed", every session, across reboots,
// because it was reading PATH and PATH was right. Removing software is the one
// operation that has to ask the thing that installed it.
package pkgs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// Owned is the set of package names the platform's package manager installed,
// keyed the way the manifest names them - `brew` on macOS and Linux, `winget`
// on Windows.
//
// One inventory call rather than one query per package: `brew list` is ~40ms
// for the whole machine, and sixteen `winget list --id` invocations would be
// felt on a screen somebody is waiting for.
func Owned(ctx context.Context, platform manifest.Platform, runner Runner) (map[string]bool, error) {
	if platform == manifest.Windows {
		return wingetOwned(ctx, runner)
	}
	return brewOwned(ctx, platform, runner)
}

// brewOwned reads `brew list`.
//
// Formulae and casks separately, because they are two namespaces and the
// manifest draws from both - `bat` is a formula and `ghostty` is a cask, and a
// single `brew list` conflates them into one column of names.
//
// Casks are macOS-only: `brew list --cask` on Linux exits non-zero, and that is
// not a failure to report.
func brewOwned(ctx context.Context, platform manifest.Platform, runner Runner) (map[string]bool, error) {
	if !runner.Look("brew") {
		// Nothing brew installed, because there is no brew. Not an error: a
		// machine without it has an empty removal list, which is true.
		return map[string]bool{}, nil
	}
	out, err := runner.Output(ctx, "brew", "list", "--formula", "-1")
	if err != nil {
		return nil, fmt.Errorf("asking brew what it installed: %w", err)
	}
	owned := names(out)
	if platform == manifest.MacOS {
		if casks, err := runner.Output(ctx, "brew", "list", "--cask", "-1"); err == nil {
			for name := range names(casks) {
				owned[name] = true
			}
		}
	}
	return owned, nil
}

// wingetOwned reads `winget export`, which writes the same file WingetPackages
// renders - the inventory in the one shape this package already models, rather
// than the localised table `winget list` prints.
func wingetOwned(ctx context.Context, runner Runner) (map[string]bool, error) {
	if !runner.Look("winget") {
		return map[string]bool{}, nil
	}
	file, cleanup, err := exportFile()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// winget exits non-zero when some installed package has no matching
	// manifest in a source, having written the file anyway. That is the normal
	// state of a Windows machine, so the file is what gets checked - not the
	// exit status.
	_, _ = runner.Output(ctx, "winget", "export", "--output", file,
		"--accept-source-agreements", "--disable-interactivity")

	body, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("reading the winget export: %w", err)
	}
	var exported wingetFile
	if err := json.Unmarshal(body, &exported); err != nil {
		return nil, fmt.Errorf("parsing the winget export: %w", err)
	}
	owned := map[string]bool{}
	for _, source := range exported.Sources {
		for _, pkg := range source.Packages {
			owned[pkg.PackageIdentifier] = true
		}
	}
	return owned, nil
}

// exportFile makes the path winget writes its inventory to.
//
// Created and closed rather than merely named, so the path is one this process
// owns - and removed immediately, because an inventory of somebody's machine is
// not a thing to leave in the temp directory.
func exportFile() (string, func(), error) {
	f, err := os.CreateTemp("", "doti-winget-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("making a temp file for the winget export: %w", err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		return "", nil, fmt.Errorf("closing %s: %w", path, err)
	}
	return path, func() { _ = os.Remove(path) }, nil
}

// names splits command output into a set, one name per line.
func names(out []byte) map[string]bool {
	set := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			set[name] = true
		}
	}
	return set
}

// Formula is how brew keys a formula in its own inventory: the name with any
// tap stripped off.
//
// A manifest may name a formula tap-qualified, and for some tools it has to -
// `anomalyco/tap/opencode` is the spelling opencode's own docs recommend,
// because homebrew-core's copy lags. `brew install` and `brew uninstall` both
// take that name happily. `brew list` does not give it back: it prints the
// Cellar's short names, so the qualified spelling matched nothing in the owned
// set and a tap-qualified tool would have been permanently invisible to the
// removal selector and permanently "missing" in the install one - the jq bug
// again, arrived at from the other side.
//
// `brew list --full-name` would print the qualified names, and was measured at
// 221-292ms against 12-16ms for the short list on the same 51 formulae: it has
// to load tap metadata where the short list only reads directory names. Fifteen
// times the cost of the thing it would fix, on the path that opens the menu.
// Stripping the manifest's side is free.
func Formula(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// BunGlobals reports which of the named packages `bun install -g` has put on
// the machine.
//
// The Windows counterpart of NpmGlobals, and the reason the manifest has a `bun`
// field at all: a bun global is invisible to `winget export`, so without this a
// tool installed that way reads as missing forever in the install selector and
// is never offered for removal - the same shape of bug a tap-qualified brew name
// had, from a third direction.
//
// No subprocess. bun's global prefix is $BUN_INSTALL, defaulting to ~/.bun, and
// packages land in install/global/node_modules underneath it - which is bun's
// own account of the layout: `bun pm bin -g` on a machine that has never
// installed one answers `No package.json was found for directory
// "<home>/.bun/install/global"`. Asking bun would also have meant handling that
// error as "none" rather than as a failure, where a stat that finds nothing
// already says exactly that.
func BunGlobals(home string, packages []string) map[string]bool {
	present := map[string]bool{}
	if len(packages) == 0 {
		return present
	}
	prefix := os.Getenv("BUN_INSTALL")
	if prefix == "" {
		if home == "" {
			return present
		}
		prefix = filepath.Join(home, ".bun")
	}
	root := filepath.Join(prefix, "install", "global", "node_modules")
	for _, pkg := range packages {
		// A scoped name is two directories deep, exactly as npm lays it out -
		// bun keeps the same shape.
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(pkg))); err == nil {
			present[pkg] = true
		}
	}
	return present
}

// NpmGlobals reports which of the named global npm packages are installed.
//
// By `npm root -g` and a stat each, not `npm ls -g`: the root is one 120ms
// invocation and the stats are free, where `npm ls -g` walks and resolves the
// whole global tree at ~700ms - slow enough that the selector used to show
// these as "declared" and decline to say whether they were there.
//
// A package that is not installed is absent from the map rather than an error.
func NpmGlobals(ctx context.Context, runner Runner, packages []string) (map[string]bool, error) {
	if len(packages) == 0 || !runner.Look("npm") {
		return map[string]bool{}, nil
	}
	out, err := runner.Output(ctx, "npm", "root", "-g")
	if err != nil {
		return nil, fmt.Errorf("asking npm where its global packages live: %w", err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return map[string]bool{}, nil
	}
	present := map[string]bool{}
	for _, pkg := range packages {
		// A scoped name carries its own separator, which filepath.Join folds
		// into the path - @scope/name is two directories deep, and that is
		// exactly how npm lays it out.
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(pkg))); err == nil {
			present[pkg] = true
		}
	}
	return present, nil
}
