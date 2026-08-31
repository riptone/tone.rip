package tui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/apps/doti/internal/secrets"
)

// The mechanism that lets the window own a password prompt: an operation asks
// for the terminal, the model hands it over, and the answer comes back.

// runtime stands in for what Bubble Tea does with a borrow - pick the request
// off the channel, run it, answer it - so the contract can be tested with a
// real subprocess and no program.
func runtime(t *testing.T, j *job, bin string) {
	t.Helper()
	go func() {
		for req := range j.borrows {
			command := newBorrowedCommand(context.Background(), bin, req)
			command.SetStdin(strings.NewReader(""))
			command.SetStderr(&bytes.Buffer{})
			err := command.Run()
			req.reply <- borrowResult{stdout: command.out.Bytes(), err: err}
		}
	}()
}

func newJob() *job {
	return &job{
		events:  make(chan app.Record, 8),
		borrows: make(chan borrow),
		done:    make(chan struct{}),
		cancel:  func() {},
	}
}

func TestABorrowGetsTheCommandsStdoutBack(t *testing.T) {
	j := newJob()
	defer j.closeChannels()
	runtime(t, j, "/bin/sh")

	out, err := newBorrowRunner(j.borrows, j.done).
		RunInteractive(context.Background(), nil, "-c", "printf a-session-key")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "a-session-key" {
		t.Errorf("stdout = %q, want the session key", out)
	}
}

func TestABorrowReportsTheCommandsFailure(t *testing.T) {
	j := newJob()
	defer j.closeChannels()
	runtime(t, j, "/bin/sh")

	if _, err := (newBorrowRunner(j.borrows, j.done)).
		RunInteractive(context.Background(), nil, "-c", "exit 3"); err == nil {
		t.Fatal("a failed unlock should be an error")
	}
}

// The load-bearing detail, and the reason this cannot use tea.ExecProcess:
// `bw unlock --raw` writes its prompt to stderr and the session key to stdout,
// so stdout has to stay captured while the other two go to the terminal.
// tea.Exec points all three at the terminal, which would print the key to the
// screen and lose it.
func TestABorrowedCommandKeepsStdoutAndGivesAwayTheOtherTwo(t *testing.T) {
	command := newBorrowedCommand(context.Background(), "/bin/sh", borrow{
		args: []string{"-c", `printf the-key; printf 'Master password:' >&2; read -r line; printf "%s" "$line" >&2`},
	})

	var stderr, stolen bytes.Buffer
	command.SetStdin(strings.NewReader("typed-in-secret\n"))
	command.SetStderr(&stderr)
	// What tea.Exec does, and what must be ignored.
	command.SetStdout(&stolen)

	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	if got := command.out.String(); got != "the-key" {
		t.Errorf("captured stdout = %q, want the key", got)
	}
	if stolen.Len() != 0 {
		t.Errorf("SetStdout was honoured and wrote %q - the key would be on screen", stolen.String())
	}
	if !strings.Contains(stderr.String(), "Master password:") {
		t.Errorf("the prompt did not reach the terminal: %q", stderr.String())
	}
	// And the typed answer reached bw rather than this process.
	if !strings.Contains(stderr.String(), "typed-in-secret") {
		t.Errorf("stdin was not handed over: %q", stderr.String())
	}
}

// This blocks a goroutine that is holding up an install. Every wait has to be
// releasable, or a reader who quits mid-prompt has a deadlock instead of a
// program.
func TestABorrowIsReleasedWhenTheWindowCloses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		release func(*job, context.CancelFunc)
		want    error
	}{
		{"the window closed", func(j *job, _ context.CancelFunc) { close(j.done) }, errWindowClosed},
		{"the run was cancelled", func(_ *job, cancel context.CancelFunc) { cancel() }, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := newJob() // nothing is reading j.borrows
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			returned := make(chan error, 1)
			go func() {
				_, err := newBorrowRunner(j.borrows, j.done).RunInteractive(ctx, nil, "unlock", "--raw")
				returned <- err
			}()

			select {
			case err := <-returned:
				t.Fatalf("the request did not block, so this proves nothing: %v", err)
			case <-time.After(20 * time.Millisecond):
			}

			tc.release(j, cancel)
			select {
			case err := <-returned:
				if !errors.Is(err, tc.want) {
					t.Errorf("err = %v, want %v", err, tc.want)
				}
			case <-time.After(time.Second):
				t.Fatal("the request was never released")
			}
		})
	}
}

