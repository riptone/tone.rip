package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
)

// The rule these two guard is small and was wrong in a way no test could see,
// because the wrong version was one expression inside build(). It is a
// function now so it can be asked directly.
func TestCanPromptNeedsBothStreams(t *testing.T) {
	// A pipe is what `curl ... | bash` hands to the process it execs, and
	// what a test can create. /dev/null is the CI shape.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { devNull.Close() })

	regular, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { regular.Close() })

	for _, tc := range []struct {
		name          string
		stdout, stdin *os.File
	}{
		{"piped stdin, terminal stdout is the curl|bash case", regular, r},
		{"stdin from /dev/null is not a person", regular, devNull},
		{"both piped", w, r},
		{"a file for stdout", regular, regular},
		{"nil streams", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if canPrompt(tc.stdout, tc.stdin) {
				t.Error("canPrompt = true; nothing here can answer a question")
			}
		})
	}
}

// /dev/null is a character device, so the Stat().Mode()&os.ModeCharDevice
// test that shipped called it a terminal - which is how an unattended
// `doti install </dev/null` went looking for a vault password. Pinned
// separately from the table above because it is the specific regression.
func TestDevNullIsNotATerminal(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()

	info, err := devNull.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		t.Skip("this platform does not make /dev/null a character device, so the old bug could not occur")
	}
	if isTerminal(devNull) {
		t.Error("isTerminal(/dev/null) = true; the ModeCharDevice heuristic is back")
	}
}

// `doti --repo ~/dotfiles` took "--repo" as the command name, matched nothing,
// and printed the usage - which reads as "that flag does not exist" rather than
// "name a command first".
func TestALeadingFlagIsAFlagNotACommand(t *testing.T) {
	for _, args := range [][]string{
		{"--repo", "/nowhere"},
		{"-n"},
		{"--tui"},
	} {
		if err := run(args); err != nil && strings.Contains(err.Error(), "unknown command") {
			t.Errorf("run(%v) read the flag as a command: %v", args, err)
		}
	}
}

// And a real command still is one.
func TestAKnownCommandIsStillACommand(t *testing.T) {
	if err := run([]string{"version"}); err != nil {
		t.Errorf("run([version]) = %v", err)
	}
	err := run([]string{"dance"})
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("run([dance]) = %v, want an unknown-command error", err)
	}
}

// ------------------------------------------------------------ the two choices --

// The rendering is chosen once, from whether anything is watching.
func TestReporterPicksPlainWhenNothingIsWatching(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	if _, ok := reporter(file).(app.PlainReporter); !ok {
		t.Errorf("a file got %T, want plain lines", reporter(file))
	}

	// A terminal gets colour and a spinner.
	if _, ok := reporterFor(file, true).(*app.LiveReporter); !ok {
		t.Errorf("a terminal got %T, want the live reporter", reporterFor(file, true))
	}

	// NO_COLOR is set by people who mean it, so it wins even on a terminal.
	t.Setenv("NO_COLOR", "1")
	if _, ok := reporterFor(file, true).(app.PlainReporter); !ok {
		t.Error("NO_COLOR did not force plain lines on a terminal")
	}
}

func TestDefaultRepo(t *testing.T) {
	t.Setenv("DOTFILES_DIR", "/somewhere/else")
	if got := defaultRepo(); got != "/somewhere/else" {
		t.Errorf("defaultRepo = %q, want the environment's answer", got)
	}

	t.Setenv("DOTFILES_DIR", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory to fall back to")
	}
	if got, want := defaultRepo(), filepath.Join(home, "dotfiles"); got != want {
		t.Errorf("defaultRepo = %q, want %q", got, want)
	}
}

// The screenshot path, which is also the only way to look at a change without
// installing anything.
func TestPreviewDumpsEveryScreen(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "dotfiles")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"app":"dotfiles","version":"1.0.0","stow_packages":[],` +
		`"stow_ignore":[],"tools":[],"health":{}}`
	if err := os.WriteFile(filepath.Join(repo, "manifest.jsonc"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	frames := filepath.Join(dir, "frames")
	if err := run([]string{"preview", "--frames", frames, "--repo", repo}); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(frames)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 6 {
		t.Fatalf("wrote %d frames, want the menu, the offer, the selector and a run", len(entries))
	}
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(frames, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		// The trap this path exists to avoid: a renderer bound to a file
		// resolves to Ascii and the capture comes out monochrome.
		if !strings.Contains(string(body), "\x1b[") {
			t.Errorf("%s has no colour in it", entry.Name())
		}
	}
}

// The window needs a terminal to drive it. Reachable only when something asked
// for it explicitly - and it must say what to do instead.
func TestTheWindowRefusesWithoutATerminal(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "dotfiles")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"app":"dotfiles","version":"1.0.0","stow_packages":[],` +
		`"stow_ignore":[],"tools":[],"health":{}}`
	if err := os.WriteFile(filepath.Join(repo, "manifest.jsonc"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	// `doti` with no arguments is the window, and under `go test` stdin and
	// stdout are not terminals.
	err := run([]string{"menu", "--repo", repo})
	if err == nil {
		t.Fatal("the window opened without a terminal")
	}
	if !strings.Contains(err.Error(), "--term") {
		t.Errorf("the error does not name the way out: %v", err)
	}
}

// ------------------------------------------------------------------- help --

// A regression from the leading-flag rule: after it, `--help` is not a command,
// and the flag package answered it with its own flag list and a non-zero exit.
// Both spellings have to reach the paragraph that names the commands.
func TestEverySpellingOfHelpPrintsTheUsage(t *testing.T) {
	for _, args := range [][]string{
		{"help"}, {"-h"}, {"--help"},
		// On a command too, which is where the flag package used to win.
		{"install", "--help"}, {"check", "-h"},
	} {
		if err := run(args); err != nil {
			t.Errorf("run(%v) = %v, want the usage and no error", args, err)
		}
	}
}

func TestEverySpellingOfVersionAnswers(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"-v"}, {"--version"}} {
		if err := run(args); err != nil {
			t.Errorf("run(%v) = %v", args, err)
		}
	}
}

// It has to answer before a repository is looked for: `doti --version` on a
// machine with no dotfiles checkout should still say what it is.
func TestVersionAndHelpDoNotNeedARepository(t *testing.T) {
	t.Setenv("DOTFILES_DIR", filepath.Join(t.TempDir(), "definitely-not-here"))
	for _, args := range [][]string{{"--version"}, {"--help"}} {
		if err := run(args); err != nil {
			t.Errorf("run(%v) = %v", args, err)
		}
	}
}

// The usage text is the contract: every command it lists has to be one, and
// every command has to be listed.
func TestTheUsageListsEveryCommand(t *testing.T) {
	commands := []string{
		"install", "adopt", "check", "link", "unlink", "sync", "update",
		"secrets", "upgrade", "packages", "validate", "preview", "version",
	}
	for _, name := range commands {
		if !strings.Contains(usage, "doti "+name) {
			t.Errorf("%q is a command but is not in the usage text", name)
		}
	}
	if !strings.Contains(usage, "--term") {
		t.Error("the usage text does not mention --term")
	}
	if strings.Contains(usage, "--tui") {
		t.Error("the usage text still mentions --tui")
	}
}

// An unknown command says so and shows the list, rather than failing silently.
func TestAnUnknownCommandSaysWhatIsAvailable(t *testing.T) {
	err := run([]string{"instal"})
	if err == nil {
		t.Fatal("a typo was accepted")
	}
	if !strings.Contains(err.Error(), "instal") {
		t.Errorf("the error does not quote what was typed: %v", err)
	}
}
