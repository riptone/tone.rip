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

// A dry run may unlock, because unlocking is a read: the session is held in
// memory and written nowhere. The earlier rule refused it along with signing in,
// which left Preview able to report only "the vault is locked" - not a preview
// of anything.
func TestADryRunMayUnlockAndStillWritesNothing(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, oneSecret)

	vault := newVault(`{"serverUrl":"https://vault.bitwarden.eu","status":"locked"}`).
		answer("unlock --raw", "a-session-key").
		answer("get item dotfiles/creds", noteBody)
	a.Vault = vault
	a.Interactive = true
	a.DryRun = true

	if err := a.Secrets(context.Background()); err != nil {
		t.Fatalf("Secrets: %v", err)
	}
	if !vault.sawInteractive("unlock --raw") {
		t.Errorf("it did not unlock, so it cannot have read anything: %v",
			vault.asked("interactive"))
	}
	// The point of a preview: it says what would land.
	if !rec.Contains("would write creds -> ") {
		t.Errorf("it did not report what would change: %v", rec.Texts())
	}
	// And nothing did.
	if _, err := os.Stat(filepath.Join(a.Home, ".doti", "creds.json")); err == nil {
		t.Error("a dry run wrote the secret")
	}
	for _, call := range vault.asked("plain") {
		if strings.HasPrefix(call, "config") {
			t.Errorf("a dry run wrote the CLI's own state: bw %s", call)
		}
	}
}

// Signing in *is* a change: `bw login` writes credentials into the CLI's own
// data file, and those outlive the run.
func TestADryRunWillNotSignIn(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, oneSecret)

	vault := newVault(`{"serverUrl":"https://vault.bitwarden.eu","status":"unauthenticated"}`).
		answer("login", "You are logged in!").
		answer("unlock --raw", "a-session-key")
	a.Vault = vault
	a.Interactive = true
	a.DryRun = true

	if err := a.Secrets(context.Background()); err == nil {
		t.Fatal("a signed-out vault cannot be read on a dry run, and should say so")
	}
	if vault.sawInteractive("login") {
		t.Errorf("a dry run signed in: %v", vault.asked("interactive"))
	}
	if !rec.Contains("would sign in to https://vault.bitwarden.eu") {
		t.Errorf("it should say what it would have done: %v", rec.Texts())
	}
}

// Pointing the CLI at another deployment is a change too, and reading through
// the one it is pointed at now would answer about the wrong account.
func TestADryRunWillNotMoveTheDeployment(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, oneSecret)

	vault := newVault(`{"serverUrl":"https://vault.bitwarden.com","status":"unauthenticated"}`)
	a.Vault = vault
	a.Interactive = true
	a.DryRun = true

	if err := a.Secrets(context.Background()); err == nil {
		t.Fatal("want an error rather than an answer about the wrong account")
	}
	if !rec.Contains("would point bw at https://vault.bitwarden.eu") {
		t.Errorf("%v", rec.Texts())
	}
	for _, call := range vault.asked("plain") {
		if strings.HasPrefix(call, "config") {
			t.Errorf("a dry run wrote the CLI's own state: bw %s", call)
		}
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
	a.Include = Refs([]string{"creds"})

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

// `bw` answers from a local cache, so a failed sync means every value read
// after it could be stale - and a rotated credential rendering as the old one,
// silently, is the failure this phase exists to avoid. It stops.
func TestAFailedSyncStopsBeforeAnythingIsRead(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, oneSecret)

	vault := newVault(`{"serverUrl":"https://vault.bitwarden.eu","status":"unlocked"}`).
		refuse("sync", errors.New("Failed to sync: connect ECONNREFUSED")).
		answer("get item dotfiles/creds", noteBody)
	a.Vault = vault

	err := a.Secrets(context.Background())
	if err == nil {
		t.Fatal("a failed sync should be an error")
	}
	if !strings.Contains(err.Error(), "syncing vault") {
		t.Errorf("the error does not say what failed: %v", err)
	}
	if !rec.Contains("vault sync failed") {
		t.Errorf("the failure was not reported: %v", rec.Texts())
	}
	for _, call := range vault.asked("plain") {
		if strings.HasPrefix(call, "get item") {
			t.Errorf("a value was read through a stale cache: bw %s", call)
		}
	}
	if _, statErr := os.Stat(filepath.Join(a.Home, ".doti", "creds.json")); statErr == nil {
		t.Error("a secret was written from a cache that could not be trusted")
	}
}

