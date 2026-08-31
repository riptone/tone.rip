package main

import (
	"context"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/apps/doti/internal/tui"
	"github.com/riptone/tone.rip/packages/gotui"
)

// Opening the window, and the one table that says which command is which
// operation.
//
// This file is the whole of what package main knows about running work: build
// the App, hand the window a function that calls App.Do, and get out of the
// way. It used to be a second switch over the window's chosen Action, sitting
// next to the first one over the command name - which is how the selector's
// ticked components came to be dropped: only one of the two switches had
// anywhere to put them.

// operations maps a command name to the operation behind it.
//
// A table rather than cases, so `doti install`, `doti install --term` and the
// window's Install cannot drift: there is one name, one operation, one call.
// `check` and `unlink` are absent on purpose - each has a flag that is the
// command's own, and folding those in would mean App.Do carrying an argument
// only one caller ever sets.
var operations = map[string]app.Operation{
	"install": app.OpInstall,
	"adopt":   app.OpAdopt,
	"sync":    app.OpSync,
	"update":  app.OpUpdate,
	"secrets": app.OpSecrets,
	// Distinct from `unlink`, which removes symlinks and leaves the software.
	"uninstall": app.OpRemovePackages,
	"upgrade":   app.OpSelfUpdate,
}

// runWindow draws the window.
//
// start is the operation to open on, or "" for the menu.
func runWindow(ctx context.Context, instance *app.App, opts options, start app.Operation) error {
	if start == "" && !instance.Cloned() {
		// There is nothing to show a menu about yet. Offering an empty one
		// would be worse than doing the obvious thing.
		return instance.Install(ctx)
	}
	if !instance.Interactive {
		// Bubble Tea would take the alt screen and then wait for keys that
		// cannot arrive. Only reachable when something asked for the window
		// explicitly: without that, wantsWindow has already sent this run
		// down the plain path.
		return fmt.Errorf("the window needs a terminal to drive it; " +
			"add --term to print lines instead, or see `doti --help`")
	}

	scan := inventoryScanner(instance)
	var inventory tui.Inventory
	if instance.Cloned() {
		var err error
		if inventory, err = scan(ctx); err != nil {
			return err
		}
	}

	// The renderer and the terminal's own colours, both of which apps/ssh-cv
	// had and this did not - which is the whole of why the same black card came
	// out sitting on the reader's theme. gotui owns the policy for both now.
	renderer := gotui.LocalRenderer(os.Stdout)
	defer gotui.PaintTerminal(os.Stdout)()

	model := tui.New(tui.Config{
		Components: inventory.Components,
		Removable:  inventory.Removable,
		Scan:       scan,
		Version:    version,
		Renderer:   renderer,
		Run:        operationRunner(instance, opts),
		Check:      updateChecker(app.Releases{}),
		Start:      tui.Action(start),
	})

	final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return fmt.Errorf("running the window: %w", err)
	}
	done, ok := final.(tui.Model)
	if !ok {
		return nil
	}
	// A window that stood in for a command leaves the command's output behind.
	//
	// The alt screen is discarded on exit, so an install run inside one would
	// vanish the moment it finished - and the first thing anybody does with a
	// failed install is scroll back through it. Replayed through the plain
	// reporter, so what lands in the scrollback is exactly what `--term` would
	// have printed. Only for a launched run: quitting a menu you were browsing
	// should not fill the terminal.
	if done.Launched() {
		replay(done.Transcript(), os.Stdout)
	}
	if done.Restart() {
		return relaunch()
	}
	// The screen said "failed", so the shell hears it too. A window that shows
	// a warning line and exits 0 lies to whatever started it.
	return done.Err()
}

