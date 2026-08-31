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

// VaultStatus is what `bw status` reports.
type VaultStatus struct {
	State State
	// ServerURL is the deployment the CLI is pointed at. Empty until one is
	// configured - the CLI defaults to the US cloud and does not say so.
	ServerURL string
}

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
	// RunInteractive runs bw with the terminal attached to its prompts and
	// returns only what it wrote to stdout.
	//
	// The split is load-bearing and was determined by looking rather than
	// guessing: `bw unlock --raw` writes its "Master password:" prompt to
	// *stderr* and the session key to *stdout*. Inheriting stdin and stderr
	// therefore lets somebody type their master password straight into bw -
	// it never passes through doti, is never in doti's argv, and is never in
	// doti's memory - while doti still gets the key it needs.
	RunInteractive(ctx context.Context, env []string, args ...string) ([]byte, error)
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

func (r ExecRunner) RunInteractive(ctx context.Context, env []string, args ...string) ([]byte, error) {
	bin := r.Bin
	if bin == "" {
		bin = "bw"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), env...)
	// Inherited, so bw owns the prompt and the typing.
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	var out strings.Builder
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		// Deliberately without the captured stdout: on the unlock path that
		// is the session key, and an error carrying it would put a live
		// vault credential into a log.
		return nil, fmt.Errorf("bw %s: %w", strings.Join(args, " "), err)
	}
	return []byte(out.String()), nil
}

// Login signs the CLI in, interactively.
//
// Every stream is inherited: bw asks for the email, the master password and a
// two-factor code if the account has one, and none of that should pass
// through this process. There is nothing to capture - success is a state
// change in bw's own data file.
func (c *Client) Login(ctx context.Context) error {
	if _, err := c.runner.RunInteractive(ctx, nil, "login"); err != nil {
		return fmt.Errorf("bw login: %w", err)
	}
	return nil
}

// Unlock unlocks the vault and adopts the resulting session for this run.
//
// The session is held in memory only. It is deliberately *not* written
// anywhere: a session key on disk is a vault with the lock left open, and
// exporting it into the caller's shell is not something a child process can
// do anyway.
func (c *Client) Unlock(ctx context.Context) error {
	out, err := c.runner.RunInteractive(ctx, nil, "unlock", "--raw")
	if err != nil {
		return fmt.Errorf("bw unlock: %w", err)
	}
	session := strings.TrimSpace(string(out))
	if session == "" {
		return fmt.Errorf("bw unlock returned no session key")
	}
	c.session = session
	// The cache belongs to the previous session, if there was one.
	c.items = map[string]*Item{}
	return nil
}

// HasSession reports whether this client is holding a session key.
func (c *Client) HasSession() bool { return c.session != "" }

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

// Status reports what the CLI is pointed at and whether it can be read.
func (c *Client) Status(ctx context.Context) (VaultStatus, error) {
	out, err := c.runner.Run(ctx, c.env(), "status")
	if err != nil {
		return VaultStatus{}, err
	}
	var status struct {
		Status    State  `json:"status"`
		ServerURL string `json:"serverUrl"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return VaultStatus{}, fmt.Errorf("parsing `bw status`: %w", err)
	}
	return VaultStatus{State: status.Status, ServerURL: status.ServerURL}, nil
}

// RequireUnlocked returns an actionable error unless the vault is readable.
func (c *Client) RequireUnlocked(ctx context.Context) error {
	status, err := c.Status(ctx)
	if err != nil {
		return err
	}
	if status.State != Unlocked {
		return &UnavailableError{State: status.State}
	}
	return nil
}

// normaliseServer makes two deployment URLs comparable.
func normaliseServer(url string) string {
	return strings.TrimRight(strings.TrimSpace(strings.ToLower(url)), "/")
}

// EnsureServer points the CLI at the deployment the manifest names.
//
// This exists because the failure without it is actively misleading. `bw`
// defaults to the US cloud and does not mention it, so logging in with a
// bitwarden.eu account fails with
//
//	Invalid master password. Confirm your email is correct...
//
// which sends you looking at your password. It is a wrong-server error. The
// region is a deployment fact rather than a secret, so it belongs in the
// manifest, and every new machine gets it right without anyone remembering.
//
// Reports whether it changed anything.
func (c *Client) EnsureServer(ctx context.Context, want string) (bool, error) {
	if want == "" {
		// Nothing declared: leave whatever the operator configured alone.
		return false, nil
	}
	status, err := c.Status(ctx)
	if err != nil {
		return false, err
	}
	if normaliseServer(status.ServerURL) == normaliseServer(want) {
		return false, nil
	}
	// `bw config server` is refused while logged in, because the session
	// belongs to the old deployment. Say that, rather than letting bw's own
	// error stand.
	if status.State != Unauthenticated {
		return false, fmt.Errorf(
			"the CLI is signed in to %s but the manifest names %s - run `bw logout`, then re-run",
			displayServer(status.ServerURL), want)
	}
	if _, err := c.runner.Run(ctx, nil, "config", "server", want); err != nil {
		return false, fmt.Errorf("pointing bw at %s: %w", want, err)
	}
	return true, nil
}

// displayServer names the deployment for a human, including the one bw uses
// when nothing has been configured.
func displayServer(url string) string {
	if strings.TrimSpace(url) == "" {
		return "the default (vault.bitwarden.com)"
	}
	return url
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
	// `bw get item` *searches*: it will happily answer a partial name match.
	// So asking for "dotfiles/mssql-envs" can be satisfied by an item called
	// something else that merely contains it, and the wrong credentials land
	// in a config file with nothing anywhere saying so. An id is exempt
	// because an id is exact by construction.
	if !looksLikeID(name) && !strings.EqualFold(item.Name, name) {
		return nil, fmt.Errorf(
			"asked bitwarden for %q and it answered with %q - "+
				"rename the item to match, or use its id",
			name, item.Name)
	}
	c.items[name] = item
	return item, nil
}

// looksLikeID reports whether a lookup string is a Bitwarden item id rather
// than a name: 36 characters, 8-4-4-4-12 hex.
func looksLikeID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
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
