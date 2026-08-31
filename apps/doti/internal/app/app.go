package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
	"github.com/riptone/tone.rip/apps/doti/internal/pkgs"
	"github.com/riptone/tone.rip/apps/doti/internal/secrets"
	"github.com/riptone/tone.rip/apps/doti/internal/stow"
)

// BackupsDir is where displaced files go, relative to $HOME.
const BackupsDir = ".dotfiles-backups"

// App is one invocation: where the configs are, which machine this is, and
// who is watching.
//
// Everything a command needs is a field, and the two things that touch the
// outside world - the Reporter and the Runner - are interfaces. That is what
// lets the whole surface be tested without a package manager, a vault, a
// terminal or a $HOME.
type App struct {
	Repo     string
	RepoURL  string
	Home     string
	Platform manifest.Platform
	DryRun   bool
	// Only narrows link/unlink to one stow package.
	Only string
	// Tools narrows package installation to a comma-separated subset.
	Tools string
	// Include narrows a run to the components the selector ticked, by the
	// labels MenuItems produced. Empty means everything, which is what every
	// command-line invocation wants.
	//
	// It exists because the window's checkboxes did nothing: the menu returned
	// what had been ticked and the handler that ran the operation never asked.
	Include []string
	// Interactive is true when a person is watching and can answer a prompt.
	//
	// Set from the same check that picks the Reporter. It gates the vault
	// login: prompting for a master password is right when somebody typed
	// `doti secrets`, and a hang when a script did.
	Interactive bool
	// FontBaseURL overrides where the Nerd Font release is fetched from.
	// For a mirror, and for the test that proves the checksum is enforced.
	FontBaseURL string
	Report      Reporter
	Runner      pkgs.Runner
	// Vault runs the `bw` CLI. Nil is the real binary with this process's
	// terminal, which is right for a command and wrong inside a window: the
	// alt screen has taken the terminal `bw` needs for its own password
	// prompt, so internal/tui supplies one that borrows it back.
	//
	// It is also what makes the secrets phase reachable from a test at all.
	Vault secrets.Runner

	// manifest is loaded once per invocation, on first use.
	manifest *manifest.Manifest
	ignorer  *stow.Ignorer
}

// New builds an App for this machine.
func New(repo string, report Reporter, runner pkgs.Runner) (*App, error) {
	// Absolute, and resolved here rather than trusted from the caller: the
	// linker stores whatever path it is given inside the symlinks it
	// creates, and a relative one resolves against $HOME and dangles.
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return nil, fmt.Errorf("resolving repo path %q: %w", repo, err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("finding home directory: %w", err)
	}
	platform, err := CurrentPlatform()
	if err != nil {
		return nil, err
	}
	return &App{
		Repo: absRepo, Home: home, Platform: platform,
		Report: report, Runner: runner,
	}, nil
}

// CurrentPlatform maps GOOS onto the manifest's vocabulary.
func CurrentPlatform() (manifest.Platform, error) {
	switch runtime.GOOS {
	case "darwin":
		return manifest.MacOS, nil
	case "linux":
		return manifest.Linux, nil
	case "windows":
		return manifest.Windows, nil
	default:
		return "", fmt.Errorf("unsupported platform %q", runtime.GOOS)
	}
}

// Manifest loads manifest.jsonc, once.
func (a *App) Manifest() (*manifest.Manifest, error) {
	if a.manifest != nil {
		return a.manifest, nil
	}
	m, err := manifest.Load(filepath.Join(a.Repo, "manifest.jsonc"))
	if err != nil {
		return nil, err
	}
	a.manifest = m
	return m, nil
}

// Ignorer compiles the manifest's stow_ignore patterns, once.
func (a *App) Ignorer() (*stow.Ignorer, error) {
	if a.ignorer != nil {
		return a.ignorer, nil
	}
	m, err := a.Manifest()
	if err != nil {
		return nil, err
	}
	ignorer, err := stow.NewIgnorer(m.StowIgnore)
	if err != nil {
		return nil, err
	}
	a.ignorer = ignorer
	return ignorer, nil
}

// Cloned reports whether there is a checkout to work with.
func (a *App) Cloned() bool {
	_, err := os.Stat(filepath.Join(a.Repo, "manifest.jsonc"))
	return err == nil
}

// Expand resolves a ~/ path against this machine's home directory.
func (a *App) Expand(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	return filepath.Join(a.Home, filepath.FromSlash(strings.TrimPrefix(path, "~/")))
}

// wants reports whether a named component is part of this run.
//
// An empty Include is "everything", which is what every command-line
// invocation wants and what the window means before anything is unticked.
func (a *App) wants(label string) bool {
	return len(a.Include) == 0 || slices.Contains(a.Include, label)
}

// Packages is the manifest's stow packages for this platform, in manifest
// order - which is load-bearing: the `stow` package carries
// ~/.stow-global-ignore and is listed first so it is in place before anything
// else is linked.
func (a *App) Packages() ([]manifest.StowPackage, error) {
	m, err := a.Manifest()
	if err != nil {
		return nil, err
	}
	var out []manifest.StowPackage
	for _, pkg := range m.StowPackages {
		if len(pkg.Platforms) > 0 && !slices.Contains(pkg.Platforms, a.Platform) {
			continue
		}
		// The selector's Configs group is these names, so this is where a
		// unticked box stops being decorative.
		if !a.wants(pkg.Name) {
			continue
		}
		out = append(out, pkg)
	}
	if a.Only == "" {
		return out, nil
	}
	for _, pkg := range out {
		if pkg.Name == a.Only {
			return []manifest.StowPackage{pkg}, nil
		}
	}
	names := make([]string, 0, len(out))
	for _, pkg := range out {
		names = append(names, pkg.Name)
	}
	return nil, fmt.Errorf("no stow package %q for %s (have: %s)",
		a.Only, a.Platform, strings.Join(names, ", "))
}

// Validate parses the manifest and reports what it holds.
func (a *App) Validate() error {
	m, err := a.Manifest()
	if err != nil {
		return err
	}
	a.Report.Summary(fmt.Sprintf("%s %s - manifest ok", m.App, m.Version))
	a.Report.Line(MarkOK, fmt.Sprintf(
		"%d stow packages, %d tools, %d secrets",
		len(m.StowPackages), len(m.Tools), len(m.Secrets)))
	return nil
}

// PackageLists renders the generated package files.
func (a *App) PackageLists(brew, winget bool) (string, error) {
	m, err := a.Manifest()
	if err != nil {
		return "", err
	}
	both := !brew && !winget
	var out strings.Builder
	if brew || both {
		out.WriteString(pkgs.Brewfile(m))
	}
	if both {
		out.WriteString("\n")
	}
	if winget || both {
		body, err := pkgs.WingetPackages(m)
		if err != nil {
			return "", err
		}
		out.WriteString(body)
	}
	return out.String(), nil
}

// tempFile writes a generated package list somewhere disposable.
//
// Deliberately not into the repo: these are generated at install time
// precisely so neither is committed and neither can drift from the manifest.
func tempFile(name, body string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "doti-")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", nil, fmt.Errorf("writing %s: %w", path, err)
	}
	return path, func() { os.RemoveAll(dir) }, nil
}
