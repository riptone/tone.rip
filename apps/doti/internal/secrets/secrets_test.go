package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// fakeRunner answers like `bw` without a vault, a login or a network.
type fakeRunner struct {
	responses map[string]string
	calls     []string
	// interactive records the calls that got the terminal.
	interactive []string
	env         []string
	err         error
}

// RunInteractive stands in for the streams bw would own. The tests never type
// anything, so it records the call and answers from the same canned map -
// what matters is *which* commands doti reaches for and in what order.
func (f *fakeRunner) RunInteractive(ctx context.Context, env []string, args ...string) ([]byte, error) {
	f.interactive = append(f.interactive, strings.Join(args, " "))
	return f.Run(ctx, env, args...)
}

func (f *fakeRunner) Run(_ context.Context, env []string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	f.calls = append(f.calls, key)
	f.env = env
	if f.err != nil {
		return nil, f.err
	}
	body, ok := f.responses[key]
	if !ok {
		return nil, errors.New("fakeRunner: no canned response for `bw " + key + "`")
	}
	return []byte(body), nil
}

const mssqlNote = `{"environments":[{"name":"testaria","password":"sup3r-s3cret-value"}]}`

func newFake() *fakeRunner {
	return &fakeRunner{responses: map[string]string{
		"status": `{"status":"unlocked"}`,
		"sync":   `Syncing complete.`,
		"get item dotfiles/mssql-envs": `{
			"id":"1","name":"dotfiles/mssql-envs","notes":` + quote(mssqlNote) + `,
			"login":{"username":"","password":""},"fields":[]}`,
		"get item dotfiles/git-identity": `{
			"id":"2","name":"dotfiles/git-identity",
			"login":{"username":"me@example.com","password":"pw-not-used"},
			"fields":[{"name":"signing-key","value":"ABCDEF0123456789"}]}`,
	}}
}

