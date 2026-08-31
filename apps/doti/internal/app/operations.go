package app

import "context"

// The one place an operation's name becomes a call.
//
// There used to be two switches on it: one in package main for the commands,
// one in main's menu handler for what the window had chosen. They agreed, which
// is not the same as being the same thing - and the second one quietly dropped
// the components the selector had ticked, because only the first one had
// anywhere to put them.
//
// Everything arrives here now: `doti install`, `doti install --term`, and the
// window's Install. Adding an operation is one case in one switch, and the
// window picks it up from the menu table without another.

// Operation names one thing doti can do.
type Operation string

const (
	OpInstall    Operation = "install"
	OpUnlink     Operation = "unlink"
	OpAdopt      Operation = "adopt"
	OpPreview    Operation = "preview"
	OpCheck      Operation = "check"
	OpSync       Operation = "sync"
	OpUpdate     Operation = "update"
	OpSecrets    Operation = "secrets"
	OpSelfUpdate Operation = "self-update"
)

// Do performs one operation.
//
// include narrows it to the named components, as the selector's labels; empty
// means everything. version is what a self-update compares against, and is
// ignored by everything else.
func (a *App) Do(ctx context.Context, op Operation, include []string, version string) error {
	// Set on a copy of nothing - App is a pointer and this is the field the
	// narrowing lives in, so it is assigned once here rather than threaded
	// through nine signatures.
	a.Include = include

	switch op {
	case OpInstall:
		return a.Install(ctx)
	case OpAdopt:
		return a.Adopt(ctx)
	case OpPreview:
		// Preview *is* install, with nothing written. Two names for one path
		// rather than a second path that reports what the first would do.
		a.DryRun = true
		return a.Install(ctx)
	case OpUnlink:
		a.Report.Phase("configs")
		return a.Unlink(false)
	case OpCheck:
		return a.Check(false)
	case OpSync:
		return a.Sync(ctx)
	case OpUpdate:
		return a.Update(ctx)
	case OpSecrets:
		a.Report.Phase("secrets")
		return a.Secrets(ctx)
	case OpSelfUpdate:
		return a.SelfUpdate(ctx, version)
	}
	return &UnknownOperationError{Op: op}
}

// UnknownOperationError is a name nothing answers to.
type UnknownOperationError struct{ Op Operation }

func (e *UnknownOperationError) Error() string {
	return "unknown operation " + string(e.Op)
}
