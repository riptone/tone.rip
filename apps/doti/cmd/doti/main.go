// Command doti installs and maintains this machine's dotfiles.
//
// It is replacing scripts/install.sh and scripts/Install.ps1 in the dotfiles
// repository, which implement the same operations twice - once in bash for
// macOS/Linux and once in PowerShell for Windows - and carry three separate
// "keep them in sync" rules to hold the two in step. One binary that
// cross-compiles removes the class of bug rather than the instances.
//
// Only the manifest reader and the secrets renderer exist so far. Stow,
// package installation and the interactive menu still live in the shell
// scripts, and this does not replace them until `doti --check` reports parity
// on a real machine.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/riptone/tone.rip/apps/doti/internal/health"
	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
	"github.com/riptone/tone.rip/apps/doti/internal/pkgs"
	"github.com/riptone/tone.rip/apps/doti/internal/secrets"
	"github.com/riptone/tone.rip/apps/doti/internal/stow"
	"github.com/riptone/tone.rip/apps/doti/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

// version is stamped at build time with -ldflags -X. Unstamped builds stay
// "dev" so nothing can mistake a working copy for a release.
var version = "dev"

const usage = `doti - dotfiles installer

usage:
  doti menu     [--repo DIR]            interactive menu
  doti install  [--repo DIR] [-n]       packages, then configs, then secrets
  doti adopt    [--repo DIR] [-n]       scan first, then install only the gaps
  doti check    [--repo DIR] [--strict] verify tools and symlinks, change nothing
  doti sync     [--repo DIR] [-n]       git pull, then re-link
  doti update   [--repo DIR]            upgrade installed packages
  doti validate [--repo DIR]            parse and check manifest.jsonc
  doti link     [--repo DIR] [-n]       link configs into $HOME
  doti unlink   [--repo DIR] [-n]       remove the links this repo owns
  doti packages [--repo DIR] [--brew|--winget]
                                        print the generated package lists
  doti secrets  [--repo DIR] [-n]       render secrets from Bitwarden
  doti preview  [--repo DIR] [--frames DIR]
                                        run the menu, or dump frames to files
  doti version

flags:
  --repo DIR   dotfiles checkout (default $DOTFILES_DIR, else ~/dotfiles)
  -n           dry run: report what would change, write nothing
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "doti: %v\n", err)
		os.Exit(1)
	}
}

func run(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	repo := flags.String("repo", defaultRepo(), "dotfiles checkout")
	dryRun := flags.Bool("n", false, "report what would change, write nothing")
	brewOnly := flags.Bool("brew", false, "packages: Brewfile only")
	wingetOnly := flags.Bool("winget", false, "packages: winget list only")
	frames := flags.String("frames", "", "preview: write frames here instead of running")
	strict := flags.Bool("strict", false, "check: exit non-zero when something is missing")
	if err := flags.Parse(args); err != nil {
		return err
	}

	switch command {
	case "version":
		fmt.Println(version)
		return nil
	case "validate":
		return validate(*repo)
	case "adopt":
		return adopt(context.Background(), *repo, *dryRun)
	case "check":
		return check(*repo, *strict)
	case "sync":
		return sync(context.Background(), *repo, *dryRun)
	case "update":
		return update(context.Background(), *repo)
	case "menu":
		return runMenu(context.Background(), *repo)
	case "preview":
		return preview(*repo, *frames)
	case "install":
		return install(context.Background(), *repo, *dryRun)
	case "link":
		return link(*repo, *dryRun)
	case "unlink":
		return unlink(*repo, *dryRun)
	case "packages":
		return printPackages(*repo, *brewOnly, *wingetOnly)
	case "secrets":
		return renderSecrets(context.Background(), *repo, *dryRun)
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", command)
	}
}

func defaultRepo() string {
	if dir := os.Getenv("DOTFILES_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "dotfiles")
}

func load(repo string) (*manifest.Manifest, error) {
	return manifest.Load(filepath.Join(repo, "manifest.jsonc"))
}

func validate(repo string) error {
	m, err := load(repo)
	if err != nil {
		return err
	}
	fmt.Printf("%s %s - manifest ok\n", m.App, m.Version)
	fmt.Printf("  %d stow packages, %d tools, %d secrets\n",
		len(m.StowPackages), len(m.Tools), len(m.Secrets))
	return nil
}

// currentPlatform maps GOOS onto the manifest's vocabulary.
func currentPlatform() (manifest.Platform, error) {
	switch runtime.GOOS {
	case "darwin":
		return manifest.MacOS, nil
	case "linux":
		return manifest.Linux, nil
	case "windows":
		return manifest.Windows, nil
	default:
		return "", fmt.Errorf("unsupported platform %q", runtime.GOOS)
	}
}

func renderSecrets(ctx context.Context, repo string, dryRun bool) error {
	m, err := load(repo)
	if err != nil {
		return err
	}
	if len(m.Secrets) == 0 {
		fmt.Println("no secrets declared in manifest.jsonc")
		return nil
	}

	platform, err := currentPlatform()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("finding home directory: %w", err)
	}

	// BW_SESSION from the environment, never from a file: a session key on
	// disk is a vault with the lock left open.
	client := secrets.New(secrets.ExecRunner{}, os.Getenv("BW_SESSION"))
	if err := client.RequireUnlocked(ctx); err != nil {
		return err
	}
	// `bw` answers from a local cache, so without this a rotated credential
	// renders as the old value and nothing says so.
	if err := client.Sync(ctx); err != nil {
		return fmt.Errorf("syncing vault: %w", err)
	}

	renderer := &secrets.Renderer{
		Client:   client,
		RepoRoot: repo,
		Home:     home,
		Platform: platform,
		DryRun:   dryRun,
	}
	results, err := renderer.RenderAll(ctx, m.Secrets)
	// Print what did land before returning the failure - a partial run is
	// worth knowing about.
	for _, r := range results {
		switch {
		case r.Skipped:
			fmt.Printf("  skip    %s (%s)\n", r.Name, r.Reason)
		case r.Changed && dryRun:
			fmt.Printf("  would   %s -> %s\n", r.Name, r.Target)
		case r.Changed:
			fmt.Printf("  wrote   %s -> %s\n", r.Name, r.Target)
		default:
			fmt.Printf("  ok      %s (unchanged)\n", r.Name)
		}
	}
	return err
}

// setup resolves the pieces every linking command needs.
func setup(repo string) (*manifest.Manifest, *stow.Ignorer, string, manifest.Platform, error) {
	m, err := load(repo)
	if err != nil {
		return nil, nil, "", "", err
	}
	ignore, err := stow.NewIgnorer(m.StowIgnore)
	if err != nil {
		return nil, nil, "", "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, "", "", fmt.Errorf("finding home directory: %w", err)
	}
	platform, err := currentPlatform()
	if err != nil {
		return nil, nil, "", "", err
	}
	return m, ignore, home, platform, nil
}

// wanted returns the stow packages that apply to this platform, in manifest
// order. The order is load-bearing: the `stow` package carries
// ~/.stow-global-ignore and is listed first so it is in place before anything
// else is linked.
func wanted(m *manifest.Manifest, platform manifest.Platform) []manifest.StowPackage {
	var out []manifest.StowPackage
	for _, pkg := range m.StowPackages {
		if len(pkg.Platforms) == 0 || slices.Contains(pkg.Platforms, platform) {
			out = append(out, pkg)
		}
	}
	return out
}

func link(repo string, dryRun bool) error {
	m, ignore, home, platform, err := setup(repo)
	if err != nil {
		return err
	}

	// One backup directory per run, stamped, so a restore is "copy the newest
	// one back" rather than a merge of several runs.
	backupDir := filepath.Join(home, ".dotfiles-backups",
		time.Now().UTC().Format("2006-01-02T15-04-05Z"))

	for _, pkg := range wanted(m, platform) {
		ops, err := stow.Plan(filepath.Join(repo, pkg.Name), home, ignore)
		if err != nil {
			return err
		}
		counts := stow.Count(ops)
		verb := "linked"
		if dryRun {
			verb = "would link"
		}
		fmt.Printf("  %-10s %s %d, relinked %d, already %d, ignored %d\n",
			pkg.Name, verb, counts[stow.Link], counts[stow.Relink],
			counts[stow.Skip], counts[stow.Ignore])
		for _, op := range ops {
			if op.Kind == stow.Relink {
				fmt.Printf("      backup  %s (%s)\n", op.Target, op.Reason)
			}
		}
		if err := stow.Apply(ops, backupDir, dryRun); err != nil {
			return err
		}
	}
	return nil
}

func unlink(repo string, dryRun bool) error {
	m, ignore, home, platform, err := setup(repo)
	if err != nil {
		return err
	}
	for _, pkg := range wanted(m, platform) {
		removed, err := stow.Unlink(filepath.Join(repo, pkg.Name), home, ignore, dryRun)
		if err != nil {
			return err
		}
		verb := "removed"
		if dryRun {
			verb = "would remove"
		}
		fmt.Printf("  %-10s %s %d link(s)\n", pkg.Name, verb, len(removed))
	}
	return nil
}

func printPackages(repo string, brewOnly, wingetOnly bool) error {
	m, err := load(repo)
	if err != nil {
		return err
	}
	both := !brewOnly && !wingetOnly
	if brewOnly || both {
		fmt.Print(pkgs.Brewfile(m))
	}
	if both {
		fmt.Println()
	}
	if wingetOnly || both {
		out, err := pkgs.WingetPackages(m)
		if err != nil {
			return err
		}
		fmt.Print(out)
	}
	return nil
}

// install is the whole path: packages, then configs, then secrets.
//
// The order matters and so does the error handling. Secrets come last and a
// failure there is a warning rather than a stop, because the vault is the one
// dependency a fresh machine may legitimately not have yet - `bw login` is
// interactive and this has to work under `--all` with nobody watching. A
// machine with no vault still ends up fully configured, minus the credential
// files, and is told so.
func install(ctx context.Context, repo string, dryRun bool) error {
	m, _, _, platform, err := setup(repo)
	if err != nil {
		return err
	}

	fmt.Println("packages")
	if err := installPackages(ctx, m, platform, dryRun); err != nil {
		return err
	}

	fmt.Println("\nconfigs")
	if err := link(repo, dryRun); err != nil {
		return err
	}

	if len(m.Secrets) == 0 {
		return nil
	}
	fmt.Println("\nsecrets")
	if err := renderSecrets(ctx, repo, dryRun); err != nil {
		fmt.Printf("  skipped: %v\n", err)
		fmt.Println("  everything else is installed; re-run `doti secrets` once the vault is available")
	}
	return nil
}

func installPackages(ctx context.Context, m *manifest.Manifest, platform manifest.Platform, dryRun bool) error {
	runner := pkgs.ExecRunner{Out: os.Stdout}
	status := pkgs.Inspect(m, runner.Look)
	fmt.Printf("  %d of %d tools already present\n",
		len(status.Present), len(status.Present)+len(status.Missing))
	for _, tool := range status.Missing {
		fmt.Printf("      missing %s\n", tool.Cmd)
	}
	if len(status.Missing) == 0 {
		return nil
	}

	if platform == manifest.Windows {
		if dryRun {
			fmt.Println("  would run: winget import (generated from manifest.jsonc)")
			return nil
		}
		return runWinget(ctx, runner, m)
	}

	if dryRun {
		fmt.Println("  would run: brew bundle (generated from manifest.jsonc)")
		return nil
	}
	if !runner.Look("brew") {
		return fmt.Errorf("homebrew is not installed - see https://brew.sh")
	}
	file, cleanup, err := tempFile("Brewfile", pkgs.Brewfile(m))
	if err != nil {
		return err
	}
	defer cleanup()
	return runner.Run(ctx, "brew", "bundle", "--no-lock", "--file="+file)
}

func runWinget(ctx context.Context, runner pkgs.Runner, m *manifest.Manifest) error {
	body, err := pkgs.WingetPackages(m)
	if err != nil {
		return err
	}
	file, cleanup, err := tempFile("packages.json", body)
	if err != nil {
		return err
	}
	defer cleanup()
	return runner.Run(ctx, "winget", "import", "-i", file,
		"--accept-package-agreements", "--accept-source-agreements")
}

// tempFile writes a generated package list somewhere disposable.
//
// Deliberately not into the repo: the shell installer generated these at
// install time precisely so neither file is committed and neither can drift
// from the manifest.
func tempFile(name, body string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "doti-")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", nil, fmt.Errorf("writing %s: %w", path, err)
	}
	return path, func() { os.RemoveAll(dir) }, nil
}

// collectItems describes the machine to the selector: what is installed, what
// is linked, what has been rendered. Everything defaults to ticked, because
// re-running a step is how drift gets repaired and the common case is "yes,
// all of it".
func collectItems(repo string) ([]tui.Item, error) {
	m, ignore, home, platform, err := setup(repo)
	if err != nil {
		return nil, err
	}

	runner := pkgs.ExecRunner{Out: io.Discard}
	status := pkgs.Inspect(m, runner.Look)
	total := len(status.Present) + len(status.Missing)
	items := []tui.Item{{
		Group:    "Packages",
		Label:    "brew packages",
		Status:   fmt.Sprintf("%d of %d present", len(status.Present), total),
		Done:     len(status.Missing) == 0,
		Selected: true,
	}}
	for _, extra := range m.Extras {
		if len(extra.Platforms) == 0 || slices.Contains(extra.Platforms, platform) {
			items = append(items, tui.Item{
				Group: "Packages", Label: extra.Name, Status: "not checked", Selected: true,
			})
		}
	}

	for _, pkg := range wanted(m, platform) {
		state := "not linked"
		done := false
		if ops, err := stow.Plan(filepath.Join(repo, pkg.Name), home, ignore); err == nil {
			counts := stow.Count(ops)
			if counts[stow.Link] == 0 && counts[stow.Relink] == 0 && counts[stow.Unfold] == 0 {
				state, done = "linked", true
			} else if counts[stow.Skip] > 0 {
				state = "partly linked"
			}
		}
		items = append(items, tui.Item{
			Group: "Configs", Label: pkg.Name, Status: state, Done: done, Selected: true,
		})
	}

	for _, secret := range m.Secrets {
		if !secret.WantsPlatform(platform) {
			continue
		}
		state, done := "not rendered", false
		target := secret.Target
		if strings.HasPrefix(target, "~/") {
			target = filepath.Join(home, strings.TrimPrefix(target, "~/"))
		}
		if _, err := os.Stat(target); err == nil {
			state, done = "rendered", true
		}
		items = append(items, tui.Item{
			Group: "Secrets", Label: secret.Name, Status: state, Done: done, Selected: true,
		})
	}
	return items, nil
}

func runMenu(ctx context.Context, repo string) error {
	items, err := collectItems(repo)
	if err != nil {
		return err
	}
	model := tui.New(tui.Config{Items: items, Version: version, Width: 80, Height: 24})

	final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return fmt.Errorf("running the menu: %w", err)
	}
	chosen, ok := final.(tui.Model)
	if !ok {
		return nil
	}

	switch chosen.Action() {
	case tui.ActionNone:
		return nil
	case tui.ActionInstall:
		if !chosen.Confirmed() {
			return nil
		}
		return install(ctx, repo, false)
	case tui.ActionAdopt:
		if !chosen.Confirmed() {
			return nil
		}
		return adopt(ctx, repo, false)
	case tui.ActionPreview:
		return install(ctx, repo, true)
	case tui.ActionUnlink:
		return unlink(repo, false)
	case tui.ActionCheck:
		return check(repo, false)
	case tui.ActionSync:
		return sync(ctx, repo, false)
	case tui.ActionUpdate:
		return update(ctx, repo)
	}
	return nil
}

func preview(repo, framesDir string) error {
	if framesDir == "" {
		return runMenu(context.Background(), repo)
	}
	items, err := collectItems(repo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(framesDir, 0o755); err != nil {
		return err
	}
	for i, frame := range tui.Frames(items, version, 80, 26) {
		path := filepath.Join(framesDir, fmt.Sprintf("%02d-%s.ansi", i+1, frame.Name))
		if err := os.WriteFile(path, []byte(frame.Body), 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", path)
	}
	return nil
}

// scan is the read-only look at the machine that `check` prints and `adopt`
// acts on.
func scan(repo string) (health.Report, error) {
	m, _, home, platform, err := setup(repo)
	if err != nil {
		return health.Report{}, err
	}
	runner := pkgs.ExecRunner{Out: io.Discard}
	return health.Check(health.Options{
		Manifest: m, Platform: platform, Repo: repo, Home: home, Look: runner.Look,
	}), nil
}

// check prints the report and changes nothing.
//
// --strict is what makes this usable from a login shell or a cron job: the
// default exit code is 0 even with drift, because "tell me" and "fail" are
// different questions and only the caller knows which one it is asking.
func check(repo string, strict bool) error {
	report, err := scan(repo)
	if err != nil {
		return err
	}
	passed, total := report.Counts()
	fmt.Printf("%d of %d checks passed\n", passed, total)
	for _, finding := range report.Missing() {
		fmt.Printf("  %-6s %-34s %s\n", finding.Kind, finding.Name, finding.Detail)
	}
	if strict && !report.OK() {
		return fmt.Errorf("%d check(s) failed", len(report.Missing()))
	}
	return nil
}

// adopt is install for a machine that is already in use: scan, say what is
// already there, then act only on the gaps.
//
// The scan is the whole point. Someone running this has tools they installed
// by hand and configs they wrote years ago, and the question they actually
// want answered before anything is touched is "what are you about to do".
func adopt(ctx context.Context, repo string, dryRun bool) error {
	report, err := scan(repo)
	if err != nil {
		return err
	}
	passed, total := report.Counts()
	fmt.Printf("scan: %d of %d already in place\n", passed, total)
	for _, finding := range report.Missing() {
		fmt.Printf("  gap    %-6s %-30s %s\n", finding.Kind, finding.Name, finding.Detail)
	}
	if report.OK() {
		fmt.Println("\nnothing to do")
		return nil
	}
	fmt.Println()
	// `brew bundle` and the link planner are both already idempotent - they
	// skip what exists - so acting on the gaps is the same call as install.
	// What adopt adds is the report above, before anything happens.
	return install(ctx, repo, dryRun)
}

// sync brings the repo forward and re-links.
func sync(ctx context.Context, repo string, dryRun bool) error {
	runner := pkgs.ExecRunner{Out: os.Stdout}
	if dryRun {
		fmt.Printf("would run: git -C %s pull --ff-only\n", repo)
	} else {
		// --ff-only rather than a merge: this runs unattended, and a sync
		// that stops to ask about a merge conflict is a sync that hangs.
		if err := runner.Run(ctx, "git", "-C", repo, "pull", "--ff-only"); err != nil {
			return fmt.Errorf("%w (resolve it by hand, then re-run)", err)
		}
	}
	fmt.Println()
	return link(repo, dryRun)
}

// update upgrades what the package manager installed, and nothing else.
func update(ctx context.Context, repo string) error {
	_, _, _, platform, err := setup(repo)
	if err != nil {
		return err
	}
	runner := pkgs.ExecRunner{Out: os.Stdout}

	if platform == manifest.Windows {
		return runner.Run(ctx, "winget", "upgrade", "--all",
			"--accept-package-agreements", "--accept-source-agreements")
	}
	if !runner.Look("brew") {
		return fmt.Errorf("homebrew is not installed - see https://brew.sh")
	}
	if err := runner.Run(ctx, "brew", "update"); err != nil {
		return err
	}
	return runner.Run(ctx, "brew", "upgrade")
}
