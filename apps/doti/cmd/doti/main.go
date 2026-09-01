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
	"strings"

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
  doti                      the window: pick an operation and watch it run
  doti install              clone if needed, then packages, configs, secrets
  doti adopt                scan first, then act only on the gaps
  doti check                verify tools and symlinks; changes nothing
  doti link                 link configs into $HOME
  doti unlink               remove the links this repo owns
  doti uninstall            remove tools or MCP servers - names only, never all
  doti sync                 git pull --ff-only, then re-link
  doti update               upgrade installed packages
  doti secrets              render secret files from Bitwarden
  doti upgrade              replace this binary with the newest release
  doti packages             print the generated package lists
  doti validate             parse and check manifest.jsonc
  doti preview              open the window, or --frames DIR to dump screens
  doti version

flags:
  --repo DIR    dotfiles checkout (default $DOTFILES_DIR, else ~/dotfiles)
  --url URL     install: clone from here (default $DOTFILES_REPO_URL)
  --only PKG    link/unlink: act on this stow package alone
  --tools LIST  install: only these missing tools; uninstall: these to remove
  --term        print lines instead of drawing the window
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
	// term prints lines instead of drawing the window.
	//
	// The window is the default, because when somebody is watching it is
	// strictly more informative. This is the escape hatch for when lines are
	// the right answer: output that has to land in the scrollback as it
	// happens, or a terminal doing something the alt screen does not survive.
	//
	// A pipe, a file and CI do not need it - wantsWindow works that out from
	// the streams, the same way the reporter always has.
	//
	// It replaced --tui, which had this the other way round because the window
	// could not own the vault's password prompt yet. It can.
	term bool
}

// include is the component list an operation acts on, taken from the flags.
//
// Only a removal takes one. Everywhere else an empty Include means everything,
// which is what a command line wants; for `uninstall` an empty one means
// nothing, and --tools is the only way to name something. There is deliberately
// no flag that means "all of them".
func (o options) include(op app.Operation) []app.Ref {
	if op != app.OpRemovePackages {
		return nil
	}
	// Unqualified: `--tools jq` names whatever jq turns out to be, and a
	// removal list holds nothing whose name means two things.
	return app.Refs(app.SplitList(o.tools))
}

// wantsHelp reports whether this invocation is a request for the usage text.
//
// Both spellings of the flag anywhere in the arguments, plus the bare word:
// `doti help`, `doti --help`, `doti install -h`. A person reaching for any of
// them wants the same paragraph.
func wantsHelp(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-h", "--help", "help":
			return true
		}
	}
	return false
}

// wantsVersion is the same for the version, which is the other thing that has
// to answer before a repository is looked for: `doti --version` on a machine
// with no dotfiles checkout should still say what it is.
func wantsVersion(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-v", "--version", "version":
			return true
		}
	}
	return false
}

func run(args []string) error {
	// No arguments is the menu. That is the shape the shell installer had and
	// the one people's hands know.
	//
	// A leading flag is a flag, not a command: `doti --repo ~/dotfiles` used to
	// take "--repo" as the command name, fail to match anything, and print the
	// usage - which reads as "that flag does not exist" rather than "name a
	// command first".
	// Asked before anything else, because both spellings have to work and one
	// of them is a flag: after the leading-flag rule below, `doti --help` is
	// not a command at all, and the flag package answered it with its own
	// usage and a non-zero exit.
	if wantsHelp(args) {
		fmt.Print(usage)
		return nil
	}
	if wantsVersion(args) {
		fmt.Println(version)
		return nil
	}

	command := "menu"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command = args[0]
		args = args[1:]
	}

	var opts options
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	// One usage text, so `doti --help` and a mistyped flag both print the one
	// that names the commands rather than the one that lists only flags.
	flags.Usage = func() { fmt.Fprint(os.Stderr, usage) }
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
	flags.BoolVar(&opts.term, "term", false, "print lines instead of drawing the window")
	if err := flags.Parse(args); err != nil {
		return err
	}

	instance, err := build(opts)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// The operations, which are one table rather than a switch: package app
	// owns what each one does, this owns only the two names it answers to.
	// `doti install` and the window's Install reach the same App.Do.
	window := wantsWindow(opts, instance.Interactive)
	if op, ok := operations[command]; ok {
		if window {
			return runWindow(ctx, instance, opts, op)
		}
		return instance.Do(ctx, op, opts.include(op), version)
	}

	switch command {
	case "menu":
		return runWindow(ctx, instance, opts, "")
	case "check":
		// Not in the table: --strict is this command's alone, and folding it
		// in would mean Do carrying a flag only one caller sets.
		if window && !opts.strict {
			return runWindow(ctx, instance, opts, app.OpCheck)
		}
		return instance.Check(opts.strict)
	case "link":
		instance.Report.Phase("configs")
		return instance.Link()
	case "unlink":
		// Same: --restore belongs to the command.
		if opts.restore {
			instance.Report.Phase("configs")
			return instance.Unlink(true)
		}
		if window {
			return runWindow(ctx, instance, opts, app.OpUnlink)
		}
		return instance.Do(ctx, app.OpUnlink, nil, version)
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
func reporter(out *os.File) app.Reporter { return reporterFor(out, isTerminal(out)) }

// reporterFor is the rule, with the terminal question asked rather than
// answered - so both branches are reachable from a test. Making os.Stdout a
// terminal from inside `go test` is not a thing; deciding what to do when it is
// one is.
func reporterFor(out *os.File, terminal bool) app.Reporter {
	if !terminal {
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

func preview(ctx context.Context, instance *app.App, opts options) error {
	if opts.frames == "" {
		// No directory to write to, so this is "show me what would change".
		// With --term, or with nothing watching, that is a dry-run install
		// printed as lines - which is exactly what the window's Preview runs.
		// It used to demand a window either way and refuse when there was none.
		if !wantsWindow(opts, instance.Interactive) {
			return instance.Do(ctx, app.OpPreview, nil, version)
		}
		return runWindow(ctx, instance, opts, "")
	}
	components, err := instance.MenuItems(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(opts.frames, 0o755); err != nil {
		return err
	}
	for i, frame := range tui.Frames(components, version, 80, 26) {
		path := filepath.Join(opts.frames, fmt.Sprintf("%02d-%s.ansi", i+1, frame.Name))
		if err := os.WriteFile(path, []byte(frame.Body), 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", path)
	}
	return nil
}
