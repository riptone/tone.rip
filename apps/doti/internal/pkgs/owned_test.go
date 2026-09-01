package pkgs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// askingRunner answers questions and records them, and writes a file when
// winget is handed one to write.
type askingRunner struct {
	cmds  map[string]bool
	out   map[string]string
	asked []string
	// export is what `winget export` writes to the path it is given.
	export string
	// exportErr is what it returns *after* writing, which is winget's normal
	// behaviour on a machine holding a package no source has a manifest for.
	exportErr error
	fail      map[string]error
}

func (r *askingRunner) Run(context.Context, string, ...string) error { return nil }
func (r *askingRunner) Look(name string) bool                        { return r.cmds[name] }
func (r *askingRunner) HasApp(string) bool                           { return false }

func (r *askingRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	invocation := name
	for _, arg := range args {
		invocation += " " + arg
	}
	r.asked = append(r.asked, invocation)
	if err := r.fail[name]; err != nil {
		return nil, err
	}
	if name == "winget" {
		for i, arg := range args {
			if arg == "--output" && i+1 < len(args) {
				if err := os.WriteFile(args[i+1], []byte(r.export), 0o600); err != nil {
					return nil, err
				}
				return nil, r.exportErr
			}
		}
	}
	return []byte(r.out[invocation]), nil
}

func TestOwnedReadsBrewsFormulaeAndCasks(t *testing.T) {
	runner := &askingRunner{
		cmds: map[string]bool{"brew": true},
		out: map[string]string{
			"brew list --formula -1": "jq\nfd\nripgrep\n",
			// Two namespaces, and the manifest draws from both.
			"brew list --cask -1": "ghostty\n",
		},
	}
	owned, err := Owned(context.Background(), manifest.MacOS, runner)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"jq", "fd", "ripgrep", "ghostty"} {
		if !owned[want] {
			t.Errorf("%s is missing from %v", want, owned)
		}
	}
	if owned["node"] {
		t.Errorf("something brew never listed is owned: %v", owned)
	}
}

// The whole point of the exercise: `command -v jq` and `brew list` disagree on a
// Mac, because macOS ships /usr/bin/jq. Owned answers the second question only.
func TestOwnedIgnoresWhatIsMerelyOnPath(t *testing.T) {
	runner := &askingRunner{
		cmds: map[string]bool{"brew": true, "jq": true},
		out:  map[string]string{"brew list --formula -1": "fd\n"},
	}
	owned, err := Owned(context.Background(), manifest.MacOS, runner)
	if err != nil {
		t.Fatal(err)
	}
	if owned["jq"] {
		t.Error("a jq that is only on PATH is reported as brew's")
	}
}

