// Package pkgs turns the manifest's tool lists into the files the platform
// package managers understand, and installs from them.
//
// Neither file is committed. They are generated at install time from
// manifest.jsonc, which is what keeps "add a tool" a one-line edit in one
// place rather than an edit plus two lockfiles that drift. The shell
// installer already worked this way; this is the same contract, and
// TestBrewfileMatchesTheShellInstaller holds the two outputs to each other
// so the migration cannot quietly change what gets installed.
package pkgs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// Runner executes a package manager. An interface so tests never install
// anything.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
	// Look reports whether a command exists on PATH.
	Look(name string) bool
	// HasApp reports whether an application bundle is installed. Always
	// false off macOS, where the idea does not apply.
	HasApp(bundle string) bool
}

// ExecRunner runs the real thing.
type ExecRunner struct {
	// Out receives the command's stdout and stderr as it runs. Leave it nil
	// to capture instead, which is the default and the better one:
	//
	//   - the display stays readable. brew, git and npm are chatty, and
	//     twenty lines of pour progress under every step buries the six
	//     that say what doti did.
	//   - a failure becomes diagnosable. Streamed output has scrolled past
	//     by the time an error surfaces; captured output goes into it.
	//
	// `--verbose` sets this to os.Stdout for the times that is what you want.
	Out io.Writer
}

// tailBytes is how much captured output an error carries.
//
// The tail, not the head, because the failure is at the end - and capped,
// because `brew bundle` on a fresh machine emits megabytes and an error
// message is not a log file.
const tailBytes = 4 << 10

// tailBuffer keeps only the last tailBytes written to it.
type tailBuffer struct{ buf []byte }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailBytes {
		t.buf = t.buf[len(t.buf)-tailBytes:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return strings.TrimSpace(string(t.buf)) }

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)

	var tail *tailBuffer
	if r.Out != nil {
		cmd.Stdout, cmd.Stderr = r.Out, r.Out
	} else {
		tail = &tailBuffer{}
		cmd.Stdout, cmd.Stderr = tail, tail
	}

	if err := cmd.Run(); err != nil {
		invocation := name + " " + strings.Join(args, " ")
		if tail == nil || tail.String() == "" {
			return fmt.Errorf("%s: %w", invocation, err)
		}
		return fmt.Errorf("%s: %w\n%s", invocation, err, indent(tail.String()))
	}
	return nil
}

