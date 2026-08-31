package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
	"github.com/riptone/tone.rip/apps/doti/internal/tui"
)

// Which of the two renderings a run gets, and why.

func TestWantsWindow(t *testing.T) {
	for _, tc := range []struct {
		name        string
		opts        options
		interactive bool
		want        bool
	}{
		// The default now: somebody is watching, and the window is strictly
		// more informative than lines.
		{"a terminal gets the window", options{}, true, true},
		// Decided here rather than asked for: an alt screen in a log is
		// thousands of cursor movements and no output.
		{"a pipe gets lines", options{}, false, false},
		// The escape hatch that replaced --tui, pointing the other way.
		{"--term overrides a terminal", options{term: true}, true, false},
		{"--term on a pipe is already true", options{term: true}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wantsWindow(tc.opts, tc.interactive); got != tc.want {
				t.Errorf("wantsWindow = %v, want %v", got, tc.want)
			}
		})
	}
}

// The alt screen is discarded on exit, and the first thing anybody does with a
// failed install is scroll back through it. What lands in the scrollback has to
// be what --term would have printed, which is why this replays through the same
// reporter rather than a second renderer that agrees with it.
func TestReplayWritesWhatThePlainPathWouldHave(t *testing.T) {
	records := []app.Record{
		{Kind: "phase", Text: "configs"},
		{Kind: "line", Mark: app.MarkChange, Text: "zsh        linked 4"},
		{Kind: "line", Mark: app.MarkWarn, Text: "ghostty: backing up ~/.config/ghostty"},
		{Kind: "summary", Text: "1 changed"},
	}

	var replayed bytes.Buffer
	replay(records, &replayed)

	// The same events straight through the reporter.
	var direct bytes.Buffer
	reporter := app.PlainReporter{Out: &direct}
	reporter.Phase("configs")
	reporter.Line(app.MarkChange, "zsh        linked 4")
	reporter.Line(app.MarkWarn, "ghostty: backing up ~/.config/ghostty")
	reporter.Summary("1 changed")

	if replayed.String() != direct.String() {
		t.Errorf("a replayed run differs from a printed one:\n%q\n%q",
			replayed.String(), direct.String())
	}
	for _, want := range []string{"configs", "linked 4", "backing up", "1 changed"} {
		if !strings.Contains(replayed.String(), want) {
			t.Errorf("the replay is missing %q", want)
		}
	}
}

// Quitting a menu you were only browsing should not fill the terminal.
func TestReplayingNothingWritesNothing(t *testing.T) {
	var out bytes.Buffer
	replay(nil, &out)
	if out.Len() != 0 {
		t.Errorf("an empty run wrote %q", out.String())
	}
}

// One name, one operation, one call - so `doti install`, the window's Install
// and a future caller cannot drift.
func TestEveryOperationInTheTableIsRealAndDocumented(t *testing.T) {
	for name, op := range operations {
		if op == "" {
			t.Errorf("%q maps to no operation", name)
		}
		if !strings.Contains(usage, "doti "+name) {
			t.Errorf("%q is dispatchable but not in the usage text", name)
		}
	}
	// The two that are deliberately absent, because each has a flag that is
	// the command's own.
	for _, absent := range []string{"check", "unlink"} {
		if _, ok := operations[absent]; ok {
			t.Errorf("%q is in the table; its flag would be dropped", absent)
		}
	}
}

// --tui is gone. A stale flag in a script should fail loudly rather than being
// swallowed as an unknown command or ignored.
func TestTuiIsNoLongerAFlag(t *testing.T) {
	err := run([]string{"check", "--tui"})
	if err == nil {
		t.Fatal("--tui was accepted")
	}
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Errorf("err = %v, want a flag-parsing failure naming it", err)
	}
}

func TestTermIsAFlagOnEveryOperation(t *testing.T) {
	for name := range operations {
		// Parsing only: --repo points nowhere, so nothing runs far enough to
		// touch the machine. What matters is that the flag is accepted.
		err := run([]string{name, "--term", "--repo", t.TempDir(), "-n"})
		if err != nil && strings.Contains(err.Error(), "not defined") {
			t.Errorf("%s rejected --term: %v", name, err)
		}
	}
}

// ---------------------------------------------------------- the update check --

