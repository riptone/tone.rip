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
	"os/exec"
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
}

// ExecRunner runs the real thing.
type ExecRunner struct {
	// Out receives the command's stdout and stderr. Package managers are
	// chatty and the chatter is the progress bar, so it is passed through
	// rather than captured.
	Out interface{ Write([]byte) (int, error) }
}

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = r.Out
	cmd.Stderr = r.Out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func (r ExecRunner) Look(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
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

// Inspect reports which of the manifest's tools are already on PATH.
//
// Detection is by command name rather than by asking the package manager,
// because the question "can I run this" is the one that matters and a tool
// installed outside brew answers it just as well. That is what makes
// `--adopt` work on a machine someone has been using for years.
func Inspect(m *manifest.Manifest, look func(string) bool) Status {
	var status Status
	for _, tool := range m.Tools {
		if look(tool.Cmd) {
			status.Present = append(status.Present, tool)
		} else {
			status.Missing = append(status.Missing, tool)
		}
	}
	return status
}
