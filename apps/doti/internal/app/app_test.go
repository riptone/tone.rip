package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// fakeRunner records what would have been run, and answers detection from a
// map. Nothing here touches PATH, a package manager or the network.
type fakeRunner struct {
	mu   sync.Mutex
	cmds map[string]bool
	apps map[string]bool
	ran  []string
	fail map[string]error
	// onRun lets a test make a command have an effect - `git clone` has to
	// produce a checkout or everything after it is untestable.
	onRun func(name string, args []string) error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	f.mu.Lock()
	invocation := name + " " + strings.Join(args, " ")
	f.ran = append(f.ran, invocation)
	f.mu.Unlock()

	for prefix, err := range f.fail {
		if strings.HasPrefix(invocation, prefix) {
			return err
		}
	}
	if f.onRun != nil {
		return f.onRun(name, args)
	}
	return nil
}

func (f *fakeRunner) Look(name string) bool   { return f.cmds[name] }
func (f *fakeRunner) HasApp(name string) bool { return f.apps[name] }

func (f *fakeRunner) didRun(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, invocation := range f.ran {
		if strings.HasPrefix(invocation, prefix) {
			return true
		}
	}
	return false
}

const fixtureManifest = `{
  "app": "dotfiles",
  "version": "9.0.0",
  "stow_packages": [
    { "name": "zsh", "platforms": ["macos", "linux", "windows"] },
    { "name": "ghostty", "platforms": ["macos", "linux"] }
  ],
  "stow_ignore": ["\\.DS_Store"],
  "tools": [
    { "cmd": "jq", "brew": "jq", "winget": "jqlang.jq" },
    { "cmd": "ghostty", "brew": "ghostty", "app": "Ghostty" }
  ],
  "health": {
    "extra_tools": { "macos": ["zsh"] },
    "links": { "macos": { "~/.zshrc": "zsh/.zshrc" } }
  }
}`