func TestUpdateCheckerOffersOnlyWhatIsNewer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"tag_name":"doti/v9.9.9"},{"tag_name":"doti/v0.0.1"}]`))
	}))
	defer srv.Close()

	check := updateChecker(app.Releases{BaseURL: srv.URL})

	// A stamped build behind the newest release is offered it.
	version = "v0.1.0"
	got, err := check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "v9.9.9" {
		t.Errorf("offer = %q, want v9.9.9", got)
	}

	// A working copy must not be told to replace the binary being worked on.
	version = "dev"
	if got, err := check(context.Background()); err != nil || got != "" {
		t.Errorf("a dev build was offered %q (err %v)", got, err)
	}

	// Nor may a build that is already newer than anything published.
	version = "v99.0.0"
	if got, err := check(context.Background()); err != nil || got != "" {
		t.Errorf("a newer build was offered %q (err %v)", got, err)
	}
	version = "dev"
}

// A failure is silence, and the window turns that into no footer offer. What it
// must not do is invent a version.
func TestUpdateCheckerReportsAFailureWithoutAVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	version = "v0.1.0"
	defer func() { version = "dev" }()
	got, err := updateChecker(app.Releases{BaseURL: srv.URL})(context.Background())
	if err == nil {
		t.Error("a rate-limited check should report the failure")
	}
	if got != "" {
		t.Errorf("it offered %q anyway", got)
	}
}

// ------------------------------------------------------- the operation bridge --

// The window can run several operations in one session, and Preview sets
// DryRun. A run that mutated the App the window was built from would leave the
// next Install writing nothing.
func TestEachRunGetsItsOwnCopyOfTheApp(t *testing.T) {
	instance := &app.App{Report: &app.Recorder{}}
	runner := operationRunner(instance, options{dryRun: true})

	// An operation nothing answers to: the copy is made and configured before
	// the switch refuses it, which is exactly the window this tests.
	err := runner(context.Background(), "dance", []string{"zsh"},
		tui.RunOptions{Report: &app.Recorder{}})
	if err == nil {
		t.Fatal("want an unknown-operation error")
	}

	if instance.DryRun {
		t.Error("the run set DryRun on the shared App")
	}
	if instance.Interactive {
		t.Error("the run set Interactive on the shared App")
	}
	if instance.Include != nil {
		t.Errorf("the run left Include = %v on the shared App", instance.Include)
	}
	if instance.Vault != nil {
		t.Error("the run left a Vault runner on the shared App")
	}
}

// The two things that make the vault work inside a window: prompting is on, and
// the `bw` runner it was handed is the one used. Asserted together, because
// either alone is useless.
func TestARunPromptsThroughTheVaultRunnerItWasGiven(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
	  "app": "dotfiles", "version": "1.0.0",
	  "stow_packages": [], "stow_ignore": [], "tools": [],
	  "vault": {"server": "https://vault.bitwarden.eu"},
	  "secrets": [{"name":"creds","mode":"file","item":"dotfiles/creds","target":"~/.doti/creds.json"}],
	  "health": {}
	}`
	if err := os.WriteFile(filepath.Join(repo, "manifest.jsonc"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	platform, err := app.CurrentPlatform()
	if err != nil {
		t.Fatal(err)
	}
	recorder := &app.Recorder{}
	instance := &app.App{Repo: repo, Home: home, Platform: platform, Report: recorder}

	vault := &promptingVault{}
	runErr := operationRunner(instance, options{})(context.Background(),
		tui.Action(app.OpSecrets), nil, tui.RunOptions{Report: recorder, Vault: vault})

	if !vault.unlocked {
		t.Errorf("the vault was never asked for a password - either prompting was "+
			"off or the runner was dropped (err: %v)", runErr)
	}
	if !recorder.Contains("vault unlocked for this run") {
		t.Errorf("the unlock was not reported: %v", recorder.Texts())
	}
}

// promptingVault is a `bw` that is locked until it is asked interactively.
type promptingVault struct {
	unlocked bool
	mu       sync.Mutex
}

func (v *promptingVault) Run(_ context.Context, _ []string, args ...string) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	switch strings.Join(args, " ") {
	case "status":
		if v.unlocked {
			return []byte(`{"serverUrl":"https://vault.bitwarden.eu","status":"unlocked"}`), nil
		}
		return []byte(`{"serverUrl":"https://vault.bitwarden.eu","status":"locked"}`), nil
	case "sync":
		return []byte("Syncing complete."), nil
	case "get item dotfiles/creds":
		return []byte(`{"id":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",` +
			`"name":"dotfiles/creds","notes":"{}","revisionDate":"2026-01-01T00:00:00Z"}`), nil
	}
	return nil, errors.New("promptingVault: unexpected `bw " + strings.Join(args, " ") + "`")
}

func (v *promptingVault) RunInteractive(_ context.Context, _ []string, args ...string) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if strings.Join(args, " ") != "unlock --raw" {
		return nil, errors.New("promptingVault: unexpected interactive call")
	}
	v.unlocked = true
	return []byte("a-session-key"), nil
}
