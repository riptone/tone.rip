// Package secrets renders files that must never live in the dotfiles repo.
//
// Values come from Bitwarden through the `bw` CLI rather than from any
// crypto implemented here: the vault format, the key derivation and the
// session handling are Bitwarden's problem, and reimplementing any of it
// would be a worse version of something already audited.
//
// The session key is held in memory for the life of the process and passed
// to `bw` through its environment. It is never written to disk, never
// exported to the caller's shell, and never printed - see scrub.go, which
// makes leaking a value through an error path a test failure.
package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// State is the vault's lock state, as `bw status` reports it.
type State string

const (
	Unauthenticated State = "unauthenticated"
	Locked          State = "locked"
	Unlocked        State = "unlocked"
)

// UnavailableError says the vault cannot answer yet and what the human has
// to do about it. Returned rather than prompting, because the installer runs
// unattended (`doti --all`) as often as it runs interactively.
type UnavailableError struct {
	State State
}

func (e *UnavailableError) Error() string {
	switch e.State {
	case Unauthenticated:
		return "bitwarden vault is not logged in - run `bw login`, then re-run"
	case Locked:
		return "bitwarden vault is locked - run `export BW_SESSION=$(bw unlock --raw)`, then re-run"
	default:
		return fmt.Sprintf("bitwarden vault is unavailable (state %q)", e.State)
	}
}

// Runner executes the `bw` CLI. An interface so tests can answer without a
// vault, a network or a login.
type Runner interface {
	Run(ctx context.Context, env []string, args ...string) ([]byte, error)
}

// ExecRunner runs the real binary.
type ExecRunner struct {
	// Bin defaults to "bw" on PATH.
	Bin string
}

func (r ExecRunner) Run(ctx context.Context, env []string, args ...string) ([]byte, error) {
	bin := r.Bin
	if bin == "" {
		bin = "bw"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), env...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("bw %s: %w: %s",
				args[0], err, strings.TrimSpace(stderr.String()))
		}
		// The most common one by far, and worth naming: the CLI is not
		// installed. It is in the manifest's tool list for exactly this.
		return nil, fmt.Errorf("running `%s`: %w (is bitwarden-cli installed?)", bin, err)
	}
	return out, nil
}

// Item is the subset of a Bitwarden item this package reads.
type Item struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Notes        string    `json:"notes"`
	RevisionDate time.Time `json:"revisionDate"`
	Login        struct {
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"login"`
	Fields []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"fields"`
}

// Client talks to one vault.
type Client struct {
	runner  Runner
	session string
	// items caches whole items rather than fetching per field. A templated
	// file pulling three values off one login item is one `bw` invocation,
	// not three - and each invocation is a Node process start.
	items map[string]*Item
}

// New builds a client. An empty session means `bw` is left to find its own
// (it reads BW_SESSION from the inherited environment).
func New(runner Runner, session string) *Client {
	return &Client{runner: runner, session: session, items: map[string]*Item{}}
}

func (c *Client) env() []string {
	if c.session == "" {
		return nil
	}
	return []string{"BW_SESSION=" + c.session}
}

// Status reports whether the vault can be read.
func (c *Client) Status(ctx context.Context) (State, error) {
	out, err := c.runner.Run(ctx, c.env(), "status")
	if err != nil {
		return "", err
	}
	var status struct {
		Status State `json:"status"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return "", fmt.Errorf("parsing `bw status`: %w", err)
	}
	return status.Status, nil
}

// RequireUnlocked returns an actionable error unless the vault is readable.
func (c *Client) RequireUnlocked(ctx context.Context) error {
	state, err := c.Status(ctx)
	if err != nil {
		return err
	}
	if state != Unlocked {
		return &UnavailableError{State: state}
	}
	return nil
}

// Sync pulls the latest vault from the server.
//
// Not optional. `bw` answers from a local cache, so without this a rotated
// credential renders as the old value and nothing anywhere says so - the
// worst failure this package could have.
func (c *Client) Sync(ctx context.Context) error {
	_, err := c.runner.Run(ctx, c.env(), "sync")
	return err
}

// Item fetches an item by name or id, once per process.
func (c *Client) Item(ctx context.Context, name string) (*Item, error) {
	if cached, ok := c.items[name]; ok {
		return cached, nil
	}
	out, err := c.runner.Run(ctx, c.env(), "get", "item", name)
	if err != nil {
		return nil, fmt.Errorf("bitwarden item %q: %w", name, err)
	}
	item := &Item{}
	if err := json.Unmarshal(out, item); err != nil {
		return nil, fmt.Errorf("parsing bitwarden item %q: %w", name, err)
	}
	c.items[name] = item
	return item, nil
}

// Field reads one field off an item.
//
// "notes", "username" and "password" are the built-ins; anything else is
// matched against the item's custom fields. A miss lists the field *names*
// available on the item and never their values, because the obvious way to
// write this error message is to dump the item.
func (c *Client) Field(ctx context.Context, itemName, field string) (string, error) {
	item, err := c.Item(ctx, itemName)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(field) {
	case "notes":
		return item.Notes, nil
	case "username":
		return item.Login.Username, nil
	case "password":
		return item.Login.Password, nil
	}
	for _, f := range item.Fields {
		if f.Name == field {
			return f.Value, nil
		}
	}
	names := make([]string, 0, len(item.Fields))
	for _, f := range item.Fields {
		names = append(names, f.Name)
	}
	return "", fmt.Errorf(
		"bitwarden item %q has no field %q (custom fields: %s)",
		itemName, field, strings.Join(names, ", "))
}
