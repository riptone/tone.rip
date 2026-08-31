package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The secrets phase, which used to be unreachable from a test: it built its
// own `bw` runner, so asserting any of this meant a real vault, a real login and
// a real password. App.Vault is the seam that made the window able to own the
// prompt, and it made this testable at the same time.

// vaultStub answers `bw` from a canned map, and records what was asked.
type vaultStub struct {
	mu sync.Mutex
	// answers is keyed by the joined arguments, as ExecRunner would spell them.
	answers map[string]string
	// fail is keyed the same way, for the calls that should error.
	fail map[string]error

	plain       []string
	interactive []string
}

func newVault(status string) *vaultStub {
	return &vaultStub{
		answers: map[string]string{"status": status, "sync": "Syncing complete."},
		fail:    map[string]error{},
	}
}

func (v *vaultStub) answer(key, body string) *vaultStub {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.answers[key] = body
	return v
}

func (v *vaultStub) refuse(key string, err error) *vaultStub {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.fail[key] = err
	return v
}

func (v *vaultStub) Run(_ context.Context, _ []string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	v.mu.Lock()
	defer v.mu.Unlock()
	v.plain = append(v.plain, key)
	if err := v.fail[key]; err != nil {
		return nil, err
	}
	body, ok := v.answers[key]
	if !ok {
		return nil, errors.New("vaultStub: no answer for `bw " + key + "`")
	}
	return []byte(body), nil
}

func (v *vaultStub) RunInteractive(_ context.Context, _ []string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	v.mu.Lock()
	defer v.mu.Unlock()
	v.interactive = append(v.interactive, key)
	if err := v.fail[key]; err != nil {
		return nil, err
	}
	body, ok := v.answers[key]
	if !ok {
		return nil, errors.New("vaultStub: no answer for `bw " + key + "`")
	}
	// The vault is unlocked from here on, so a Status after this says so.
	v.answers["status"] = `{"serverUrl":"https://vault.bitwarden.eu","status":"unlocked"}`
	return []byte(body), nil
}

func (v *vaultStub) asked(kind string) []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if kind == "interactive" {
		return append([]string(nil), v.interactive...)
	}
	return append([]string(nil), v.plain...)
}

func (v *vaultStub) sawInteractive(want string) bool {
	for _, call := range v.asked("interactive") {
		if call == want {
			return true
		}
	}
	return false
}

// withSecrets rewrites the fixture's manifest to declare a vault and secrets.
func withSecrets(t *testing.T, a *App, decl string) {
	t.Helper()
	write(t, filepath.Join(a.Repo, "manifest.jsonc"),
		strings.Replace(fixtureManifest, `"health"`, decl+`
			 "health"`, 1))
}

const oneSecret = `"vault": {"server":"https://vault.bitwarden.eu"},
			 "secrets": [{"name":"creds","mode":"file","item":"dotfiles/creds","target":"~/.doti/creds.json"}],`

// What `bw get item` returns: one object, whose name is checked against the one
// that was asked for - `get item` searches rather than matches, so a near miss
// would otherwise render a different item's contents.
const noteBody = `{"id":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","name":"dotfiles/creds",` +
	`"notes":"{\"server\":\"db.example\"}","revisionDate":"2026-01-01T00:00:00Z"}`

// The path that used to be a dead end inside a window: locked vault, somebody
// watching, so `bw` is asked for the password and the secret lands.
func TestAnInteractiveRunUnlocksAndRendersTheSecret(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, oneSecret)

	vault := newVault(`{"serverUrl":"https://vault.bitwarden.eu","status":"locked"}`).
		answer("unlock --raw", "a-session-key").
		answer("get item dotfiles/creds", noteBody)
	a.Vault = vault
	a.Interactive = true

	if err := a.Secrets(context.Background()); err != nil {
		t.Fatalf("Secrets: %v", err)
	}
	if !vault.sawInteractive("unlock --raw") {
		t.Errorf("the vault was never unlocked: %v", vault.asked("interactive"))
	}
	if !rec.Contains("vault unlocked for this run") {
		t.Errorf("the unlock was not reported: %v", rec.Texts())
	}

	target := filepath.Join(a.Home, ".doti", "creds.json")
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("the secret was not written: %v", err)
	}
	if !strings.Contains(string(body), "db.example") {
		t.Errorf("the rendered secret is %q", body)
	}
	// A credential on disk is readable by its owner and nobody else.
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

// A signed-out vault needs the email, the password and possibly a code - all of
// which are bw's own prompts, in order.
func TestASignedOutVaultLogsInFirst(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, oneSecret)

	vault := newVault(`{"serverUrl":"https://vault.bitwarden.eu","status":"unauthenticated"}`).
		answer("login", "You are logged in!").
		answer("unlock --raw", "a-session-key").
		answer("get item dotfiles/creds", noteBody)
	a.Vault = vault
	a.Interactive = true

	if err := a.Secrets(context.Background()); err != nil {
		t.Fatalf("Secrets: %v", err)
	}
	calls := vault.asked("interactive")
	if len(calls) < 2 || calls[0] != "login" || calls[1] != "unlock --raw" {
		t.Errorf("interactive calls = %v, want login then unlock", calls)
	}
}