// Linux has no casks, and `brew list --cask` exits non-zero there. That is not
// a failure to report.
func TestOwnedSkipsCasksOffMacOS(t *testing.T) {
	runner := &askingRunner{
		cmds: map[string]bool{"brew": true},
		out:  map[string]string{"brew list --formula -1": "fd\n"},
	}
	owned, err := Owned(context.Background(), manifest.Linux, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !owned["fd"] {
		t.Errorf("owned = %v", owned)
	}
	for _, asked := range runner.asked {
		if asked == "brew list --cask -1" {
			t.Error("casks were asked about on Linux")
		}
	}
}

// A machine without brew owns nothing, which is true rather than an error.
func TestOwnedWithoutAPackageManager(t *testing.T) {
	owned, err := Owned(context.Background(), manifest.MacOS, &askingRunner{cmds: map[string]bool{}})
	if err != nil {
		t.Fatalf("no brew is not a failure: %v", err)
	}
	if len(owned) != 0 {
		t.Errorf("owned = %v", owned)
	}
}

// A brew that answers with an error is different from a brew that is not there,
// and reporting nothing owned would silently empty the removal list.
func TestOwnedReportsABrokenBrew(t *testing.T) {
	runner := &askingRunner{
		cmds: map[string]bool{"brew": true},
		fail: map[string]error{"brew": errors.New("Error: Not a git repository")},
	}
	if _, err := Owned(context.Background(), manifest.MacOS, runner); err == nil {
		t.Fatal("a brew that cannot answer should not read as 'nothing installed'")
	}
}

func TestOwnedReadsTheWingetExport(t *testing.T) {
	runner := &askingRunner{
		cmds: map[string]bool{"winget": true},
		export: `{"Sources":[{"Packages":[
			{"PackageIdentifier":"jqlang.jq"},
			{"PackageIdentifier":"sharkdp.fd"}]}]}`,
	}
	owned, err := Owned(context.Background(), manifest.Windows, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !owned["jqlang.jq"] || !owned["sharkdp.fd"] {
		t.Errorf("owned = %v", owned)
	}
	if len(owned) != 2 {
		t.Errorf("owned = %v", owned)
	}
}

// winget exits non-zero when an installed package has no manifest in any
// source, having written the file anyway - which is the normal state of a
// Windows machine. The file is what gets checked, not the exit status.
func TestOwnedTrustsTheExportFileOverWingetsExitStatus(t *testing.T) {
	runner := &askingRunner{
		cmds:      map[string]bool{"winget": true},
		export:    `{"Sources":[{"Packages":[{"PackageIdentifier":"jqlang.jq"}]}]}`,
		exportErr: errors.New("exit status 1"),
	}
	owned, err := Owned(context.Background(), manifest.Windows, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !owned["jqlang.jq"] {
		t.Errorf("owned = %v", owned)
	}
}

func TestOwnedRejectsAnUnreadableExport(t *testing.T) {
	runner := &askingRunner{cmds: map[string]bool{"winget": true}, export: "not json"}
	if _, err := Owned(context.Background(), manifest.Windows, runner); err == nil {
		t.Fatal("a malformed export should not read as 'nothing installed'")
	}
}

// The export is an inventory of somebody's machine. It does not stay in the
// temp directory.
func TestTheWingetExportFileIsRemoved(t *testing.T) {
	runner := &askingRunner{cmds: map[string]bool{"winget": true}, export: `{"Sources":[]}`}
	if _, err := Owned(context.Background(), manifest.Windows, runner); err != nil {
		t.Fatal(err)
	}

	var written string
	for _, asked := range runner.asked {
		fields := strings.Fields(asked)
		for i, field := range fields {
			if field == "--output" && i+1 < len(fields) {
				written = fields[i+1]
			}
		}
	}
	if written == "" {
		t.Fatal("winget was never handed a path")
	}
	if _, err := os.Stat(written); err == nil {
		t.Errorf("%s is still there", written)
	}
}

func TestNpmGlobalsStatsTheGlobalRoot(t *testing.T) {
	root := t.TempDir()
	// npm lays a scoped package out as two directories.
	if err := os.MkdirAll(filepath.Join(root, "@modelcontextprotocol", "server-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "plain-package"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &askingRunner{
		cmds: map[string]bool{"npm": true},
		out:  map[string]string{"npm root -g": root + "\n"},
	}
	want := []string{"@modelcontextprotocol/server-a", "plain-package", "@modelcontextprotocol/server-b"}
	present, err := NpmGlobals(context.Background(), runner, want)
	if err != nil {
		t.Fatal(err)
	}
	if !present["@modelcontextprotocol/server-a"] || !present["plain-package"] {
		t.Errorf("present = %v", present)
	}
	if present["@modelcontextprotocol/server-b"] {
		t.Errorf("something absent was reported present: %v", present)
	}
}

// One invocation for the root, then stats. `npm ls -g` walks the whole global
// tree at ~700ms, which is why the selector used to decline to say.
func TestNpmGlobalsAsksNpmOnce(t *testing.T) {
	runner := &askingRunner{
		cmds: map[string]bool{"npm": true},
		out:  map[string]string{"npm root -g": t.TempDir()},
	}
	if _, err := NpmGlobals(context.Background(), runner, []string{"a", "b", "c", "d"}); err != nil {
		t.Fatal(err)
	}
	if len(runner.asked) != 1 {
		t.Errorf("asked npm %d times: %v", len(runner.asked), runner.asked)
	}
}

func TestNpmGlobalsWithoutNpm(t *testing.T) {
	present, err := NpmGlobals(context.Background(),
		&askingRunner{cmds: map[string]bool{}}, []string{"a"})
	if err != nil {
		t.Fatalf("no npm is not a failure: %v", err)
	}
	if len(present) != 0 {
		t.Errorf("present = %v", present)
	}
}

func TestNpmGlobalsWithNothingDeclared(t *testing.T) {
	runner := &askingRunner{cmds: map[string]bool{"npm": true}}
	present, err := NpmGlobals(context.Background(), runner, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(present) != 0 || len(runner.asked) != 0 {
		t.Errorf("present=%v asked=%v", present, runner.asked)
	}
}

func TestNpmGlobalsReportsABrokenNpm(t *testing.T) {
	runner := &askingRunner{
		cmds: map[string]bool{"npm": true},
		fail: map[string]error{"npm": errors.New("EACCES")},
	}
	if _, err := NpmGlobals(context.Background(), runner, []string{"a"}); err == nil {
		t.Fatal("an npm that cannot answer should not read as 'nothing installed'")
	}
}

// ExecRunner.Output is the one thing here that talks to a real binary, so it is
// exercised against real ones.
func TestExecRunnerOutputCapturesStdout(t *testing.T) {
	out, err := ExecRunner{}.Output(context.Background(), "printf", "jq\nfd\n")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "jq\nfd\n" {
		t.Errorf("out = %q", out)
	}
	// And it parses into the shape brewOwned expects.
	if set := names(out); !set["jq"] || !set["fd"] || len(set) != 2 {
		t.Errorf("names(%q) = %v", out, set)
	}
}

// The error carries what the command said on stderr, indented as evidence -
// which is the whole reason stderr is kept back rather than discarded.
func TestExecRunnerOutputPutsStderrInTheError(t *testing.T) {
	_, err := ExecRunner{}.Output(context.Background(), "sh", "-c",
		"echo 'Error: Not a git repository' >&2; exit 1")
	if err == nil {
		t.Fatal("a failing command should be an error")
	}
	if !strings.Contains(err.Error(), "Not a git repository") {
		t.Errorf("the error does not carry what the command said: %v", err)
	}
	if !strings.Contains(err.Error(), "sh -c") {
		t.Errorf("the error does not say what was run: %v", err)
	}
}

// A silent failure still names the invocation, because "exit status 1" on its
// own is not a diagnosis.
func TestExecRunnerOutputNamesASilentFailure(t *testing.T) {
	_, err := ExecRunner{}.Output(context.Background(), "sh", "-c", "exit 3")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "sh -c exit 3") {
		t.Errorf("the error does not say what was run: %v", err)
	}
}

// r.Out is deliberately ignored: these are questions, not steps, and echoing
// `brew list` into the log under every menu open would bury the lines that say
// what doti did.
func TestExecRunnerOutputDoesNotEchoToOut(t *testing.T) {
	var log strings.Builder
	out, err := ExecRunner{Out: &log}.Output(context.Background(), "printf", "jq\n")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "jq\n" {
		t.Errorf("out = %q", out)
	}
	if log.String() != "" {
		t.Errorf("the inventory was echoed into the log: %q", log.String())
	}
}

// An npm that answers with nothing is not an npm with nothing installed - there
// is simply no root to stat against, so nothing can be claimed either way.
func TestNpmGlobalsWithAnEmptyRoot(t *testing.T) {
	runner := &askingRunner{
		cmds: map[string]bool{"npm": true},
		out:  map[string]string{"npm root -g": "  \n"},
	}
	present, err := NpmGlobals(context.Background(), runner, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(present) != 0 {
		t.Errorf("present = %v", present)
	}
}

// A manifest may name a formula tap-qualified, and `brew list` never gives that
// spelling back - it prints the Cellar's short names. Formula is the bridge, and
// it has to leave an unqualified name and a winget identifier untouched.
func TestFormulaStripsTheTap(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"anomalyco/tap/opencode", "opencode"},
		{"oven-sh/bun/bun", "bun"},
		{"bun", "bun"},
		{"", ""},
		// Windows identifiers come through the same lookup and carry dots, not
		// slashes. Untouched, or Removable would stop matching them.
		{"Oven-sh.Bun", "Oven-sh.Bun"},
		{"SST.opencode", "SST.opencode"},
		// Casks live in taps too.
		{"someone/tap/font-x", "font-x"},
	} {
		if got := Formula(tc.in); got != tc.want {
			t.Errorf("Formula(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A bun global leaves no trace in `brew list` or `winget export`. Its own
// directory is the only place that answers, and bun named that directory itself:
// `bun pm bin -g` on a machine that has never installed one replies
// `No package.json was found for directory "<home>/.bun/install/global"`.
func TestBunGlobalsReadsBunsOwnDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUN_INSTALL", "")
	root := filepath.Join(home, ".bun", "install", "global", "node_modules")
	// A scoped name is two directories deep, exactly as npm lays it out.
	for _, pkg := range []string{"opencode-ai", filepath.Join("@scope", "thing")} {
		if err := os.MkdirAll(filepath.Join(root, pkg), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := BunGlobals(home, []string{"opencode-ai", "@scope/thing", "absent"})
	if !got["opencode-ai"] || !got["@scope/thing"] {
		t.Errorf("BunGlobals = %v", got)
	}
	if got["absent"] {
		t.Errorf("something bun never installed is present: %v", got)
	}
}

// BUN_INSTALL moves the prefix, and bun honours it, so this has to.
func TestBunGlobalsHonoursBunInstall(t *testing.T) {
	elsewhere := t.TempDir()
	t.Setenv("BUN_INSTALL", elsewhere)
	if err := os.MkdirAll(filepath.Join(elsewhere, "install", "global",
		"node_modules", "opencode-ai"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A home that holds nothing, to prove the env is what was read.
	if got := BunGlobals(t.TempDir(), []string{"opencode-ai"}); !got["opencode-ai"] {
		t.Errorf("BUN_INSTALL was ignored: %v", got)
	}
}

// A machine that has never used bun globally has no such directory, which is
// "none installed" rather than a failure - there is no error to return here.
func TestBunGlobalsWithoutABunDirectory(t *testing.T) {
	t.Setenv("BUN_INSTALL", "")
	if got := BunGlobals(t.TempDir(), []string{"opencode-ai"}); len(got) != 0 {
		t.Errorf("BunGlobals = %v", got)
	}
}