// The same guard on the second half: the request was accepted and then the
// window went away before the answer came.
func TestAnAcceptedBorrowIsStillReleasable(t *testing.T) {
	j := newJob()
	accepted := make(chan struct{})
	go func() {
		<-j.borrows // taken, never answered
		close(accepted)
	}()

	returned := make(chan error, 1)
	go func() {
		_, err := newBorrowRunner(j.borrows, j.done).
			RunInteractive(context.Background(), nil, "unlock", "--raw")
		returned <- err
	}()

	<-accepted
	close(j.done)
	select {
	case err := <-returned:
		if !errors.Is(err, errWindowClosed) {
			t.Errorf("err = %v, want errWindowClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("a taken-but-unanswered request was never released")
	}
}

// `bw status`, `bw sync` and `bw get` want no terminal at all, and routing them
// through the window would suspend it for every one of them.
func TestTheNonInteractiveHalfDoesNotBorrowAnything(t *testing.T) {
	j := newJob()
	defer j.closeChannels()

	runner := borrowRunner{plain: fakeVault{out: "{}"}, requests: j.borrows, done: j.done}
	out, err := runner.Run(context.Background(), nil, "status")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "{}" {
		t.Errorf("out = %q", out)
	}
	select {
	case req := <-j.borrows:
		t.Errorf("a non-interactive call asked for the terminal: %v", req.args)
	default:
	}
}

// ------------------------------------------------------------- the model side --

func TestTheModelTurnsARequestIntoAnExecAndSaysSo(t *testing.T) {
	m := running(t)
	req := borrow{args: []string{"unlock", "--raw"}, reply: make(chan borrowResult, 1)}

	next, cmd := m.Update(borrowMsg{req: req})
	if cmd == nil {
		t.Fatal("no command returned; the terminal would never be handed over")
	}
	body := plain(next.(Model))
	if !strings.Contains(body, "handing the terminal to bw unlock") {
		t.Errorf("the log does not say what is happening:\n%s", body)
	}
}

// A line that prints whatever was passed is one flag away from printing
// something that should not be on screen.
func TestTheBorrowLineNamesOnlyTheSubcommand(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"unlock", "--raw"}, "bw unlock"},
		{[]string{"login", "--apikey", "secret-looking-thing"}, "bw login"},
		{nil, "bw"},
	} {
		if got := describeBorrow(tc.args); got != tc.want {
			t.Errorf("describeBorrow(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestBorrowDoneReArmsTheWaiter(t *testing.T) {
	m := running(t)
	if _, cmd := m.Update(borrowDoneMsg{}); cmd == nil {
		t.Fatal("nothing re-armed the waiter; a second prompt would never arrive")
	}
	// And with no job there is nothing to wait for, rather than a nil receive
	// on a goroutine nobody is watching.
	if waitBorrow(nil) != nil {
		t.Error("waitBorrow(nil) should produce no command")
	}
}

// The mirror of waitEvent: the operation closes both channels when it returns,
// and a closed one ends the wait instead of leaking the goroutine.
func TestWaitBorrowEndsWhenTheChannelCloses(t *testing.T) {
	j := newJob()
	j.closeChannels()
	if msg := waitBorrow(j)(); msg != nil {
		t.Errorf("a closed channel produced %#v, want nothing", msg)
	}
	// Twice, because the panic path and the normal path both reach it.
	j.closeChannels()
}

// fakeVault is a secrets.Runner that answers without a vault.
type fakeVault struct {
	out string
	err error
	// interactive records what was asked of the interactive half.
	interactive *[]string
}

func (f fakeVault) Run(context.Context, []string, ...string) ([]byte, error) {
	return []byte(f.out), f.err
}

func (f fakeVault) RunInteractive(_ context.Context, _ []string, args ...string) ([]byte, error) {
	if f.interactive != nil {
		*f.interactive = append(*f.interactive, strings.Join(args, " "))
	}
	return []byte(f.out), f.err
}

var _ secrets.Runner = fakeVault{}
var _ tea.ExecCommand = (*borrowedCommand)(nil)
