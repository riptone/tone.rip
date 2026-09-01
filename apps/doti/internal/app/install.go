package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultRepoURL is where the configs come from when nothing says otherwise.
//
// Compiled in, and it has to be: the manifest lives *inside* the repository
// this clones, so on a new machine there is nothing to read it from. `--url`
// and DOTFILES_REPO_URL cover forks and renames.
const DefaultRepoURL = "https://github.com/riptone/dotfiles.git"

// RepoURL resolves where a clone comes from.
func RepoURL(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if fromEnv := os.Getenv("DOTFILES_REPO_URL"); fromEnv != "" {
		return fromEnv
	}
	return DefaultRepoURL
}

// gitInstallHint is what to tell someone whose machine has no git.
//
// doti does not install it. That needs a package manager and sudo, and a
// binary which silently escalates is a worse trade than one sentence -
// especially as the shell installer that fetched this binary has already done
// it in the case that matters.
func gitInstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "run `xcode-select --install`, finish the Command Line Tools install, then re-run"
	case "windows":
		return "run `winget install Git.Git`, then re-run"
	default:
		return "install git with your package manager (apt install git / dnf install git / pacman -S git), then re-run"
	}
}

// EnsureRepo makes sure there is a dotfiles checkout to work with.
//
// This is what makes `doti install` work on a machine that has never seen the
// configs: the binary arrives on its own and knows where to get the rest. An
// existing checkout is left alone - pulling here would drag install into
// moving someone's working tree under them, which is `sync`'s job and should
// be asked for by name.
func (a *App) EnsureRepo(ctx context.Context) error {
	if a.Cloned() {
		a.Report.Line(MarkOK, a.Repo)
		return nil
	}

	// A directory that exists but holds no manifest is far more likely a
	// typo'd --repo than a broken checkout, and cloning into it would fail
	// anyway.
	if entries, err := os.ReadDir(a.Repo); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s exists but has no manifest.jsonc - is that the right --repo?", a.Repo)
	}
	if !a.Runner.Look("git") {
		return fmt.Errorf("git is required to clone %s - %s", a.RepoURL, gitInstallHint())
	}
	if a.DryRun {
		a.Report.Line(MarkChange, fmt.Sprintf("would clone %s", a.RepoURL))
		return nil
	}

	done := a.Report.Working("cloning " + a.RepoURL)
	if err := os.MkdirAll(filepath.Dir(a.Repo), 0o755); err != nil {
		done(MarkWarn, "could not create the parent directory")
		return fmt.Errorf("creating %s: %w", filepath.Dir(a.Repo), err)
	}
	// --depth 1: this is a checkout to install from, not to develop in, and
	// the history is not small.
	if err := a.Runner.Run(ctx, "git", "clone", "--depth", "1", a.RepoURL, a.Repo); err != nil {
		done(MarkWarn, "clone failed")
		return fmt.Errorf("cloning the dotfiles repository: %w", err)
	}
	done(MarkChange, "cloned into "+a.Repo)
	return nil
}

// Install is the whole path: repository, packages, configs, secrets.
//
// The order matters and so does the error handling. Secrets come last and a
// failure there is a warning rather than a stop, because the vault is the one
// dependency a fresh machine may legitimately not have yet - `bw login` is
// interactive and this has to work with nobody watching. A machine with no
// vault still ends up fully configured, minus the credential files, and is
// told so.
func (a *App) Install(ctx context.Context) error {
	// Whether this run is the one that creates the checkout, asked before it
	// does. What the selector could offer at that point was a single row naming
	// the checkout, so a selection carried past here would narrow every list to
	// nothing - an install that ticked the only available box and then installed
	// no packages at all.
	fresh := !a.Cloned()
	a.Report.Phase("repository")
	if err := a.EnsureRepo(ctx); err != nil {
		return err
	}
	if fresh {
		a.Include = nil
	}
	if !a.Cloned() {
		// Only reachable under --dry-run, where the clone did not happen.
		a.Report.Summary("nothing further to preview until the repository is cloned")
		return nil
	}

	m, err := a.Manifest()
	if err != nil {
		return err
	}

	a.Report.Phase("packages")
	if err := a.InstallPackages(ctx); err != nil {
		return err
	}
	// Over the declared extras rather than by name, so the selector's rows and
	// the steps behind them come from one list. Only the names in
	// installableExtras are offered, and only those are dispatched here.
	for name := range installableExtras {
		if !a.WantsExtra(name) {
			continue
		}
		if err := a.InstallNerdFont(ctx); err != nil {
			return err
		}
	}
	a.installMcps(ctx, m.Mcps)

	a.Report.Phase("configs")
	if err := a.Link(); err != nil {
		return err
	}
	// After the configs, because the tracked .gitconfig is what includes it -
	// writing it first would leave a file nothing reads yet.
	if err := a.writeGitLocal(); err != nil {
		return err
	}
	if err := a.installSystemLinks(); err != nil {
		return err
	}

	if len(m.Secrets) == 0 {
		return nil
	}
	a.Report.Phase("secrets")
	if err := a.Secrets(ctx); err != nil {
		a.Report.Line(MarkWarn, err.Error())
		a.Report.Line(MarkSkip,
			"everything else is installed; re-run `doti secrets` once the vault is available")
	}
	return nil
}