// indent shifts captured output right, so it reads as evidence under the
// error rather than as more of the message.
func indent(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

func (r ExecRunner) Look(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// appDirs are where a macOS application can be installed: system-wide, or
// for one user. Homebrew casks use the first; a hand-installed app is often
// in the second.
var appDirs = []string{"/Applications", filepath.Join(os.Getenv("HOME"), "Applications")}

func (r ExecRunner) HasApp(bundle string) bool {
	if runtime.GOOS != "darwin" || bundle == "" {
		return false
	}
	for _, dir := range appDirs {
		if _, err := os.Stat(filepath.Join(dir, bundle+".app")); err == nil {
			return true
		}
	}
	return false
}

// Brewfile renders the Homebrew bundle.
//
// Casks are guarded with `if OS.mac?` rather than omitted on Linux, so one
// generated file is valid on both and `brew bundle` decides - which is what
// the shell installer did, and why a Linux run does not fail on a macOS-only
// GUI app.
func Brewfile(m *manifest.Manifest) string {
	var b strings.Builder
	b.WriteString("# GENERATED from manifest.jsonc — do not edit manually.\n")
	b.WriteString("# install:  brew bundle --file=<temp>\n")
	b.WriteString("\n")

	b.WriteString("# --- CLI (all platforms) ---\n")
	for _, tool := range m.Tools {
		if tool.Brew != "" {
			fmt.Fprintf(&b, "brew %q\n", tool.Brew)
		}
	}

	b.WriteString("\n# --- zsh interactive plugins (sourced from ~/.zshrc; no framework) ---\n")
	for _, plugin := range m.ZshPlugins {
		if plugin.Brew != "" {
			fmt.Fprintf(&b, "brew %q\n", plugin.Brew)
		}
	}

	b.WriteString("\n# --- GUI apps + font (macOS only) ---\n")
	for _, cask := range m.BrewCasks {
		if cask.Brew == "" {
			continue
		}
		// Guarded rather than omitted, so one generated file is valid on
		// both platforms and `brew bundle` decides - which is why a Linux
		// run does not fail on a macOS-only GUI app.
		if slices.Contains(cask.Platforms, manifest.MacOS) {
			fmt.Fprintf(&b, "cask %q if OS.mac?\n", cask.Brew)
		} else {
			fmt.Fprintf(&b, "cask %q\n", cask.Brew)
		}
	}
	return b.String()
}

// BrewfileForTools renders a Brewfile holding only the given formulae.
//
// For `--tools fd,gh`: installing one missing tool should not also pull in
// every cask and zsh plugin the full Brewfile carries, which is what made
// "install just this one thing" impossible from the CLI before.
func BrewfileForTools(tools []manifest.Tool) string {
	var b strings.Builder
	b.WriteString("# GENERATED by doti --tools — do not edit manually.\n\n")
	for _, tool := range tools {
		if tool.Brew != "" {
			fmt.Fprintf(&b, "brew %q\n", tool.Brew)
		}
	}
	return b.String()
}

// wingetIdentifiers is the tool list plus the GUI extras, in that order and
// without duplicates - the shell installer's `.[0] + (.[1] - .[0])`.
func wingetIdentifiers(m *manifest.Manifest) []string {
	ids := make([]string, 0, len(m.Tools)+len(m.WingetExtras))
	for _, tool := range m.Tools {
		if tool.Winget != "" {
			ids = append(ids, tool.Winget)
		}
	}
	for _, extra := range m.WingetExtras {
		if !slices.Contains(ids, extra) {
			ids = append(ids, extra)
		}
	}
	return ids
}

type wingetPackage struct {
	PackageIdentifier string `json:"PackageIdentifier"`
}

type wingetSource struct {
	SourceDetails struct {
		Argument   string `json:"Argument"`
		Identifier string `json:"Identifier"`
		Name       string `json:"Name"`
		Type       string `json:"Type"`
	} `json:"SourceDetails"`
	Packages []wingetPackage `json:"Packages"`
}

type wingetFile struct {
	Schema  string         `json:"$schema"`
	Sources []wingetSource `json:"Sources"`
}

// WingetPackages renders the `winget import` file.
func WingetPackages(m *manifest.Manifest) (string, error) {
	source := wingetSource{}
	source.SourceDetails.Argument = "https://cdn.winget.microsoft.com/cache"
	source.SourceDetails.Identifier = "Microsoft.Winget.Source_8wekyb3d8bbwe"
	source.SourceDetails.Name = "winget"
	source.SourceDetails.Type = "Microsoft.PreIndexed.Package"
	for _, id := range wingetIdentifiers(m) {
		source.Packages = append(source.Packages, wingetPackage{PackageIdentifier: id})
	}

	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetIndent("", "    ")
	// The identifiers are package names, never URLs; escaping would only
	// mangle them into < style noise if one ever contained an angle
	// bracket.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(wingetFile{
		Schema:  "https://aka.ms/winget-packages.schema.2.0.json",
		Sources: []wingetSource{source},
	}); err != nil {
		return "", fmt.Errorf("rendering winget packages: %w", err)
	}
	return out.String(), nil
}

// Status is what a machine already has.
type Status struct {
	Present []manifest.Tool
	Missing []manifest.Tool
}

// Inspect reports which of the manifest's tools this machine already has.
//
// Detection is by command name rather than by asking the package manager,
// because "can I run this" is the question that matters and a tool installed
// outside brew answers it just as well - which is what makes `adopt` work on
// a machine someone has used for years.
//
// A tool that declares an `app` falls back to looking for the bundle. Without
// that, every macOS GUI app is a permanent false negative: they put nothing
// on PATH, so `command -v ghostty` fails on a machine where Ghostty is
// running. That false positive would make `check --strict` unusable.
func Inspect(m *manifest.Manifest, runner Runner) Status {
	var status Status
	for _, tool := range m.Tools {
		if runner.Look(tool.Cmd) || runner.HasApp(tool.App) {
			status.Present = append(status.Present, tool)
		} else {
			status.Missing = append(status.Missing, tool)
		}
	}
	return status
}