func quote(s string) string {
	out := strings.ReplaceAll(s, `\`, `\\`)
	return `"` + strings.ReplaceAll(out, `"`, `\"`) + `"`
}

func TestVaultStateIsActionable(t *testing.T) {
	for state, want := range map[string]string{
		"locked":          "bw unlock",
		"unauthenticated": "bw login",
	} {
		t.Run(state, func(t *testing.T) {
			runner := &fakeRunner{responses: map[string]string{
				"status": `{"status":"` + state + `"}`,
			}}
			err := New(runner, "sess").RequireUnlocked(context.Background())
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error should tell the user to run `%s`, got: %v", want, err)
			}
			var unavailable *UnavailableError
			if !errors.As(err, &unavailable) {
				t.Fatalf("want an *UnavailableError, got %T", err)
			}
		})
	}
}

func TestTheSessionReachesTheCLIAndOnlyThroughTheEnvironment(t *testing.T) {
	runner := newFake()
	client := New(runner, "session-token-value")
	if _, err := client.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.env) != 1 || runner.env[0] != "BW_SESSION=session-token-value" {
		t.Fatalf("session should be passed in the env, got %v", runner.env)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "session-token-value") {
			t.Fatalf("session leaked into argv (visible in ps): %q", call)
		}
	}
}

func TestAnItemIsFetchedOnceHoweverManyFieldsAreRead(t *testing.T) {
	runner := newFake()
	client := New(runner, "s")
	ctx := context.Background()
	for _, field := range []string{"username", "signing-key", "password"} {
		if _, err := client.Field(ctx, "dotfiles/git-identity", field); err != nil {
			t.Fatalf("%s: %v", field, err)
		}
	}
	var gets int
	for _, call := range runner.calls {
		if strings.HasPrefix(call, "get item") {
			gets++
		}
	}
	if gets != 1 {
		t.Fatalf("want 1 `bw get item` for 3 fields, got %d", gets)
	}
}

func TestFieldResolution(t *testing.T) {
	client := New(newFake(), "s")
	ctx := context.Background()
	for field, want := range map[string]string{
		"username":    "me@example.com",
		"signing-key": "ABCDEF0123456789",
	} {
		got, err := client.Field(ctx, "dotfiles/git-identity", field)
		if err != nil {
			t.Fatalf("%s: %v", field, err)
		}
		if got != want {
			t.Fatalf("%s = %q, want %q", field, got, want)
		}
	}
}

// The obvious way to write this error dumps the item so the user can see
// what is on it. That puts every credential on the item into the message.
func TestAMissingFieldNamesTheFieldsButNeverTheirValues(t *testing.T) {
	client := New(newFake(), "s")
	_, err := client.Field(context.Background(), "dotfiles/git-identity", "nope")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "signing-key") {
		t.Fatalf("error should list available field names, got: %v", err)
	}
	if strings.Contains(err.Error(), "ABCDEF0123456789") ||
		strings.Contains(err.Error(), "me@example.com") {
		t.Fatalf("error leaked a field value: %v", err)
	}
}

func newRenderer(t *testing.T, dryRun bool) (*Renderer, string) {
	t.Helper()
	home := t.TempDir()
	return &Renderer{
		Client:   New(newFake(), "s"),
		RepoRoot: t.TempDir(),
		Home:     home,
		Platform: manifest.MacOS,
		DryRun:   dryRun,
	}, home
}

var fileSecret = manifest.Secret{
	Name:   "mssql-envs",
	Mode:   manifest.ModeFile,
	Item:   "dotfiles/mssql-envs",
	Field:  "notes",
	Target: "~/.config/opencode/mssql-envs.json",
}

func TestModeFileWritesTheNoteOwnerOnly(t *testing.T) {
	r, home := newRenderer(t, false)
	result, err := r.Render(context.Background(), fileSecret)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Error("first render should report a change")
	}

	path := filepath.Join(home, ".config", "opencode", "mssql-envs.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != mssqlNote {
		t.Fatalf("content = %q", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}
	// The parent directory is created owner-only too - a 0755 directory
	// holding a 0600 file still tells every account on the box it exists.
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir mode = %o, want 700", perm)
	}
}

func TestRenderingTwiceIsIdempotentAndLeavesNoTempFiles(t *testing.T) {
	r, home := newRenderer(t, false)
	ctx := context.Background()
	if _, err := r.Render(ctx, fileSecret); err != nil {
		t.Fatal(err)
	}
	second, err := r.Render(ctx, fileSecret)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Error("second render should report no change")
	}

	dir := filepath.Join(home, ".config", "opencode")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".doti-") {
			t.Fatalf("left a temp file behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Fatalf("want exactly the target file, got %d entries", len(entries))
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	r, home := newRenderer(t, true)
	result, err := r.Render(context.Background(), fileSecret)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Error("dry run should still report what would change")
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Fatal("dry run created something on disk")
	}
}

func TestModeTemplateFillsTheHoles(t *testing.T) {
	r, home := newRenderer(t, false)
	tmplPath := filepath.Join(r.RepoRoot, "git", ".gitconfig.local.tmpl")
	if err := os.MkdirAll(filepath.Dir(tmplPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[user]\n\temail = {{.email}}\n\tsigningkey = {{.signingkey}}\n"
	if err := os.WriteFile(tmplPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	secret := manifest.Secret{
		Name: "gitconfig-local", Mode: manifest.ModeTemplate,
		Template: "git/.gitconfig.local.tmpl", Target: "~/.gitconfig.local",
		Values: map[string]manifest.ValueRef{
			"email":      {Item: "dotfiles/git-identity", Field: "username"},
			"signingkey": {Item: "dotfiles/git-identity", Field: "signing-key"},
		},
	}
	if _, err := r.Render(context.Background(), secret); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".gitconfig.local"))
	if err != nil {
		t.Fatal(err)
	}
	want := "[user]\n\temail = me@example.com\n\tsigningkey = ABCDEF0123456789\n"
	if string(got) != want {
		t.Fatalf("rendered:\n%s\nwant:\n%s", got, want)
	}
}

// Without missingkey=error this renders the literal "<no value>" into the
// config and the failure surfaces later, somewhere else.
func TestAPlaceholderWithNoValueIsAnError(t *testing.T) {
	r, _ := newRenderer(t, false)
	tmplPath := filepath.Join(r.RepoRoot, "t.tmpl")
	if err := os.WriteFile(tmplPath, []byte("k = {{.absent}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := r.Render(context.Background(), manifest.Secret{
		Name: "x", Mode: manifest.ModeTemplate, Template: "t.tmpl", Target: "~/x",
		Values: map[string]manifest.ValueRef{
			"email": {Item: "dotfiles/git-identity", Field: "username"},
		},
	})
	if err == nil {
		t.Fatal("want an error for an unfilled placeholder")
	}
}

// An empty value nearly always means the item or field is wrong. Writing it
// truncates a working config to nothing.
func TestAnEmptyValueIsRefusedRatherThanWritten(t *testing.T) {
	runner := newFake()
	runner.responses["get item empty/item"] =
		`{"id":"3","name":"empty/item","notes":"","login":{},"fields":[]}`
	r, home := newRenderer(t, false)
	r.Client = New(runner, "s")

	_, err := r.Render(context.Background(), manifest.Secret{
		Name: "empty", Mode: manifest.ModeFile, Item: "empty/item",
		Field: "notes", Target: "~/empty.json",
	})
	if err == nil {
		t.Fatal("want an error for an empty value")
	}
	if _, statErr := os.Stat(filepath.Join(home, "empty.json")); !os.IsNotExist(statErr) {
		t.Fatal("wrote a file despite the empty value")
	}
}

func TestASecretForAnotherPlatformIsSkipped(t *testing.T) {
	r, home := newRenderer(t, false)
	secret := fileSecret
	secret.Platforms = []manifest.Platform{manifest.Windows}
	result, err := r.Render(context.Background(), secret)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Skipped {
		t.Fatal("want skipped")
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Fatal("a skipped secret still wrote something")
	}
}

// The wiring that makes the scrubber worth having: every value this package
// fetches must be registered, so any message routed through it is clean.
func TestFetchedValuesAreRegisteredForRedaction(t *testing.T) {
	r, _ := newRenderer(t, false)
	if _, err := r.Render(context.Background(), fileSecret); err != nil {
		t.Fatal(err)
	}
	leaky := "failed while handling " + mssqlNote
	if got := r.Scrubber().Text(leaky); strings.Contains(got, "sup3r-s3cret-value") {
		t.Fatalf("value was not registered for redaction: %q", got)
	}
}

func TestScrubber(t *testing.T) {
	var s Scrubber
	s.Add("hunter2-and-then-some")
	s.Add("hunter2-and-then-some") // deduped
	s.Add("abc")                   // too short to mask safely

	if got := s.Text("pw=hunter2-and-then-some end"); got != "pw=[redacted] end" {
		t.Fatalf("Text = %q", got)
	}
	if got := s.Text("abc"); got != "abc" {
		t.Fatalf("short values must not be masked, got %q", got)
	}

	err := s.Err(errors.New("connect failed: hunter2-and-then-some"))
	if strings.Contains(err.Error(), "hunter2-and-then-some") {
		t.Fatalf("Err leaked: %v", err)
	}
	// A scrubbed error must not expose the original through Unwrap, which
	// would be the same leak by a longer route.
	if errors.Unwrap(err) != nil {
		t.Fatal("scrubbed error should not unwrap to the original")
	}
	if s.Err(nil) != nil {
		t.Fatal("Err(nil) should stay nil")
	}
}

// The failure this guard exists for, reproduced exactly.
//
// `~/.config/opencode/mssql-envs.json` reads like a path in $HOME. But stow
// *folds*: on the machine this was found on, `~/.config/opencode` was a
// symlink into the checkout, so rendering "into $HOME" wrote the credentials
// into the working tree - one `git add -A` from being committed, which is the
// single thing this package exists to prevent.
func TestRenderingRefusesToWriteThroughAFoldIntoTheRepo(t *testing.T) {
	r, home := newRenderer(t, false)

	// The repo holds a config directory, and $HOME folds onto it - the shape
	// stow produces when nothing else needs to share the parent.
	repoConfig := filepath.Join(r.RepoRoot, "opencode", ".config", "opencode")
	if err := os.MkdirAll(repoConfig, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repoConfig, filepath.Join(home, ".config", "opencode")); err != nil {
		t.Fatal(err)
	}

	_, err := r.Render(context.Background(), manifest.Secret{
		Name: "mssql-envs", Mode: manifest.ModeFile,
		Item: "dotfiles/mssql-envs", Field: "notes",
		Target: "~/.config/opencode/mssql-envs.json",
	})
	if err == nil {
		t.Fatal("want a refusal - the target resolves into the repository")
	}
	if !strings.Contains(err.Error(), "refusing to render into the repository") {
		t.Fatalf("error = %v", err)
	}
	// And nothing was written on the way to deciding that.
	if _, statErr := os.Stat(filepath.Join(repoConfig, "mssql-envs.json")); !os.IsNotExist(statErr) {
		t.Fatal("the secret was written into the repo despite the refusal")
	}
}

// A target that merely shares a prefix with the repo path must still be
// allowed; the check is about where it lands, not how it is spelled.
func TestATargetOutsideTheRepoIsAllowed(t *testing.T) {
	r, home := newRenderer(t, false)
	if err := os.MkdirAll(filepath.Join(home, ".doti"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := r.Render(context.Background(), manifest.Secret{
		Name: "mssql-envs", Mode: manifest.ModeFile,
		Item: "dotfiles/mssql-envs", Field: "notes",
		Target: "~/.doti/mssql-envs.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Error("want it written")
	}
	if _, err := os.Stat(filepath.Join(home, ".doti", "mssql-envs.json")); err != nil {
		t.Fatalf("not written: %v", err)
	}
}

func TestWithin(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "a", "b")
	for path, want := range map[string]bool{
		filepath.Join(root):                                  true,
		filepath.Join(root, "c"):                             true,
		filepath.Join(root, "c", "d"):                        true,
		filepath.Join(string(filepath.Separator), "a"):       false,
		filepath.Join(string(filepath.Separator), "a", "bc"): false,
		filepath.Join(string(filepath.Separator), "x"):       false,
	} {
		if got := within(path, root); got != want {
			t.Errorf("within(%q, %q) = %v, want %v", path, root, got, want)
		}
	}
}

// A whole-file secret is pasted into a vault note by hand, and a truncated
// copy is the commonest way that goes wrong: the note still renders, and the
// tool reading it fails somewhere that looks unrelated.
func TestAMalformedJSONNoteIsRefused(t *testing.T) {
	runner := newFake()
	runner.responses["get item broken/note"] =
		`{"id":"9","name":"broken/note","notes":"{\"environments\": [",` +
			`"login":{},"fields":[]}`
	r, home := newRenderer(t, false)
	r.Client = New(runner, "s")

	_, err := r.Render(context.Background(), manifest.Secret{
		Name: "envs", Mode: manifest.ModeFile, Item: "broken/note",
		Field: "notes", Target: "~/.doti/envs.json",
	})
	if err == nil {
		t.Fatal("want a refusal for a truncated note")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".doti", "envs.json")); !os.IsNotExist(statErr) {
		t.Error("a malformed note was written anyway")
	}
}

// Only when the target says it is JSON - a note holding an SSH key or an
// ini file is not malformed for failing to parse as JSON.
func TestANonJSONTargetIsNotParsed(t *testing.T) {
	runner := newFake()
	runner.responses["get item plain/note"] =
		`{"id":"9","name":"plain/note","notes":"not json at all","login":{},"fields":[]}`
	r, home := newRenderer(t, false)
	r.Client = New(runner, "s")

	if _, err := r.Render(context.Background(), manifest.Secret{
		Name: "conf", Mode: manifest.ModeFile, Item: "plain/note",
		Field: "notes", Target: "~/.doti/thing.conf",
	}); err != nil {
		t.Fatalf("a non-JSON target should not be parsed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".doti", "thing.conf")); err != nil {
		t.Fatalf("not written: %v", err)
	}
}

// The misleading failure this exists for: `bw` defaults to the US cloud and
// does not say so, so a bitwarden.eu account fails to log in with "Invalid
// master password" - which sends you looking at your password.
func TestEnsureServerPointsTheCLIAtTheRightDeployment(t *testing.T) {
	runner := newFake()
	runner.responses["status"] = `{"serverUrl":null,"lastSync":null,"status":"unauthenticated"}`
	runner.responses["config server https://vault.bitwarden.eu"] = "Saved setting `config`."

	changed, err := New(runner, "").EnsureServer(context.Background(), "https://vault.bitwarden.eu")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("want a change on a fresh CLI")
	}
	var configured bool
	for _, call := range runner.calls {
		if call == "config server https://vault.bitwarden.eu" {
			configured = true
		}
	}
	if !configured {
		t.Fatalf("bw config server was never run: %v", runner.calls)
	}
}

func TestEnsureServerIsANoOpWhenAlreadyCorrect(t *testing.T) {
	runner := newFake()
	// Trailing slash and case differ; the deployment does not.
	runner.responses["status"] =
		`{"serverUrl":"https://Vault.Bitwarden.EU/","lastSync":null,"status":"locked"}`

	changed, err := New(runner, "").EnsureServer(context.Background(), "https://vault.bitwarden.eu")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("the same deployment spelled differently is not a change")
	}
	for _, call := range runner.calls {
		if strings.HasPrefix(call, "config") {
			t.Fatalf("nothing should have been configured: %v", runner.calls)
		}
	}
}

// `bw config server` is refused while signed in, because the session belongs
// to the old deployment. Saying so beats letting bw's own error stand.
func TestSwitchingDeploymentWhileSignedInSaysToLogOut(t *testing.T) {
	runner := newFake()
	runner.responses["status"] =
		`{"serverUrl":"https://vault.bitwarden.com","lastSync":null,"status":"unlocked"}`

	_, err := New(runner, "s").EnsureServer(context.Background(), "https://vault.bitwarden.eu")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "bw logout") {
		t.Fatalf("the error should say what to do: %v", err)
	}
	if !strings.Contains(err.Error(), "vault.bitwarden.eu") {
		t.Fatalf("the error should name the wanted deployment: %v", err)
	}
}

// Nothing declared means "leave whatever the operator configured alone".
func TestEnsureServerLeavesAnUndeclaredServerAlone(t *testing.T) {
	runner := newFake()
	changed, err := New(runner, "").EnsureServer(context.Background(), "")
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("it should not even ask: %v", runner.calls)
	}
}

func TestStatusReportsTheServer(t *testing.T) {
	runner := newFake()
	runner.responses["status"] =
		`{"serverUrl":"https://vault.bitwarden.eu","lastSync":null,"status":"locked"}`
	status, err := New(runner, "").Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != Locked || status.ServerURL != "https://vault.bitwarden.eu" {
		t.Fatalf("status = %+v", status)
	}
}

func TestDisplayServerNamesTheDefault(t *testing.T) {
	if got := displayServer(""); !strings.Contains(got, "vault.bitwarden.com") {
		t.Errorf("an unconfigured CLI should be named explicitly, got %q", got)
	}
	if got := displayServer("https://x.invalid"); got != "https://x.invalid" {
		t.Errorf("got %q", got)
	}
}

// The master password must never pass through doti. It is typed into bw,
// which owns stdin and stderr; doti only reads the session key off stdout.
func TestUnlockAdoptsTheSessionWithoutEverSeeingThePassword(t *testing.T) {
	runner := newFake()
	runner.responses["unlock --raw"] = "session-key-from-bw\n"

	client := New(runner, "")
	if client.HasSession() {
		t.Fatal("no session to start with")
	}
	if err := client.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !client.HasSession() {
		t.Fatal("the session should have been adopted")
	}

	// It got the terminal, and nothing carried a password.
	if len(runner.interactive) != 1 || runner.interactive[0] != "unlock --raw" {
		t.Fatalf("interactive calls = %v", runner.interactive)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "password") {
			t.Fatalf("a password reached argv: %q", call)
		}
	}

	// And the adopted key is what later calls use.
	if _, err := client.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.env) != 1 || runner.env[0] != "BW_SESSION=session-key-from-bw" {
		t.Fatalf("env = %v", runner.env)
	}
}

func TestUnlockRejectsAnEmptySession(t *testing.T) {
	runner := newFake()
	runner.responses["unlock --raw"] = "   \n"
	if err := New(runner, "").Unlock(context.Background()); err == nil {
		t.Fatal("an empty session key is not a success")
	}
}

// A failed unlock must not carry stdout into the error: on that path stdout
// *is* the session key, and an error holding it would put a live vault
// credential into a log.
func TestAFailedInteractiveCallDoesNotLeakItsOutput(t *testing.T) {
	runner := newFake()
	runner.err = errors.New("wrong password")
	runner.responses["unlock --raw"] = "a-real-session-key"

	err := New(runner, "").Unlock(context.Background())
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "a-real-session-key") {
		t.Fatalf("the session key leaked into the error: %v", err)
	}
}

func TestLoginGetsTheTerminal(t *testing.T) {
	runner := newFake()
	runner.responses["login"] = "You are logged in!"
	if err := New(runner, "").Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.interactive) != 1 || runner.interactive[0] != "login" {
		t.Fatalf("login should be interactive, got %v", runner.interactive)
	}
}

// `bw get item` searches rather than matching exactly, so asking for one name
// can be answered by an item that merely contains it - and the wrong
// credentials then land in a config file with nothing saying so.
func TestAnItemAnsweringToADifferentNameIsRefused(t *testing.T) {
	runner := newFake()
	runner.responses["get item dotfiles/mssql-envs"] = `{
		"id":"1","name":"dotfiles/example-mssql-envs","notes":"{}",
		"login":{},"fields":[]}`

	_, err := New(runner, "s").Item(context.Background(), "dotfiles/mssql-envs")
	if err == nil {
		t.Fatal("want a refusal - bw answered with a different item")
	}
	for _, want := range []string{"dotfiles/mssql-envs", "dotfiles/example-mssql-envs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should name %q: %v", want, err)
		}
	}
}

func TestAnExactNameMatchIsAcceptedRegardlessOfCase(t *testing.T) {
	runner := newFake()
	runner.responses["get item Dotfiles/MSSQL-Envs"] = `{
		"id":"1","name":"dotfiles/mssql-envs","notes":"{}","login":{},"fields":[]}`

	if _, err := New(runner, "s").Item(context.Background(), "Dotfiles/MSSQL-Envs"); err != nil {
		t.Fatalf("case should not matter: %v", err)
	}
}

// An id is exact by construction, so the name check must not apply to it.
func TestAnItemLookedUpByIDSkipsTheNameCheck(t *testing.T) {
	const id = "99ee88d2-6046-4ea7-92c2-acac464b1412"
	runner := newFake()
	runner.responses["get item "+id] = `{
		"id":"` + id + `","name":"Anything At All","notes":"{}","login":{},"fields":[]}`

	if _, err := New(runner, "s").Item(context.Background(), id); err != nil {
		t.Fatalf("an id lookup should be accepted: %v", err)
	}
}

func TestLooksLikeID(t *testing.T) {
	for input, want := range map[string]bool{
		"99ee88d2-6046-4ea7-92c2-acac464b1412": true,
		"99EE88D2-6046-4EA7-92C2-ACAC464B1412": true,
		"dotfiles/mssql-envs":                  false,
		"99ee88d2-6046-4ea7-92c2-acac464b141":  false, // too short
		"99ee88d2x6046-4ea7-92c2-acac464b1412": false, // wrong separator
		"99ee88d2-6046-4ea7-92c2-acac464b141z": false, // not hex
		"":                                     false,
	} {
		if got := looksLikeID(input); got != want {
			t.Errorf("looksLikeID(%q) = %v, want %v", input, got, want)
		}
	}
}