// operationRunner is the window's one way to do work.
func operationRunner(instance *app.App, opts options) tui.RunFunc {
	return func(ctx context.Context, action tui.Action, chosen []string, run tui.RunOptions) error {
		// A copy per run. The window can do several in one session, and
		// Preview sets DryRun - which must not still be set when the reader
		// goes back to the menu and picks Install.
		each := *instance
		// A fresh read of the checkout: the operation before this one may have
		// been a sync, and a pull can change the manifest.
		each.Forget()
		each.Report = run.Report
		each.DryRun = opts.dryRun
		// Prompting is on: the window hands the terminal back to `bw` for as
		// long as it needs it, so the vault is no longer a command-line job.
		// It used to be off here, and the secrets phase deferred with an
		// actionable line - which was honest and still a dead end.
		each.Interactive = true
		each.Vault = run.Vault
		// A selection supersedes --only. The selector deliberately offers every
		// component regardless of the flag, on the grounds that picking is what
		// it is for - but the run still honoured --only, so `doti --only zsh`
		// let you tick ghostty and then quietly did nothing about it. Whichever
		// question was asked last is the one that gets answered.
		if len(chosen) > 0 {
			each.Only = ""
		}
		return each.Do(ctx, app.Operation(action), chosen, version)
	}
}

// updateChecker asks whether there is a newer release than this binary.
//
// Takes the Releases rather than building one, so a test can point it at a
// server instead of at GitHub.
//
// Returns "" rather than an error for "nothing newer", because the caller
// treats both the same way: no footer offer. A failure is silence - a menu
// that shouts about DNS is worse than one that never mentions updates.
func updateChecker(releases app.Releases) tui.CheckFunc {
	return func(ctx context.Context) (string, error) {
		latest, err := releases.Latest(ctx)
		if err != nil {
			return "", err
		}
		if !app.Newer(version, latest) {
			return "", nil
		}
		return latest, nil
	}
}

// replay writes a finished run's events out as lines.
//
// Through the same PlainReporter the plain path uses, rather than a second
// renderer that agrees with it.
func replay(records []app.Record, out io.Writer) {
	if len(records) == 0 {
		return
	}
	reporter := app.PlainReporter{Out: out}
	for _, record := range records {
		switch record.Kind {
		case "phase":
			reporter.Phase(record.Text)
		case "summary":
			reporter.Summary(record.Text)
		default:
			reporter.Line(record.Mark, record.Text)
		}
	}
}

// wantsWindow decides between the window and plain lines.
//
// The window is the default now, because it is strictly more informative when
// somebody is watching: the log scrolls, the spinner says a slow step is not a
// hang, and the footer says how it ended. What replaced --tui is --term, for
// the cases where lines are the right answer:
//
//   - a pipe, a file or CI, which is decided here rather than asked for - an
//     alt screen in a log is thousands of cursor movements and no output;
//   - anything that wants the output in its scrollback as it happens rather
//     than replayed at the end.
//
// The tty test reads both streams, for the reason canPrompt does: piped into
// bash, stdout is still the terminal while stdin is the exhausted download, and
// a window nobody can send keys to is worse than lines.
func wantsWindow(opts options, interactive bool) bool {
	if opts.term {
		return false
	}
	return interactive
}

// inventoryScanner reads the machine for the selectors.
//
// The same function the window opens with and re-runs after every operation, so
// what a selector shows after a removal is the state the removal left behind
// rather than the state it started from. It used to be called once and the
// result kept for the life of the window.
func inventoryScanner(instance *app.App) tui.ScanFunc {
	return func(context.Context) (tui.Inventory, error) {
		// A copy, and a fresh manifest read: this runs after an operation that
		// may have set DryRun or narrowed Include on its own copy, and neither
		// belongs in a description of the machine.
		each := *instance
		each.DryRun = false
		each.Include = nil
		each.Forget()

		components, err := each.MenuItems()
		if err != nil {
			return tui.Inventory{}, err
		}
		removable, err := each.Removable()
		if err != nil {
			return tui.Inventory{}, err
		}
		return tui.Inventory{Components: components, Removable: removable}, nil
	}
}
