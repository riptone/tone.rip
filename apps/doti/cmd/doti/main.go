// Command doti installs and maintains this machine's dotfiles.
//
// It replaced scripts/install.sh and scripts/Install.ps1 in the dotfiles
// repository, which implemented the same forty-odd operations twice - once in
// bash for macOS and Linux, once in PowerShell for Windows - and carried three
// "keep them in sync" rules to hold the two in step. One binary that
// cross-compiles removes the class of bug rather than the instances.
//
// This file is flags and dispatch only. What the commands *do* lives in
// internal/app, where it is reachable from a test, and none of it prints:
// commands report, and the rendering is chosen once, here, from whether
// anything is watching. That is what makes `doti install` and the menu's
// Install the same thing rather than two things that agree.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/apps/doti/internal/pkgs"
	"github.com/riptone/tone.rip/apps/doti/internal/tui"
)

// version is stamped at build time with -ldflags -X. Unstamped builds stay
// "dev" so nothing can mistake a working copy for a release.
var version = "dev"

const usage = `doti - dotfiles installer

usage:
  doti                      interactive menu
  doti install              clone if needed, then packages, configs, secrets
  doti adopt                scan first, then act only on the gaps
  doti check                verify tools and symlinks; changes nothing
  doti link                 link configs into $HOME
  doti unlink               remove the links this repo owns
  doti sync                 git pull --ff-only, then re-link
  doti update               upgrade installed packages
  doti secrets              render secret files from Bitwarden
  doti upgrade              replace this binary with the newest release
  doti packages             print the generated package lists
  doti validate             parse and check manifest.jsonc
  doti preview              run the menu, or --frames DIR to dump screens
  doti version

flags:
  --repo DIR    dotfiles checkout (default $DOTFILES_DIR, else ~/dotfiles)
  --url URL     install: clone from here (default $DOTFILES_REPO_URL)
  --only PKG    link/unlink: act on this stow package alone
  --tools LIST  install: only these missing tools (comma separated)
  --restore     unlink: move the newest backup back afterwards
  --strict      check: exit non-zero when something is missing
  --brew        packages: Brewfile only
  --winget      packages: winget list only
  --frames DIR  preview: write screens here instead of running
  --verbose     stream subprocess output instead of capturing it
  -n            dry run: report what would change, write nothing
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "doti: %v\n", err)
		os.Exit(1)
	}
}

type options struct {
	repo    string
	url     string
	only    string
	tools   string
	frames  string
	restore bool
	strict  bool
	brew    bool
	winget  bool
	verbose bool
	dryRun  bool
}

func run(args []string) error {
	// No arguments is the menu. That is the shape the shell installer had and
	// the one people's hands know.
	command := "menu"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	if command == "-h" || command == "--help" || command == "help" {
		fmt.Print(usage)
		return nil
	}
	if command == "version" {
		fmt.Println(version)
		return nil
	}

	var opts options
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&opts.repo, "repo", defaultRepo(), "dotfiles checkout")
	flags.StringVar(&opts.url, "url", "", "clone from here")
	flags.StringVar(&opts.only, "only", "", "act on this stow package alone")
	flags.StringVar(&opts.tools, "tools", "", "install only these missing tools")
	flags.StringVar(&opts.frames, "frames", "", "write preview screens here")
	flags.BoolVar(&opts.restore, "restore", false, "put the newest backup back")
	flags.BoolVar(&opts.strict, "strict", false, "exit non-zero when something is missing")
	flags.BoolVar(&opts.brew, "brew", false, "Brewfile only")
	flags.BoolVar(&opts.winget, "winget", false, "winget list only")
	flags.BoolVar(&opts.verbose, "verbose", false, "stream subprocess output")
	flags.BoolVar(&opts.dryRun, "n", false, "report what would change, write nothing")
	if err := flags.Parse(args); err != nil {
		return err
	}

	instance, err := build(opts)
	if err != nil {
		return err
	}

	ctx := context.Background()
	switch command {
	case "menu":
		return runMenu(ctx, instance, opts)
	case "install":
		return instance.Install(ctx)
	case "adopt":
		return instance.Adopt(ctx)
	case "check":
		return instance.Check(opts.strict)
	case "link":
		instance.Report.Phase("configs")
		return instance.Link()
	case "unlink":
		instance.Report.Phase("configs")
		return instance.Unlink(opts.restore)
	case "sync":
		return instance.Sync(ctx)
	case "update":
		return instance.Update(ctx)
	case "secrets":
		instance.Report.Phase("secrets")
		return instance.Secrets(ctx)
	case "upgrade":
		return instance.SelfUpdate(ctx, version)
	case "validate":
		return instance.Validate()
	case "packages":
		out, err := instance.PackageLists(opts.brew, opts.winget)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	case "preview":
		return preview(ctx, instance, opts)
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", command)
	}
}

// build assembles the App, including the two decisions that belong here and
// nowhere else: how to render, and where subprocess output goes.
func build(opts options) (*app.App, error) {
	interactive := canPrompt(os.Stdout, os.Stdin)
	report := reporter(os.Stdout)
	runner := pkgs.ExecRunner{}
	if opts.verbose {
		// Streamed rather than captured, which also means no spinner: the two
		// cannot share a line.
		runner.Out = os.Stdout
	}

	instance, err := app.New(opts.repo, report, runner)
	if err != nil {
		return nil, err
	}
	instance.RepoURL = app.RepoURL(opts.url)
	instance.DryRun = opts.dryRun
	instance.Only = opts.only
	instance.Tools = opts.tools
	instance.Interactive = interactive
	return instance, nil
}

// isTerminal asks the kernel, via the same predicate Bubble Tea uses to
// decide whether it can drive a screen.
//
// The cheap version of this check - Stat().Mode()&os.ModeCharDevice - has a
// hole worth naming, because it is the version that shipped: /dev/null is a
// character device. `doti install </dev/null`, the shape every CI job takes,
// read as "a person is watching" and went looking for a password prompt.
// Matching Bubble Tea's own call also means canPrompt cannot disagree with
// whether the menu will actually run.
func isTerminal(f *os.File) bool {
	return f != nil && term.IsTerminal(f.Fd())
}

// canPrompt reports whether doti may stop and ask a question.
//
// Two streams, because they answer two different questions and conflating
// them is the bug this exists for: stdout being a terminal decides how to
// *render*, stdin being one decides whether anything can be *asked*. Piped
// into bash - `curl ... | bash` - stdout is still the terminal while stdin is
// the exhausted download, so the old one-stream test sent an unattended
// install into `bw unlock` and the prompt read from a closed pipe.
func canPrompt(stdout, stdin *os.File) bool {
	return isTerminal(stdout) && isTerminal(stdin)
}

// reporter picks the rendering from whether anything is watching.
//
// A terminal gets colour and a spinner. A pipe, a file or CI gets plain
// lines, because cursor movement in a log is noise and a spinner is thousands
// of wasted rows. Same events either way, so there is no second code path.
func reporter(out *os.File) app.Reporter {
	if !isTerminal(out) {
		return app.PlainReporter{Out: out}
	}
	// Honour the convention rather than inventing one: NO_COLOR is set by
	// people who mean it.
	if os.Getenv("NO_COLOR") != "" {
		return app.PlainReporter{Out: out}
	}
	return app.NewLiveReporter(out)
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

// runMenu shows the menu, then runs whatever was chosen - through exactly the
// same App methods the direct commands use.
func runMenu(ctx context.Context, instance *app.App, opts options) error {
	if !instance.Cloned() {
		// There is nothing to show a selector about yet. Offering an empty
		// menu would be worse than doing the obvious thing.
		return instance.Install(ctx)
	}
	if !instance.Interactive {
		// Bubble Tea would take the alt screen and then wait for keys that
		// cannot arrive. Naming the commands is the only useful thing to say.
		return fmt.Errorf("the menu needs a terminal to drive it; " +
			"run `doti install`, `doti check` or `doti --help` instead")
	}
	items, err := instance.MenuItems()
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
		return instance.Install(ctx)
	case tui.ActionAdopt:
		if !chosen.Confirmed() {
			return nil
		}
		return instance.Adopt(ctx)
	case tui.ActionPreview:
		instance.DryRun = true
		return instance.Install(ctx)
	case tui.ActionUnlink:
		instance.Report.Phase("configs")
		return instance.Unlink(false)
	case tui.ActionCheck:
		return instance.Check(false)
	case tui.ActionSync:
		return instance.Sync(ctx)
	case tui.ActionUpdate:
		return instance.Update(ctx)
	}
	return nil
}

func preview(ctx context.Context, instance *app.App, opts options) error {
	if opts.frames == "" {
		return runMenu(ctx, instance, opts)
	}
	items, err := instance.MenuItems()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(opts.frames, 0o755); err != nil {
		return err
	}
	for i, frame := range tui.Frames(items, version, 80, 26) {
		path := filepath.Join(opts.frames, fmt.Sprintf("%02d-%s.ansi", i+1, frame.Name))
		if err := os.WriteFile(path, []byte(frame.Body), 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", path)
	}
	return nil
}
