// The packages phase: the tools a machine needs, and the things no package
// manager covers.
package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
	"github.com/riptone/tone.rip/apps/doti/internal/pkgs"
)

// WantsExtra reports whether the manifest declares a named extra for this
// platform. Extras are the things no package manager covers.
func (a *App) WantsExtra(name string) bool {
	m, err := a.Manifest()
	if err != nil {
		return false
	}
	if !a.wants(name) {
		return false
	}
	for _, extra := range m.Extras {
		if extra.Name != name {
			continue
		}
		return len(extra.Platforms) == 0 || slices.Contains(extra.Platforms, a.Platform)
	}
	return false
}

// selectedTools narrows the missing set to what --tools named.
//
// An unknown name is an error rather than a silent no-op: someone typing
// `--tools fd,gh` and getting nothing would reasonably conclude the tools
// were already installed.
func (a *App) selectedTools(missing []manifest.Tool) ([]manifest.Tool, error) {
	if a.Tools == "" {
		return missing, nil
	}
	byCmd := map[string]manifest.Tool{}
	for _, tool := range missing {
		byCmd[tool.Cmd] = tool
	}
	var out []manifest.Tool
	for _, name := range strings.Split(a.Tools, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		tool, ok := byCmd[name]
		if !ok {
			available := make([]string, 0, len(byCmd))
			for cmd := range byCmd {
				available = append(available, cmd)
			}
			slices.Sort(available)
			return nil, fmt.Errorf("%q is not a missing tool (missing: %s)",
				name, strings.Join(available, ", "))
		}
		out = append(out, tool)
	}
	return out, nil
}

// InstallPackages brings the machine's tools up to the manifest.
func (a *App) InstallPackages(ctx context.Context) error {
	m, err := a.Manifest()
	if err != nil {
		return err
	}
	// The selector lists the tool set as one component, under this label.
	if !a.wants(packagesLabel) {
		a.Report.Line(MarkSkip, packagesLabel+" (not selected)")
		return nil
	}

	status := pkgs.Inspect(m, a.Runner)
	total := len(status.Present) + len(status.Missing)

	if len(status.Missing) == 0 {
		a.Report.Line(MarkOK, fmt.Sprintf("all %d tools present", total))
		return nil
	}
	wanted, err := a.selectedTools(status.Missing)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(wanted))
	for _, tool := range wanted {
		names = append(names, tool.Cmd)
	}
	a.Report.Line(MarkNone, fmt.Sprintf("%d of %d present; installing %s",
		len(status.Present), total, strings.Join(names, ", ")))

	if a.DryRun {
		a.Report.Line(MarkChange, "would install "+strings.Join(names, ", "))
		return nil
	}

	if a.Platform == manifest.Windows {
		return a.runWinget(ctx, m)
	}
	if !a.Runner.Look("brew") {
		return fmt.Errorf("homebrew is not installed - see https://brew.sh")
	}
	// A subset gets its own Brewfile: installing one missing tool must not
	// also pull in every cask the full file carries.
	body := pkgs.Brewfile(m)
	if a.Tools != "" {
		body = pkgs.BrewfileForTools(wanted)
	}
	file, cleanup, err := tempFile("Brewfile", body)
	if err != nil {
		return err
	}
	defer cleanup()

	done := a.Report.Working("brew bundle install")
	// --no-upgrade because install and update are different operations, and
	// the menu offers both. Without it `brew bundle install` upgrades every
	// outdated formula as a side effect, so asking for one missing tool
	// quietly moves your node version - a thing to opt into, not to discover
	// afterwards. `doti update` is where that lives.
	if err := a.Runner.Run(ctx, "brew", "bundle", "install", "--no-upgrade", "--file="+file); err != nil {
		done(MarkWarn, "brew bundle failed")
		return err
	}
	done(MarkChange, "installed the missing tools")
	return nil
}

func (a *App) runWinget(ctx context.Context, m *manifest.Manifest) error {
	body, err := pkgs.WingetPackages(m)
	if err != nil {
		return err
	}
	file, cleanup, err := tempFile("packages.json", body)
	if err != nil {
		return err
	}
	defer cleanup()

	done := a.Report.Working("winget import")
	if err := a.Runner.Run(ctx, "winget", "import", "-i", file,
		"--accept-package-agreements", "--accept-source-agreements"); err != nil {
		done(MarkWarn, "winget import failed")
		return err
	}
	done(MarkChange, "installed the missing tools")
	return nil
}

// installMcps installs the manifest's global npm packages.
//
// Global rather than npx-on-demand because these are MCP servers started on
// every editor launch, and npx re-resolves the registry each time.
//
// Best-effort: a missing npm or one failed package is a warning, not a failed
// install. None of them is load-bearing for a working shell.
func (a *App) installMcps(ctx context.Context, packages []string) {
	if len(packages) == 0 {
		return
	}
	if !a.Runner.Look("npm") {
		a.Report.Line(MarkSkip, "npm is not installed, so no MCP servers")
		return
	}
	if a.DryRun {
		a.Report.Line(MarkChange, fmt.Sprintf("would npm install -g %d package(s)", len(packages)))
		return
	}
	done := a.Report.Working(fmt.Sprintf("npm install -g (%d MCP servers)", len(packages)))
	var failed []string
	for _, pkg := range packages {
		if err := a.Runner.Run(ctx, "npm", "install", "-g", pkg); err != nil {
			failed = append(failed, pkg)
		}
	}
	if len(failed) > 0 {
		done(MarkWarn, fmt.Sprintf("%d of %d MCP servers did not install: %s",
			len(failed), len(packages), strings.Join(failed, ", ")))
		return
	}
	done(MarkChange, fmt.Sprintf("%d MCP servers installed", len(packages)))
}
