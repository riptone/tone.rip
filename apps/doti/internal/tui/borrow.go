package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/riptone/tone.rip/apps/doti/internal/secrets"
)

// Handing the terminal back for as long as another program needs it.
//
// `bw` owns its own password prompt: doti inherits stdin and stderr so the
// master password is typed straight into bw and is never in doti's argv, its
// memory or its errors. Inside a window there is no terminal to inherit - the
// alt screen has it - which is why the secrets phase used to defer here and say
// "run this from a shell instead".
//
// tea.Exec is the way out. It suspends the program, restores the terminal to
// the state bw expects, runs the command, and resumes. What it does not do is
// let a *command* ask for that: tea.Exec is a tea.Cmd, and only Update may
// return one. So the operation goroutine sends a request and blocks; the model
// picks it up, returns the tea.Exec, and the callback hands the result back.

// errWindowClosed is what a borrow gets when there is no longer a window to
// borrow from - the reader quit, or cancelled the run.
var errWindowClosed = errors.New("the window closed before the terminal could be handed over")

// borrow is one request for the terminal.
type borrow struct {
	env   []string
	args  []string
	reply chan borrowResult
}

// borrowResult is what the command wrote to stdout, and how it ended.
type borrowResult struct {
	stdout []byte
	err    error
}

// borrowRunner is a secrets.Runner that asks the window for the terminal
// whenever `bw` needs to talk to a person.
//
// The non-interactive half is delegated rather than reimplemented: `bw status`,
// `bw sync` and `bw get` want no terminal at all, and routing them through the
// window would suspend it for every one of them.
type borrowRunner struct {
	plain    secrets.Runner
	requests chan borrow
	done     <-chan struct{}
}

func newBorrowRunner(requests chan borrow, done <-chan struct{}) borrowRunner {
	return borrowRunner{plain: secrets.ExecRunner{}, requests: requests, done: done}
}

func (r borrowRunner) Run(ctx context.Context, env []string, args ...string) ([]byte, error) {
	return r.plain.Run(ctx, env, args...)
}

// RunInteractive parks the operation until the window has run the command.
//
// Every wait is guarded by both the window closing and the context being
// cancelled, because this blocks a goroutine that is holding up an install: a
// reader who presses ctrl+c during a vault prompt should get their program
// back, not a deadlock.
func (r borrowRunner) RunInteractive(ctx context.Context, env []string, args ...string) ([]byte, error) {
	reply := make(chan borrowResult, 1)
	select {
	case r.requests <- borrow{env: env, args: args, reply: reply}:
	case <-r.done:
		return nil, errWindowClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case result := <-reply:
		return result.stdout, result.err
	case <-r.done:
		return nil, errWindowClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// borrowedCommand runs one command with the terminal, keeping its stdout.
//
// tea.Exec points all three streams at the terminal, and for an editor that is
// exactly right. Here it is not: `bw unlock --raw` writes its "Master
// password:" prompt to *stderr* and the session key to *stdout*, so stdout has
// to stay captured or the key is printed to the screen and lost. Checked by
// looking rather than assumed - it is the same split ExecRunner relies on.
type borrowedCommand struct {
	cmd *exec.Cmd
	out bytes.Buffer
}

func newBorrowedCommand(ctx context.Context, bin string, req borrow) *borrowedCommand {
	cmd := exec.CommandContext(ctx, bin, req.args...)
	cmd.Env = req.env
	c := &borrowedCommand{cmd: cmd}
	cmd.Stdout = &c.out
	return c
}

func (c *borrowedCommand) Run() error            { return c.cmd.Run() }
func (c *borrowedCommand) SetStdin(r io.Reader)  { c.cmd.Stdin = r }
func (c *borrowedCommand) SetStderr(w io.Writer) { c.cmd.Stderr = w }

// SetStdout is deliberately empty: see borrowedCommand.
func (c *borrowedCommand) SetStdout(io.Writer) {}

// borrowMsg is a request that reached the model.
type borrowMsg struct{ req borrow }

// borrowDoneMsg re-arms the waiter. Nothing else asks for the next request.
type borrowDoneMsg struct{}

// waitBorrow parks one command goroutine on the next request for the terminal.
//
// The mirror of waitEvent, and closed the same way: the operation closes the
// channel when it returns, and a closed channel ends the wait rather than
// leaking the goroutine.
func waitBorrow(j *job) tea.Cmd {
	if j == nil {
		return nil
	}
	return func() tea.Msg {
		req, ok := <-j.borrows
		if !ok {
			return nil
		}
		return borrowMsg{req: req}
	}
}

// describeBorrow is what the log says while the terminal is elsewhere.
//
// The subcommand only. `bw` arguments are not secret today, but a line that
// prints whatever was passed is one flag away from printing something that is.
func describeBorrow(args []string) string {
	if len(args) == 0 {
		return "bw"
	}
	return "bw " + args[0]
}

// vaultBin is the binary a borrow runs. A variable so a test can point it at
// something that is not a password prompt.
var vaultBin = "bw"
