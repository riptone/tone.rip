package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
	"github.com/riptone/tone.rip/apps/doti/internal/pkgs"
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
	a.Report.Phase("repository")
	if err := a.EnsureRepo(ctx); err != nil {
		return err
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
	if a.WantsExtra("nerd-font") {
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

// WantsExtra reports whether the manifest declares a named extra for this
// platform. Extras are the things no package manager covers.
func (a *App) WantsExtra(name string) bool {
	m, err := a.Manifest()
	if err != nil {
		return false
	}
	for _, extra := range m.Extras {
		if extra.Name != name {
			continue
		}
		return len(extra.Platforms) == 0 || slices.Contains(extra.Platforms, a.Platform)
	}
	return false
}

// selectedTools narrows the missing set to what --tools named.
//
// An unknown name is an error rather than a silent no-op: someone typing
// `--tools fd,gh` and getting nothing would reasonably conclude the tools
// were already installed.
func (a *App) selectedTools(missing []manifest.Tool) ([]manifest.Tool, error) {
	if a.Tools == "" {
		return missing, nil
	}
	byCmd := map[string]manifest.Tool{}
	for _, tool := range missing {
		byCmd[tool.Cmd] = tool
	}
	var out []manifest.Tool
	for _, name := range strings.Split(a.Tools, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		tool, ok := byCmd[name]
		if !ok {
			available := make([]string, 0, len(byCmd))
			for cmd := range byCmd {
				available = append(available, cmd)
			}
			slices.Sort(available)
			return nil, fmt.Errorf("%q is not a missing tool (missing: %s)",
				name, strings.Join(available, ", "))
		}
		out = append(out, tool)
	}
	return out, nil
}

// InstallPackages brings the machine's tools up to the manifest.
func (a *App) InstallPackages(ctx context.Context) error {
	m, err := a.Manifest()
	if err != nil {
		return err
	}
	status := pkgs.Inspect(m, a.Runner)
	total := len(status.Present) + len(status.Missing)

	if len(status.Missing) == 0 {
		a.Report.Line(MarkOK, fmt.Sprintf("all %d tools present", total))
		return nil
	}
	wanted, err := a.selectedTools(status.Missing)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(wanted))
	for _, tool := range wanted {
		names = append(names, tool.Cmd)
	}
	a.Report.Line(MarkNone, fmt.Sprintf("%d of %d present; installing %s",
		len(status.Present), total, strings.Join(names, ", ")))

	if a.DryRun {
		a.Report.Line(MarkChange, "would install "+strings.Join(names, ", "))
		return nil
	}

	if a.Platform == manifest.Windows {
		return a.runWinget(ctx, m)
	}
	if !a.Runner.Look("brew") {
		return fmt.Errorf("homebrew is not installed - see https://brew.sh")
	}
	// A subset gets its own Brewfile: installing one missing tool must not
	// also pull in every cask the full file carries.
	body := pkgs.Brewfile(m)
	if a.Tools != "" {
		body = pkgs.BrewfileForTools(wanted)
	}
	file, cleanup, err := tempFile("Brewfile", body)
	if err != nil {
		return err
	}
	defer cleanup()

	done := a.Report.Working("brew bundle install")
	// --no-upgrade because install and update are different operations, and
	// the menu offers both. Without it `brew bundle install` upgrades every
	// outdated formula as a side effect, so asking for one missing tool
	// quietly moves your node version - a thing to opt into, not to discover
	// afterwards. `doti update` is where that lives.
	if err := a.Runner.Run(ctx, "brew", "bundle", "install", "--no-upgrade", "--file="+file); err != nil {
		done(MarkWarn, "brew bundle failed")
		return err
	}
	done(MarkChange, "installed the missing tools")
	return nil
}

func (a *App) runWinget(ctx context.Context, m *manifest.Manifest) error {
	body, err := pkgs.WingetPackages(m)
	if err != nil {
		return err
	}
	file, cleanup, err := tempFile("packages.json", body)
	if err != nil {
		return err
	}
	defer cleanup()

	done := a.Report.Working("winget import")
	if err := a.Runner.Run(ctx, "winget", "import", "-i", file,
		"--accept-package-agreements", "--accept-source-agreements"); err != nil {
		done(MarkWarn, "winget import failed")
		return err
	}
	done(MarkChange, "installed the missing tools")
	return nil
}

// installMcps installs the manifest's global npm packages.
//
// Global rather than npx-on-demand because these are MCP servers started on
// every editor launch, and npx re-resolves the registry each time.
//
// Best-effort: a missing npm or one failed package is a warning, not a failed
// install. None of them is load-bearing for a working shell.
func (a *App) installMcps(ctx context.Context, packages []string) {
	if len(packages) == 0 {
		return
	}
	if !a.Runner.Look("npm") {
		a.Report.Line(MarkSkip, "npm is not installed, so no MCP servers")
		return
	}
	if a.DryRun {
		a.Report.Line(MarkChange, fmt.Sprintf("would npm install -g %d package(s)", len(packages)))
		return
	}
	done := a.Report.Working(fmt.Sprintf("npm install -g (%d MCP servers)", len(packages)))
	var failed []string
	for _, pkg := range packages {
		if err := a.Runner.Run(ctx, "npm", "install", "-g", pkg); err != nil {
			failed = append(failed, pkg)
		}
	}
	if len(failed) > 0 {
		done(MarkWarn, fmt.Sprintf("%d of %d MCP servers did not install: %s",
			len(failed), len(packages), strings.Join(failed, ", ")))
		return
	}
	done(MarkChange, fmt.Sprintf("%d MCP servers installed", len(packages)))
}

// writeGitLocal writes the machine-local git config that the tracked
// .gitconfig includes.
//
// It holds the per-machine credential helper, which is why it cannot be
// tracked: the answer differs by OS, and in a shared repository it would be
// one machine's answer imposed on all of them.
//
// An existing file is never touched. It is the documented place for a
// per-machine identity override, so someone's email is very likely in it.
func (a *App) writeGitLocal() error {
	path := filepath.Join(a.Home, ".gitconfig.local")

	// Yielded, not raced. If a secret declares this path, the secrets phase
	// runs *after* this one and would overwrite whatever is written here -
	// so two mechanisms would be writing one file and the later would win by
	// accident. Whichever the manifest names is the one that owns it.
	if owner := a.secretOwning(path); owner != "" {
		a.Report.Line(MarkSkip, fmt.Sprintf(
			"~/.gitconfig.local (secret %q renders it)", owner))
		return nil
	}

	var helper string
	switch a.Platform {
	case manifest.MacOS:
		helper = "osxkeychain"
	case manifest.Linux:
		helper = "cache --timeout=3600"
	case manifest.Windows:
		helper = "manager"
	}

	if _, err := os.Stat(path); err == nil {
		a.Report.Line(MarkOK, "~/.gitconfig.local (left as it is)")
		return nil
	}
	if a.DryRun {
		a.Report.Line(MarkChange, fmt.Sprintf(
			"would write ~/.gitconfig.local (credential.helper=%s)", helper))
		return nil
	}

	var body strings.Builder
	body.WriteString("# Machine-local git config, written by doti and not tracked.\n")
	body.WriteString("# Safe to edit - a per-machine user.email belongs here.\n")
	if helper != "" {
		fmt.Fprintf(&body, "[credential]\n\thelper = %s\n", helper)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	a.Report.Line(MarkChange, "~/.gitconfig.local")
	return nil
}

// secretOwning returns the name of the secret that renders path, if any.
//
// Compared after expansion, because a manifest writes `~/x` and this code
// holds an absolute path.
func (a *App) secretOwning(path string) string {
	m, err := a.Manifest()
	if err != nil {
		return ""
	}
	for _, secret := range m.Secrets {
		if !secret.WantsPlatform(a.Platform) {
			continue
		}
		if a.Expand(secret.Target) == path {
			return secret.Name
		}
	}
	return ""
}

// SystemLink is a symlink whose target is outside $HOME, so the stow engine
// cannot place it: stow mirrors a package tree into the home directory, and
// these live in Windows' own application-data paths.
type SystemLink struct {
	Name string
	// Source is relative to the repository root.
	Source string
	// Target is absolute, resolved from the environment.
	Target string
}

// SystemLinks are the per-platform links the manifest's system_components
// describe. Empty off Windows, where every config this repo carries is
// reachable from $HOME and therefore already a stow package.
func (a *App) SystemLinks() []SystemLink {
	if a.Platform != manifest.Windows {
		return nil
	}
	var links []SystemLink

	// Windows Terminal stores its settings under its packaged app's
	// LocalState, which is not in $HOME's tree.
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		links = append(links, SystemLink{
			Name:   "windows-terminal",
			Source: filepath.Join("win", "terminal", "settings.json"),
			Target: filepath.Join(localAppData, "Packages",
				"Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json"),
		})
	}
	// $PROFILE for PowerShell 7. Documents is the documented location and is
	// what $PROFILE resolves to on a default install.
	if profile := os.Getenv("USERPROFILE"); profile != "" {
		links = append(links, SystemLink{
			Name:   "powershell-profile",
			Source: filepath.Join("win", "powershell", "profile.ps1"),
			Target: filepath.Join(profile, "Documents", "PowerShell",
				"Microsoft.PowerShell_profile.ps1"),
		})
	}
	return links
}

// installSystemLinks places the links whose targets are outside $HOME.
//
// Reported per link rather than as a group, because on Windows these are the
// two that most often fail: symlinks need Developer Mode or an elevated
// shell, and a silent failure here leaves a terminal with default settings
// and no explanation.
func (a *App) installSystemLinks() error {
	m, err := a.Manifest()
	if err != nil {
		return err
	}
	declared := map[string]bool{}
	for _, component := range m.SystemComponents {
		if len(component.Platforms) == 0 || slices.Contains(component.Platforms, a.Platform) {
			declared[component.Name] = true
		}
	}

	for _, link := range a.SystemLinks() {
		if !declared[link.Name] {
			continue
		}
		source := filepath.Join(a.Repo, link.Source)
		if _, err := os.Stat(source); err != nil {
			a.Report.Line(MarkSkip, fmt.Sprintf("%s: no %s in the repo", link.Name, link.Source))
			continue
		}

		if existing, err := os.Readlink(link.Target); err == nil && existing == source {
			a.Report.Line(MarkOK, link.Name)
			continue
		}
		if a.DryRun {
			a.Report.Line(MarkChange, fmt.Sprintf("would link %s -> %s", link.Name, link.Target))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(link.Target), 0o755); err != nil {
			return fmt.Errorf("%s: creating %s: %w", link.Name, filepath.Dir(link.Target), err)
		}
		// Removed rather than overwritten: os.Symlink refuses an existing
		// path, and whatever is there is either our own older link or a file
		// the user will want back - so it goes to the backup tree first.
		if err := a.displace(link.Target); err != nil {
			return fmt.Errorf("%s: %w", link.Name, err)
		}
		if err := os.Symlink(source, link.Target); err != nil {
			// Not fatal: on Windows this is the Developer Mode failure, and
			// the rest of the install is still worth having.
			a.Report.Line(MarkWarn, fmt.Sprintf(
				"%s: could not link (%v) - Windows needs Developer Mode or an elevated shell",
				link.Name, err))
			continue
		}
		a.Report.Line(MarkChange, link.Name)
	}
	return nil
}

// displace moves whatever is at path into the backup tree, if anything is.
func (a *App) displace(path string) error {
	if _, err := os.Lstat(path); err != nil {
		return nil
	}
	backup := filepath.Join(a.Home, BackupsDir, "system",
		filepath.Base(path)+".displaced")
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return err
	}
	if err := os.Rename(path, backup); err != nil {
		return fmt.Errorf("backing up %s: %w", path, err)
	}
	a.Report.Line(MarkWarn, "backed up "+path)
	return nil
}
