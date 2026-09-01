// Package tui is doti's window: the menu, the component selector, and the
// screen an operation runs inside.
//
// It depends on internal/app and internal/app does not depend on it. That
// direction is what makes running an operation *in* the window possible at all:
// the app reports into a channel and this package turns the channel into a
// screen, where before the window quit and handed an Action back to main to
// print with.
//
//	model.go         state, routing, and which key goes where
//	screen_menu.go   the list of operations
//	screen_select.go the per-component toggles
//	screen_run.go    an operation's live output, as a screen
//	run.go           the job behind it, and the model's side of one
//	update.go        the release check and what the footer does with it
//	theme.go         the content styles
//	window.go        how big the card gets - the frame itself is gotui's
//	keys.go          the bindings, navigation shared with apps/ssh-cv
package tui

import (
	"context"
	"errors"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/apps/doti/internal/secrets"
	"github.com/riptone/tone.rip/packages/gotui"
)

// Screen is which view is on top.
type Screen int

const (
	// ScreenMenu is the list of operations.
	ScreenMenu Screen = iota
	// ScreenSelect is the per-component toggle list reached from Install.
	ScreenSelect
	// ScreenRun is an operation's live output.
	ScreenRun
	// ScreenHelp is the keys and what the operations do.
	ScreenHelp
)

// RunFunc performs one operation, reporting progress into r.
//
// Called on a goroutine Bubble Tea owns, so it must not touch the model:
// reporting is the only channel back, which is exactly the constraint the
// Reporter interface already imposed on every command. main wires this to
// internal/app; a test wires a fake and drives the whole screen without a
// machine to install onto.
type RunFunc func(ctx context.Context, action Action, chosen []app.Ref, opts RunOptions) error

// RunOptions is what the window hands an operation: where to report, and the
// `bw` runner that borrows the terminal back when the vault needs a person.
//
// A struct rather than more parameters, because a fifth positional argument is
// where signatures stop being readable - and because the vault runner is
// optional in a way the reporter is not.
type RunOptions struct {
	Report app.Reporter
	// Vault runs the `bw` CLI. Nil means the caller does not want the window
	// involved, which is the shape a test uses.
	Vault secrets.Runner
}

// CheckFunc asks whether there is a release newer than the one running.
type CheckFunc func(ctx context.Context) (string, error)

// Config builds a model.
type Config struct {
	// Components is what the selector offers for an install.
	Components []app.Component
	// Removable is what it offers for a removal: only the tools this repository
	// installed, only the ones still present, and none of them ticked.
	Removable []app.Component
	// Version is shown in the title bar, like ssh-cv shows the language.
	Version string
	// Width and Height seed the layout before the first resize arrives.
	Width  int
	Height int
	// Renderer decides what colour is allowed. Nil means lipgloss's default,
	// which is right only in a test - see gotui.LocalRenderer.
	Renderer *lipgloss.Renderer

	Run   RunFunc
	Check CheckFunc
	// Scan re-reads the machine after a run has changed it. Nil leaves the
	// lists as they were.
	Scan ScanFunc

	// Start is an operation to run immediately instead of showing the menu.
	//
	// This is `doti install` in a terminal: the window opens on the run
	// screen, and finishing leaves rather than going back to a menu the
	// reader never asked for.
	Start Action
	// StartChosen narrows Start the way the selector would have.
	StartChosen []app.Ref
	// StartAsks opens Start's selector instead of running it.
	//
	// The difference between "the reader named this operation" and "this is the
	// only operation worth opening on". `doti install` in a terminal means run
	// it, and does. Bare `doti` on a machine with no checkout means show me the
	// install screen - it used to mean clone the repository and set the whole
	// machine up, with nothing between the reader and all of it.
	StartAsks bool
}

// errStopped is what a run the reader cancelled reports to the shell, when the
// operation itself returned nothing.
var errStopped = errors.New("stopped before it finished")