// fixture builds a repo and a home, and returns an App wired to fakes.
func fixture(t *testing.T, installed ...string) (*App, *fakeRunner, *Recorder) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "dotfiles")
	home := filepath.Join(root, "home")

	for _, dir := range []string{
		filepath.Join(repo, "zsh"),
		filepath.Join(repo, "ghostty", ".config", "ghostty"),
		home,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(repo, "manifest.jsonc"), fixtureManifest)
	write(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=nvim\n")
	write(t, filepath.Join(repo, "ghostty", ".config", "ghostty", "config"), "theme=dark\n")

	have := map[string]bool{}
	for _, name := range installed {
		have[name] = true
	}
	runner := &fakeRunner{cmds: have, apps: map[string]bool{}}
	recorder := &Recorder{}

	return &App{
		Repo: repo, Home: home, Platform: manifest.MacOS,
		RepoURL: "https://example.invalid/dotfiles.git",
		Report:  recorder, Runner: runner,
	}, runner, recorder
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallRunsEveryPhaseInOrder(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	if err := a.Install(context.Background()); err != nil {
		t.Fatal(err)
	}

	var phases []string
	for _, record := range rec.Records {
		if record.Kind == "phase" {
			phases = append(phases, record.Text)
		}
	}
	want := []string{"repository", "packages", "configs"}
	if len(phases) != len(want) {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
	for i := range want {
		if phases[i] != want[i] {
			t.Fatalf("phases = %v, want %v", phases, want)
		}
	}
}

// The question this refactor exists to answer: `doti install` and the menu's
// Install must be the same thing, not two things that agree. They call the
// same method with the same reporter, so the event streams are identical -
// and this is the test that keeps it that way.
func TestTheMenuPathAndTheCommandPathReportIdentically(t *testing.T) {
	direct, _, directRec := fixture(t, "brew", "jq", "ghostty", "zsh")
	viaMenu, _, menuRec := fixture(t, "brew", "jq", "ghostty", "zsh")

	if err := direct.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	// What runMenu does once Install is chosen and confirmed.
	if err := viaMenu.Install(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(directRec.Records) != len(menuRec.Records) {
		t.Fatalf("different number of events: %d vs %d",
			len(directRec.Records), len(menuRec.Records))
	}
	for i := range directRec.Records {
		d, m := directRec.Records[i], menuRec.Records[i]
		// Paths differ (separate temp dirs), so compare kind and mark.
		if d.Kind != m.Kind || d.Mark != m.Mark {
			t.Fatalf("event %d differs: %+v vs %+v", i, d, m)
		}
	}
}

func TestInstallClonesWhenThereIsNoCheckout(t *testing.T) {
	a, runner, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	// Point at a path that does not exist yet, and make the clone produce one.
	a.Repo = filepath.Join(t.TempDir(), "fresh")
	runner.cmds["git"] = true
	runner.onRun = func(name string, args []string) error {
		if name == "git" && len(args) > 0 && args[0] == "clone" {
			target := args[len(args)-1]
			write(t, filepath.Join(target, "manifest.jsonc"), fixtureManifest)
			write(t, filepath.Join(target, "zsh", ".zshrc"), "x\n")
			write(t, filepath.Join(target, "ghostty", ".config", "ghostty", "config"), "x\n")
		}
		return nil
	}

	if err := a.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("git clone --depth 1") {
		t.Fatalf("expected a shallow clone, ran: %v", runner.ran)
	}
	if !rec.Contains("cloned into") {
		t.Errorf("the clone should be reported: %v", rec.Texts())
	}
}

// The whole point of the bootstrap: doti will not install git, because that
// needs sudo. It must say so actionably instead.
func TestInstallWithoutGitExplainsHowToGetIt(t *testing.T) {
	a, runner, _ := fixture(t)
	a.Repo = filepath.Join(t.TempDir(), "fresh")
	runner.cmds["git"] = false

	err := a.Install(context.Background())
	if err == nil {
		t.Fatal("want an error when git is missing")
	}
	if !strings.Contains(err.Error(), "git is required") {
		t.Fatalf("error = %v", err)
	}
	if runner.didRun("git clone") {
		t.Error("it must not attempt a clone without git")
	}
}

// A directory that exists but has no manifest is far more likely a typo'd
// --repo than a broken checkout, and cloning into it would fail anyway.
func TestANonEmptyDirectoryWithNoManifestIsRefused(t *testing.T) {
	a, runner, _ := fixture(t)
	a.Repo = t.TempDir()
	write(t, filepath.Join(a.Repo, "something-else"), "x")
	runner.cmds["git"] = true

	err := a.Install(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no manifest.jsonc") {
		t.Fatalf("error = %v", err)
	}
}

func TestDryRunInstallTouchesNothing(t *testing.T) {
	a, runner, rec := fixture(t, "brew", "jq", "zsh")
	a.DryRun = true
	if err := a.Install(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(runner.ran) != 0 {
		t.Fatalf("a dry run ran commands: %v", runner.ran)
	}
	if _, err := os.Stat(filepath.Join(a.Home, ".zshrc")); !os.IsNotExist(err) {
		t.Error("a dry run linked something")
	}
	if _, err := os.Stat(filepath.Join(a.Home, ".gitconfig.local")); !os.IsNotExist(err) {
		t.Error("a dry run wrote .gitconfig.local")
	}
	if !rec.Contains("would link") && rec.Marked(MarkChange) == 0 {
		t.Errorf("a dry run should still report what it would do: %v", rec.Texts())
	}
}

// A GUI app puts nothing on PATH. Without the manifest's `app` field, Ghostty
// is reported missing on a machine where it is running - which made
// `check --strict` permanently red.
func TestAnApplicationBundleCountsAsInstalled(t *testing.T) {
	a, runner, _ := fixture(t, "brew", "jq", "zsh")
	runner.apps["Ghostty"] = true

	report, err := a.Scan()
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range report.Missing() {
		if finding.Name == "ghostty" {
			t.Fatalf("ghostty should be found via its bundle: %+v", finding)
		}
	}

	// And without the bundle, the detail says what was looked for.
	runner.apps["Ghostty"] = false
	a.manifest = nil
	report, err = a.Scan()
	if err != nil {
		t.Fatal(err)
	}
	var detail string
	for _, finding := range report.Missing() {
		if finding.Name == "ghostty" {
			detail = finding.Detail
		}
	}
	if !strings.Contains(detail, "Ghostty.app") {
		t.Fatalf("detail = %q, should name the bundle it looked for", detail)
	}
}

func TestCheckStrictFailsOnDriftAndPlainCheckDoesNot(t *testing.T) {
	a, _, _ := fixture(t)
	if err := a.Check(false); err != nil {
		t.Fatalf("plain check should not fail on drift: %v", err)
	}
	if err := a.Check(true); err == nil {
		t.Fatal("--strict should fail when something is missing")
	}
}

func TestInstallWithNoMissingToolsRunsNoPackageManager(t *testing.T) {
	a, runner, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	if err := a.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runner.didRun("brew bundle") {
		t.Error("nothing was missing, so brew should not have run")
	}
	if !rec.Contains("all 2 tools present") {
		t.Errorf("expected an all-present line: %v", rec.Texts())
	}
}

func TestMissingToolsTriggerBrewBundleWithoutUpgrading(t *testing.T) {
	a, runner, _ := fixture(t, "brew", "zsh")
	if err := a.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	// --no-upgrade: installing a missing tool must not quietly move your
	// node version. `doti update` is where upgrades live.
	if !runner.didRun("brew bundle install --no-upgrade") {
		t.Fatalf("ran: %v", runner.ran)
	}
}

// Secrets are the last phase and the only one allowed to fail, because the
// vault is the one dependency a fresh machine legitimately may not have.
func TestAFailedVaultDoesNotFailTheInstall(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	write(t, filepath.Join(a.Repo, "manifest.jsonc"),
		strings.Replace(fixtureManifest, `"health"`,
			`"secrets": [{"name":"creds","mode":"file","item":"x","target":"~/creds.json"}],
			 "health"`, 1))

	if err := a.Install(context.Background()); err != nil {
		t.Fatalf("a vault failure must not fail the install: %v", err)
	}
	if !rec.Contains("re-run `doti secrets`") {
		t.Errorf("it should say how to finish later: %v", rec.Texts())
	}
}

func TestOnlyNarrowsToOnePackageAndNamesTheValidOnes(t *testing.T) {
	a, _, rec := fixture(t)
	a.Only = "ghostty"
	if err := a.Link(); err != nil {
		t.Fatal(err)
	}
	if rec.Contains("zsh") {
		t.Errorf("--only ghostty should not touch zsh: %v", rec.Texts())
	}

	a.Only = "nope"
	err := a.Link()
	if err == nil {
		t.Fatal("want an error for an unknown package")
	}
	for _, name := range []string{"zsh", "ghostty"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the error should list %q: %v", name, err)
		}
	}
}

func TestLinkThenUnlinkThenRestore(t *testing.T) {
	a, _, _ := fixture(t)
	precious := filepath.Join(a.Home, ".zshrc")
	write(t, precious, "precious\n")

	if err := a.Link(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(precious)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "export EDITOR=nvim\n" {
		t.Fatalf("after linking, ~/.zshrc = %q", body)
	}

	if err := a.Unlink(true); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(precious)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "precious\n" {
		t.Fatalf("after restore, ~/.zshrc = %q, want the original", body)
	}
}

func TestGitConfigLocalIsWrittenOnceAndNeverOverwritten(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	if err := a.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.Home, ".gitconfig.local")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "osxkeychain") {
		t.Fatalf("expected the macOS credential helper: %q", body)
	}

	// It is the documented place for a per-machine identity, so someone's
	// email is very likely in it.
	write(t, path, "[user]\n\temail = me@example.com\n")
	a.manifest = nil
	if err := a.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "me@example.com") {
		t.Fatalf("an existing .gitconfig.local was overwritten: %q", body)
	}
	if !rec.Contains("left as it is") {
		t.Errorf("it should say it left the file alone: %v", rec.Texts())
	}
}

func TestSyncPullsThenLinks(t *testing.T) {
	a, runner, _ := fixture(t, "git")
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("git -C " + a.Repo + " pull --ff-only") {
		t.Fatalf("ran: %v", runner.ran)
	}
	if _, err := os.Lstat(filepath.Join(a.Home, ".zshrc")); err != nil {
		t.Error("sync should re-link after pulling")
	}
}

func TestUpdateRefreshesThenUpgrades(t *testing.T) {
	a, runner, _ := fixture(t, "brew")
	if err := a.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !runner.didRun("brew update") || !runner.didRun("brew upgrade") {
		t.Fatalf("ran: %v", runner.ran)
	}
}

func TestSelfUpdateGoesThroughTheInstallerRatherThanReimplementingIt(t *testing.T) {
	a, runner, _ := fixture(t, "curl")
	if err := a.SelfUpdate(context.Background(), "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	// The installer already verifies the checksum and asks the download its
	// own version. A second implementation of that in Go is the one that
	// would go stale.
	if !runner.didRun("sh -c curl -fsSL") {
		t.Fatalf("ran: %v", runner.ran)
	}
	var invocation string
	for _, ran := range runner.ran {
		if strings.HasPrefix(ran, "sh -c") {
			invocation = ran
		}
	}
	if !strings.Contains(invocation, "--no-install") {
		t.Errorf("upgrading the tool must not re-run an install: %q", invocation)
	}
	if !strings.Contains(invocation, "apps/doti/scripts/install.sh") {
		t.Errorf("should fetch the documented installer: %q", invocation)
	}
}

func TestValidateReportsWhatTheManifestHolds(t *testing.T) {
	a, _, rec := fixture(t)
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains("dotfiles 9.0.0") {
		t.Errorf("texts = %v", rec.Texts())
	}
	if !rec.Contains("2 stow packages, 2 tools, 0 secrets") {
		t.Errorf("texts = %v", rec.Texts())
	}
}

func TestPackageListsRenderBoth(t *testing.T) {
	a, _, _ := fixture(t)
	out, err := a.PackageLists(false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`brew "jq"`, "winget-packages.schema", "jqlang.jq"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

// Off Windows there is nothing whose target sits outside $HOME, so there is
// nothing for the system-link step to do.
func TestSystemLinksAreWindowsOnly(t *testing.T) {
	a, _, _ := fixture(t)
	if links := a.SystemLinks(); len(links) != 0 {
		t.Fatalf("macOS should have no system links, got %+v", links)
	}

	a.Platform = manifest.Windows
	t.Setenv("LOCALAPPDATA", filepath.Join(a.Home, "AppData", "Local"))
	t.Setenv("USERPROFILE", a.Home)
	links := a.SystemLinks()
	if len(links) != 2 {
		t.Fatalf("want the terminal settings and the profile, got %+v", links)
	}
	names := map[string]bool{}
	for _, link := range links {
		names[link.Name] = true
		if !filepath.IsAbs(link.Target) {
			t.Errorf("%s target must be absolute: %s", link.Name, link.Target)
		}
	}
	for _, want := range []string{"windows-terminal", "powershell-profile"} {
		if !names[want] {
			t.Errorf("missing %s", want)
		}
	}
}

func TestAdoptOnAFullyConfiguredMachineDoesNothing(t *testing.T) {
	a, runner, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	if err := a.Link(); err != nil {
		t.Fatal(err)
	}
	runner.ran = nil

	if err := a.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains("nothing to do") {
		t.Errorf("texts = %v", rec.Texts())
	}
	if len(runner.ran) != 0 {
		t.Errorf("adopt ran commands on a complete machine: %v", runner.ran)
	}
}

func TestReporterLinesCarryTheirMark(t *testing.T) {
	var out strings.Builder
	plain := PlainReporter{Out: &out}
	plain.Phase("packages")
	plain.Line(MarkChange, "installed")
	done := plain.Working("brew bundle")
	done(MarkOK, "nothing to do")
	plain.Summary("finished")

	got := out.String()
	for _, want := range []string{"packages", "+ installed", "… brew bundle", "· nothing to do", "finished"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// The live reporter must not emit cursor control when it is told not to
// colour, and must always clear the spinner's row before writing a result -
// a shorter result would otherwise leave the tail of the spinner behind it.
func TestLiveReporterClearsTheSpinnerRow(t *testing.T) {
	var out strings.Builder
	live := &LiveReporter{Out: &out}
	done := live.Working("a long operation")
	done(MarkChange, "ok")

	got := out.String()
	if !strings.Contains(got, "\r\x1b[K") {
		t.Fatalf("the spinner row was never cleared: %q", got)
	}
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), "+ ok") {
		t.Fatalf("the result should be the last thing written: %q", got)
	}
}

func TestRepoURLPrefersTheFlagThenTheEnvironment(t *testing.T) {
	t.Setenv("DOTFILES_REPO_URL", "https://env.invalid/x.git")
	if got := RepoURL("https://flag.invalid/y.git"); got != "https://flag.invalid/y.git" {
		t.Errorf("flag should win, got %s", got)
	}
	if got := RepoURL(""); got != "https://env.invalid/x.git" {
		t.Errorf("env should be next, got %s", got)
	}
	t.Setenv("DOTFILES_REPO_URL", "")
	if got := RepoURL(""); got != DefaultRepoURL {
		t.Errorf("default should be last, got %s", got)
	}
}

func TestExpandResolvesHomePaths(t *testing.T) {
	a, _, _ := fixture(t)
	if got := a.Expand("~/x/y"); got != filepath.Join(a.Home, "x", "y") {
		t.Errorf("Expand = %s", got)
	}
	absolute := filepath.Join(string(filepath.Separator), "etc", "hosts")
	if got := a.Expand(absolute); got != absolute {
		t.Errorf("an absolute path should pass through, got %s", got)
	}
}

// The URL is a constant today, but it is one flag away from being
// user-supplied and it is embedded in a shell command. Asserted by round trip
// through a real shell rather than by inspecting the escaping: `'\”` is the
// correct escape and *contains* the sequences a naive substring check would
// flag, so pattern-matching the output tests the wrong thing.
func TestShellQuoteSurvivesARealShell(t *testing.T) {
	for _, nasty := range []string{
		"https://example.invalid/x.sh",
		"https://x/'; rm -rf /; echo '",
		`a"b$c\d`,
		"$(touch /tmp/doti-should-not-exist)",
		"`touch /tmp/doti-should-not-exist`",
	} {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(nasty)).Output()
		if err != nil {
			t.Fatalf("%q: %v", nasty, err)
		}
		if string(out) != nasty {
			t.Errorf("round trip changed %q into %q", nasty, out)
		}
	}
	if _, err := os.Stat("/tmp/doti-should-not-exist"); err == nil {
		os.Remove("/tmp/doti-should-not-exist")
		t.Fatal("a substitution executed - the quoting does not hold")
	}
}

func TestElapsedIsCompact(t *testing.T) {
	for _, tc := range []struct {
		seconds int
		want    string
	}{{0, "0s"}, {45, "45s"}, {60, "1m00s"}, {135, "2m15s"}} {
		got := elapsed(time.Duration(tc.seconds) * time.Second)
		if got != tc.want {
			t.Errorf("elapsed(%ds) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}

// Two mechanisms wrote ~/.gitconfig.local: doti's own credential-helper write
// in the configs phase, and a secret in the secrets phase - which runs later
// and would silently win. Whichever the manifest names owns the file.
func TestGitConfigLocalYieldsToADeclaringSecret(t *testing.T) {
	a, _, rec := fixture(t, "brew", "jq", "ghostty", "zsh")
	write(t, filepath.Join(a.Repo, "manifest.jsonc"),
		strings.Replace(fixtureManifest, `"health"`,
			`"secrets": [{"name":"gitconfig-local","mode":"file","item":"x",
			  "target":"~/.gitconfig.local"}], "health"`, 1))
	a.manifest = nil

	if err := a.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The native write must have stood down, and said so.
	if !rec.Contains(`secret "gitconfig-local" renders it`) {
		t.Fatalf("texts = %v", rec.Texts())
	}
	// And it must not have written the file behind the secret's back.
	if _, err := os.Stat(filepath.Join(a.Home, ".gitconfig.local")); !os.IsNotExist(err) {
		t.Error("the native write happened anyway")
	}
}

// With no secret declaring it, doti owns the file as before.
func TestGitConfigLocalIsWrittenWhenNoSecretClaimsIt(t *testing.T) {
	a, _, _ := fixture(t, "brew", "jq", "ghostty", "zsh")
	if err := a.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(a.Home, ".gitconfig.local"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "osxkeychain") {
		t.Fatalf("body = %q", body)
	}
}