// An item that is not in the vault. The name is worth having in the error,
// because the usual cause is a typo in the manifest rather than a missing note.
func TestAMissingItemNamesWhatWasLookedFor(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, oneSecret)

	a.Vault = newVault(`{"serverUrl":"https://vault.bitwarden.eu","status":"unlocked"}`).
		refuse("get item dotfiles/creds", errors.New("Not found."))

	err := a.Secrets(context.Background())
	if err == nil {
		t.Fatal("a missing item should be an error")
	}
	if !strings.Contains(err.Error(), "dotfiles/creds") {
		t.Errorf("the error does not name the item: %v", err)
	}
}

// Whatever did land is reported before the failure is returned: a partial run
// is worth knowing about, and the next one starts from what is already there.
func TestASecondSecretFailingStillReportsTheFirst(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, `"vault": {"server":"https://vault.bitwarden.eu"},
		 "secrets": [
		   {"name":"creds","mode":"file","item":"dotfiles/creds","target":"~/.doti/creds.json"},
		   {"name":"other","mode":"file","item":"dotfiles/other","target":"~/.doti/other.json"}
		 ],`)

	a.Vault = newVault(`{"serverUrl":"https://vault.bitwarden.eu","status":"unlocked"}`).
		answer("get item dotfiles/creds", noteBody).
		refuse("get item dotfiles/other", errors.New("Not found."))

	if err := a.Secrets(context.Background()); err == nil {
		t.Fatal("the failure should be returned")
	}
	if !rec.Contains("creds -> ") {
		t.Errorf("the one that worked was not reported: %v", rec.Texts())
	}
	if _, err := os.Stat(filepath.Join(a.Home, ".doti", "creds.json")); err != nil {
		t.Errorf("the first secret was rolled back: %v", err)
	}
}

// Unticking every secret must not open the vault.
//
// It used to: the per-secret filter ran *after* the deployment was set, the
// master password was asked for and the vault was synced - so a Preview with the
// secrets unticked still stopped to prompt, and then rendered nothing. A
// master-password prompt for a phase that was going to do nothing is the worst
// version of a checkbox that does not work.
func TestNoSecretsSelectedNeverTouchesTheVault(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, oneSecret)

	// Locked, so anything that got as far as opening it would have to prompt.
	vault := newVault(`{"serverUrl":"https://vault.bitwarden.eu","status":"locked"}`).
		answer("unlock --raw", "a-session-key").
		answer("get item dotfiles/creds", noteBody)
	a.Vault = vault
	a.Include = []Ref{{Kind: KindStow, Label: "zsh"}}

	if err := a.Secrets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if asked := vault.asked("plain"); len(asked) != 0 {
		t.Errorf("bw was run: %v", asked)
	}
	if asked := vault.asked("interactive"); len(asked) != 0 {
		t.Errorf("the master password was asked for: %v", asked)
	}
	if !rec.Contains("no secrets selected") {
		t.Errorf("%v", rec.Texts())
	}
}

// And with one ticked it does, or the test above would pass for the wrong
// reason.
func TestOneSecretSelectedStillOpensTheVault(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
	withSecrets(t, a, oneSecret)

	vault := newVault(`{"serverUrl":"https://vault.bitwarden.eu","status":"locked"}`).
		answer("unlock --raw", "a-session-key").
		answer("get item dotfiles/creds", noteBody)
	a.Vault = vault
	// Somebody is watching, so a locked vault is a prompt rather than an error.
	a.Interactive = true
	a.Include = []Ref{{Kind: KindSecret, Label: "creds"}}

	if err := a.Secrets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !vault.sawInteractive("unlock --raw") {
		t.Error("a ticked secret did not open the vault")
	}
}