// Model is the whole UI.
type Model struct {
	styles styles
	keys   keymap
	cfg    Config

	screen Screen
	width  int
	height int

	menuAt int
	// components and removable are what the selectors are built from, and are
	// replaced by a re-scan after every run.
	components []app.Component
	removable  []app.Component
	// items is the working copy the open selector is toggling.
	items  []app.Component
	itemAt int
	// notice is a one-line answer to a key that did not do what the reader
	// expected, shown at the top of the selector. Cleared by the next key.
	notice string
	// folded says which parents are closed. Rebuilt into rows on every change,
	// and copied before every write - see Model.fold.
	folded map[string]bool
	rows   []row

	// update is the newest release once the check has answered, and "" until
	// then or if it never does.
	update string
	// replaced is the version a self-update put on disk.
	//
	// Separate from update, and it replaces it: once the binary has been
	// swapped, offering to install that version again is an offer to re-run
	// the whole installer for nothing. What is left to do is restart.
	replaced string

	run  runState
	help helpState

	// launched is true when the window was opened on one operation rather
	// than on the menu. Finishing that operation leaves.
	launched bool

	// startCmd is the launched operation's command, held from New until Init
	// is asked for it.
	startCmd tea.Cmd

	quit bool
	// restart asks main to exec the replacement binary, which is the only
	// useful thing to do after a self-update.
	restart bool
}

// New builds the model.
func New(cfg Config) Model {
	m := Model{
		styles:     newStyles(cfg.Renderer),
		keys:       newKeymap(),
		cfg:        cfg,
		components: cfg.Components,
		removable:  cfg.Removable,
		items:      append([]app.Component(nil), cfg.Components...),
		folded:     foldedByDefault(cfg.Components),
		width:      cfg.Width,
		height:     cfg.Height,
	}
	m.rows = flatten(m.items, m.folded)
	m.run.spin = newSpinner(m.styles)

	// The launch happens here rather than in Init, which was a bug worth
	// naming: Init returns commands, not a model, so a launch performed there
	// built the run screen and threw it away. Bubble Tea went on rendering the
	// menu, and the events arrived at a model whose job was nil - so the
	// stream stopped after the first line.
	if cfg.Start != "" {
		if at, ok := menuIndex(cfg.Start); ok && cfg.StartAsks {
			// menuAt as well as the screen, so esc goes back to the entry this
			// opened and the enter handler reads the right op.
			m.menuAt = at
			return m.openSelector(menu[at])
		}
		started, cmd := m.begin(cfg.Start, cfg.StartChosen)
		started.startCmd = cmd
		return started
	}
	return m
}

// row is one rendered line of the select screen: either a group heading or an
// item. Flattened once so the cursor moves over items only.
type row struct {
	heading string
	item    int
}

// flatten turns the components into the rows the screen draws: a heading
// whenever the group changes, then one row per item that is not folded away.
//
// The heading is emitted before the fold test, so a group whose every member is
// a folded child keeps its heading - and after it, an item hidden under a closed
// parent is simply not a row. That is what keeps the cursor honest: it moves
// over rows, and a row is by definition something on screen.
func flatten(items []app.Component, folded map[string]bool) []row {
	var rows []row
	group := ""
	for i, item := range items {
		if item.Group != group {
			group = item.Group
			rows = append(rows, row{heading: group, item: -1})
		}
		if item.Parent != "" && folded[item.Parent] {
			continue
		}
		rows = append(rows, row{item: i})
	}
	return rows
}

// Chosen is what is left ticked, as references the app can act on.
//
// Qualified, because the labels collide: `git` is both a tool the manifest
// installs and a stow package it links, and a flat list of names could not say
// which of the two a tick meant. Folding a parent does not change this - a
// hidden child is hidden, not unticked.
func (m Model) Chosen() []app.Ref {
	var out []app.Ref
	for _, item := range m.items {
		if item.Selected {
			out = append(out, app.Ref{Kind: item.Kind, Label: item.Label})
		}
	}
	return out
}

// Restart reports whether the reader asked to relaunch after a self-update.
func (m Model) Restart() bool { return m.restart }

// Launched reports whether the window opened on one operation rather than on
// the menu - which is `doti install` in a terminal, and the case where the run's
// output belongs in the scrollback after the alt screen is gone.
func (m Model) Launched() bool { return m.launched }

// Transcript is the last run's events, in order.
//
// For replaying into a plain reporter once the window has closed: the alt screen
// is discarded on exit, and an install that vanishes the moment it finishes is
// one nobody can scroll back through. Records rather than rendered rows, so what
// lands in the scrollback is what the plain path would have printed rather than
// a screenshot of a card.
func (m Model) Transcript() []app.Record {
	out := make([]app.Record, 0, len(m.run.lines))
	for _, line := range m.run.lines {
		out = append(out, app.Record{Kind: line.kind, Mark: line.mark, Text: line.text})
	}
	return out
}