// A script that stops to ask for a password is a script that hangs.
func TestANonInteractiveRunNeverPrompts(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, oneSecret)

	vault := newVault(`{"serverUrl":"https://vault.bitwarden.eu","status":"locked"}`)
	a.Vault = vault
	a.Interactive = false

	err := a.Secrets(context.Background())
	if err == nil {
		t.Fatal("a locked vault with nobody watching should be an error")
	}
	if !strings.Contains(err.Error(), "BW_SESSION") {
		t.Errorf("the error should say what to run: %v", err)
	}
	if got := vault.asked("interactive"); len(got) != 0 {
		t.Errorf("it prompted anyway: %v", got)
	}
}

// -n reports; it does not prompt, and it does not write the CLI's own state.
func TestADryRunNeitherPromptsNorWrites(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, oneSecret)

	vault := newVault(`{"serverUrl":"https://vault.bitwarden.eu","status":"locked"}`)
	a.Vault = vault
	a.Interactive = true
	a.DryRun = true

	if err := a.Secrets(context.Background()); err == nil {
		t.Fatal("a dry run cannot read a locked vault, and should say so")
	}
	if got := vault.asked("interactive"); len(got) != 0 {
		t.Errorf("a dry run prompted: %v", got)
	}
	for _, call := range vault.asked("plain") {
		if strings.HasPrefix(call, "config") {
			t.Errorf("a dry run wrote the CLI's own state: bw %s", call)
		}
	}
	if !rec.Contains("bw is pointed at https://vault.bitwarden.eu") {
		t.Errorf("it should still report what it found: %v", rec.Texts())
	}
}

// `bw` answers from a local cache, so without a sync a rotated credential
// renders as the old value and nothing says so.
func TestTheVaultIsSyncedBeforeAnythingIsRead(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, oneSecret)

	vault := newVault(`{"serverUrl":"https://vault.bitwarden.eu","status":"unlocked"}`).
		answer("get item dotfiles/creds", noteBody)
	a.Vault = vault

	if err := a.Secrets(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := vault.asked("plain")
	syncAt, readAt := -1, -1
	for i, call := range calls {
		if call == "sync" {
			syncAt = i
		}
		if strings.HasPrefix(call, "get item") && readAt < 0 {
			readAt = i
		}
	}
	if syncAt < 0 {
		t.Fatalf("the vault was never synced: %v", calls)
	}
	if readAt < 0 || syncAt > readAt {
		t.Errorf("read at %d, synced at %d - the sync has to come first", readAt, syncAt)
	}
}

// The selector's Secrets group is these names.
func TestIncludeNarrowsTheSecrets(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, `"vault": {"server":"https://vault.bitwarden.eu"},
		 "secrets": [
		   {"name":"creds","mode":"file","item":"dotfiles/creds","target":"~/.doti/creds.json"},
		   {"name":"other","mode":"file","item":"dotfiles/other","target":"~/.doti/other.json"}
		 ],`)

	vault := newVault(`{"serverUrl":"https://vault.bitwarden.eu","status":"unlocked"}`).
		answer("get item dotfiles/creds", noteBody)
	a.Vault = vault
	a.Include = []string{"creds"}

	if err := a.Secrets(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, call := range vault.asked("plain") {
		if strings.Contains(call, "dotfiles/other") {
			t.Errorf("an unticked secret was fetched: bw %s", call)
		}
	}
	if !rec.Contains("creds -> ") && !rec.Contains("creds (unchanged)") {
		t.Errorf("the ticked secret was not rendered: %v", rec.Texts())
	}
	if _, err := os.Stat(filepath.Join(a.Home, ".doti", "other.json")); err == nil {
		t.Error("an unticked secret was written")
	}
}

// Pointing the CLI at the wrong deployment fails as "Invalid master password",
// an error that sends you looking at your password rather than at the region.
func TestSigningInToTheWrongDeploymentSaysToLogOut(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, oneSecret)

	a.Vault = newVault(`{"serverUrl":"https://vault.bitwarden.com","status":"unlocked"}`)
	a.Interactive = true

	err := a.Secrets(context.Background())
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "bw logout") {
		t.Errorf("the error should say what to do: %v", err)
	}
}

// Nothing declared is not a failure, and must not reach for `bw` at all.
func TestNoSecretsDeclaredTouchesNoVault(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	vault := newVault(`{"status":"locked"}`)
	a.Vault = vault

	if err := a.Secrets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(vault.asked("plain")) + len(vault.asked("interactive")); got != 0 {
		t.Errorf("it ran %d bw commands with nothing to render", got)
	}
	if !rec.Contains("no secrets declared") {
		t.Errorf("it should say why nothing happened: %v", rec.Texts())
	}
}

// Nil means the real binary, which is right for a command and is what every
// existing caller relies on.
func TestANilVaultIsTheRealBinary(t *testing.T) {
	a, _, _ := fixture(t)
	if a.vault() == nil {
		t.Fatal("vault() returned nil")
	}
	stub := newVault(`{"status":"locked"}`)
	a.Vault = stub
	if a.vault() != stub {
		t.Error("an injected runner was ignored")
	}
}
