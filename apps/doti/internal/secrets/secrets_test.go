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
	env       []string
	err       error
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