// Err is the failure of the last operation, if it failed.
//
// Read by main so the process exit code says what the screen said. A window
// that showed a red line and exited 0 lies to the shell it was started from -
// and one that showed "interrupted" and exited 0 tells the same lie, because
// half an install is not a successful one. Whether a cancelled operation
// returned an error of its own is an accident of where it was, so the stop is
// what this answers to.
func (m Model) Err() error {
	if m.run.stopped && m.run.err == nil {
		return errStopped
	}
	return m.run.err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.startCmd, checkUpdate(m.cfg.Check))
}

// Update handles one message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.resize(), nil
	case updateFoundMsg:
		m.update = string(msg)
		return m, nil
	case inventoryMsg:
		return m.adopt(Inventory(msg)), nil
	case eventMsg:
		return m.event(app.Record(msg))
	case borrowMsg:
		return m.borrow(msg.req)
	case borrowDoneMsg:
		return m, waitBorrow(m.run.job)
	case streamDoneMsg:
		m.run.drained = true
		return m.afterRun()
	case finishedMsg:
		m.run.finished = true
		if msg.err != nil {
			m.run.err = msg.err
			// The footer said "failed" and nothing said why. An operation that
			// returns an error has usually not reported it as a line - the
			// error *is* how it reports - so it goes into the log, where the
			// reader is already looking.
			m = m.appendLine(app.MarkWarn, msg.err.Error())
		}
		return m.afterRun()
	case spinner.TickMsg:
		// Only while something is actually running: a spinner ticking behind a
		// settled screen is a redraw of the whole card, eight times a second,
		// for nothing.
		if !m.spinning() {
			return m, nil
		}
		var cmd tea.Cmd
		m.run.spin, cmd = m.run.spin.Update(msg)
		// And the new frame has to reach the viewport, or the spinner advances
		// in a model nothing renders and the line on screen sits still.
		if m.run.working != "" {
			m = m.flow()
		}
		return m, cmd
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

// key dispatches one keypress: the two bindings that mean the same thing on
// every screen first, then the screen's own.
func (m Model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// esc is bound to both Back and Quit, and which one it means depends on
	// whether there is anywhere to go back to - the rule apps/ssh-cv follows.
	// Checked first for that reason: losing the program because somebody wanted
	// to close a screen would be rude, and on the menu there is nothing behind
	// it to close.
	goingBack := m.screen != ScreenMenu && key.Matches(msg, m.keys.Back)

	// Quit is not offered while an operation is running: stopping half way
	// through a link pass is a decision, and ctrl+c is how it is made.
	if !goingBack && key.Matches(msg, m.keys.Quit) && !m.spinning() {
		m.quit = true
		m.run.job.stop()
		return m, tea.Quit
	}

	// Help is a detour from anywhere nothing is running, and returns to where
	// it was asked for.
	if key.Matches(msg, m.keys.Help) && m.screen != ScreenHelp && !m.spinning() {
		return m.openHelp(), nil
	}

	switch m.screen {
	case ScreenMenu:
		return m.menuKey(msg)
	case ScreenSelect:
		return m.selectKey(msg)
	case ScreenHelp:
		return m.helpKey(msg)
	default:
		return m.runKey(msg)
	}
}

// View renders the current screen.
func (m Model) View() string {
	if m.quit {
		return ""
	}
	var out string
	switch m.screen {
	case ScreenMenu:
		out = m.viewMenu()
	case ScreenSelect:
		out = m.viewSelect()
	case ScreenHelp:
		out = m.viewHelp()
	default:
		out = m.viewRun()
	}
	// The backstop under the arithmetic: a card one row too tall scrolls its
	// own title away, which is the most visible way a TUI can look broken.
	//
	// Clamped against the size the geometry resolved rather than the raw
	// fields. Before the first WindowSizeMsg both are zero - the geometry
	// substitutes the terminal every terminal claims to be, and clamping to
	// max(0,1) instead crushed the entire card into one cell for that frame.
	// The terminal is the terminal whichever card is drawn in it; either spec
	// resolves the same fallback when the size is not known yet.
	g := geometryFor(m.width, m.height)
	return gotui.Clamp(out, g.TermWidth, g.TermHeight)
}

func (m Model) name() string {
	if m.cfg.Version == "" {
		return "doti"
	}
	return "doti · " + m.cfg.Version
}
