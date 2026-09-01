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
	if !a.wants(KindExtra, name) {
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
	for _, name := range SplitList(a.Tools) {
		tool, ok := byCmd[name]
		if !ok {
			available := make([]string, 0, len(byCmd))
			for cmd := range byCmd {
				available = append(available, cmd)
			}
			slices.Sort(available)
			if len(available) == 0 {
				// "(missing: )" is a worse answer than the sentence it was
				// trying to be, and it is the common case now that the list is
				// narrowed to what this platform can actually install.
				return nil, fmt.Errorf("%q is not a missing tool: "+
					"nothing this platform can install is missing", name)
			}
			return nil, fmt.Errorf("%q is not a missing tool (missing: %s)",
				name, strings.Join(available, ", "))
		}
		out = append(out, tool)
	}
	return out, nil
}

// bundle is the three lists a `brew bundle` is rendered from, each narrowed by
// what this run was told to include.
//
// A struct because they are narrowed independently and must stay that way:
// rendering a tools-only file because one tool was unticked is what would
// silently decline every GUI app and both zsh plugins along with it.
type bundle struct {
	tools   []manifest.Tool
	plugins []manifest.ZshPlugin
	casks   []manifest.Cask
	// extras is the Windows counterpart of casks.
	extras []string
}

func (b bundle) empty() bool {
	return len(b.tools)+len(b.plugins)+len(b.casks)+len(b.extras) == 0
}

// describeBundle is what a dry run says it would install.
//
// Counted per list rather than named one by one, because "would install jq, fd,
// ghostty, visual-studio-code, brave-browser, font-jetbrains-mono-nerd-font,
// hiddenbar, zsh-autosuggestions, zsh-fast-syntax-highlighting" is a line nobody
// reads to the end.
func describeBundle(b bundle) string {
	var parts []string
	if n := len(b.tools); n > 0 {
		parts = append(parts, fmt.Sprintf("%d tool(s)", n))
	}
	if n := len(b.casks); n > 0 {
		parts = append(parts, fmt.Sprintf("%d cask(s)", n))
	}
	if n := len(b.plugins); n > 0 {
		parts = append(parts, fmt.Sprintf("%d zsh plugin(s)", n))
	}
	if n := len(b.extras); n > 0 {
		parts = append(parts, fmt.Sprintf("%d GUI app(s)", n))
	}
	return strings.Join(parts, ", ")
}

// selected narrows every list the packages phase installs.
//
// One place, because the phase has four of them now and a narrowing applied in
// three would be a checkbox that works most of the time.
func (a *App) selected(m *manifest.Manifest, missing []manifest.Tool) (bundle, error) {
	tools := a.toolsFor(m)
	if a.Tools != "" {
		// --tools names *missing* tools and errors on anything else, which is
		// its own contract and older than the selector's.
		//
		// Narrowed to what this platform can install before the check, or the
		// error it promises does not happen: `code` is declared with a winget id
		// and no brew formula, so on a Mac it is "missing" by PATH, passes the
		// check, and is then dropped by the renderer because it has no formula
		// to write. `--tools code` was the silent no-op the check exists to
		// rule out.
		installable := map[string]bool{}
		for _, tool := range tools {
			installable[tool.Cmd] = true
		}
		open := make([]manifest.Tool, 0, len(missing))
		for _, tool := range missing {
			if installable[tool.Cmd] {
				open = append(open, tool)
			}
		}
		named, err := a.selectedTools(open)
		if err != nil {
			return bundle{}, err
		}
		tools = named
	}

	var b bundle
	for _, tool := range tools {
		if a.wantsUnder(Ref{KindTools, packagesLabel}, KindTool, tool.Cmd) {
			b.tools = append(b.tools, tool)
		}
	}
	if a.Tools != "" && !a.hasSelection() {
		// --tools names tools and nothing else. Installing one missing tool
		// should not also pull in every cask and zsh plugin, which is what made
		// "install just this one thing" impossible from the CLI before it
		// existed. A selection supersedes it, being the more specific answer.
		return b, nil
	}
	if a.Platform == manifest.Windows {
		for _, id := range m.WingetExtras {
			if a.wantsUnder(Ref{KindWingetExtras, wingetExtrasLabel}, KindWingetExtra, id) {
				b.extras = append(b.extras, id)
			}
		}
		return b, nil
	}
	for _, plugin := range m.ZshPlugins {
		if a.wantsUnder(Ref{KindPlugins, pluginsLabel}, KindPlugin, plugin.Brew) {
			b.plugins = append(b.plugins, plugin)
		}
	}
	// Every declared cask, not just this platform's: the rendered file guards
	// the macOS ones with `if OS.mac?` and lets brew decide, which is what makes
	// one generated file valid on both.
	for _, cask := range m.BrewCasks {
		if a.wantsUnder(Ref{KindCasks, casksLabel}, KindCask, cask.Brew) {
			b.casks = append(b.casks, cask)
		}
	}
	return b, nil
}

// InstallPackages brings the machine's packages up to the manifest.
func (a *App) InstallPackages(ctx context.Context) error {
	m, err := a.Manifest()
	if err != nil {
		return err
	}

	status := pkgs.Inspect(m, a.Runner)
	// The tools this platform can install, and which of them it has - the same
	// two numbers the selector shows, from the same function, because a run and
	// the screen that launched it disagreeing about a count is worse than either
	// number being the wrong one.
	installable := a.toolsFor(m)
	present := a.toolsPresent(m)

	want, err := a.selected(m, status.Missing)
	if err != nil {
		return err
	}
	if want.empty() {
		// Nothing ticked under any of this phase's lists, which is how the
		// selector spells "skip the packages".
		a.Report.Line(MarkSkip, packagesLabel+" (not selected)")
		return nil
	}

	// Which of the *selected* tools the machine does not have.
	//
	// Only the tools: those are the ones with a present / missing answer, and
	// menu_packages.go says why the casks and plugins do not have one.
	missing := make([]string, 0, len(want.tools))
	for _, tool := range want.tools {
		if !present[tool.Cmd] {
			missing = append(missing, tool.Cmd)
		}
	}

	// Where the machine stands, about every tool the manifest names, before
	// anything about what this run would do. It used to say "all N tools
	// present" whenever nothing *selected* was missing - so ticking one present
	// tool on a machine missing another reported every tool as present, which
	// was simply false.
	// MarkOK, like the other lines that state what the machine already has: a
	// blank mark is for a continuation of the line above it, and this is the
	// first line of the phase.
	a.Report.Line(MarkOK, fmt.Sprintf("%d of %d tools present",
		len(present), len(installable)))
	switch {
	case len(missing) > 0 && a.DryRun:
		a.Report.Line(MarkChange, "would install "+strings.Join(missing, ", "))
	case len(missing) > 0:
		a.Report.Line(MarkNone, "installing "+strings.Join(missing, ", "))
	case !a.hasSelection() && !a.DryRun:
		// Everything, and nothing missing: brew bundle over the whole manifest
		// would be a no-op, and skipping the subprocess is the same outcome
		// faster.
		return nil
	}

	if a.DryRun {
		// What the bundle would be rendered from, rather than what it would
		// change: the casks and the plugins were never asked about, so claiming
		// either way about them would be a guess.
		a.Report.Line(MarkNone, "the bundle covers "+describeBundle(want))
		return nil
	}

	if a.Platform == manifest.Windows {
		return a.runWinget(ctx, want, present)
	}
	if !a.Runner.Look("brew") {
		return fmt.Errorf("homebrew is not installed - see https://brew.sh")
	}
	body := pkgs.BrewfileOf(want.tools, want.plugins, want.casks)
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

// runWinget is the Windows half of the packages phase: the winget import, then
// whatever the manifest hands to bun instead.
//
// In that order, and the order is load-bearing. bun is itself a winget package,
// so on a fresh machine it arrives *in* the import - and `bun install -g` before
// that has finished is a command that does not exist yet. The manifest's rule
// that bun be declared before the first tool naming it is the same constraint
// written where a reader can see it.
func (a *App) runWinget(ctx context.Context, want bundle, present map[string]bool) error {
	// The tools bun installs, and only the ones this machine is missing.
	//
	// `bun install -g` on a package that is already there re-resolves it to the
	// latest published version, which is an upgrade - the thing
	// `brew bundle --no-upgrade` exists to not do. Install and update are
	// different operations and the menu offers both.
	var viaBun []string
	for _, tool := range want.tools {
		if src := a.sourceFor(tool); src.manager == managerBun && !present[tool.Cmd] {
			viaBun = append(viaBun, src.name)
		}
	}

	if err := a.importWinget(ctx, want); err != nil {
		return err
	}
	return a.installViaBun(ctx, viaBun)
}

// importWinget runs the generated `winget import`, or says why it did not.
func (a *App) importWinget(ctx context.Context, want bundle) error {
	if len(pkgs.WingetIdentifiers(want.tools, want.extras)) == 0 {
		// Nothing winget can install. Reachable now that a tool can name bun
		// and no winget id, and handing winget an empty package list is a
		// subprocess that can only fail or do nothing.
		return nil
	}
	body, err := pkgs.WingetPackagesOf(want.tools, want.extras)
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

// installViaBun installs the tools the manifest routes through bun.
//
// One invocation per package rather than one for all of them, so a failure names
// which package failed - and fatal rather than best-effort, unlike the MCP
// servers: these are declared tools, and a tool the manifest asked for and did
// not get is the thing `doti check` is supposed to go red about.
func (a *App) installViaBun(ctx context.Context, packages []string) error {
	if len(packages) == 0 {
		return nil
	}
	if !a.Runner.Look("bun") {
		// The manifest's ordering rule is meant to make this unreachable. It is
		// reported rather than assumed away, because the rule holds for a whole
		// run and this can also be a selection that ticked opencode and not bun.
		return fmt.Errorf("bun is not installed, so %s cannot be installed - "+
			"tick bun as well, or install it first", strings.Join(packages, ", "))
	}
	done := a.Report.Working(fmt.Sprintf("bun install -g (%d tool(s))", len(packages)))
	for _, pkg := range packages {
		if err := a.Runner.Run(ctx, "bun", "install", "-g", pkg); err != nil {
			done(MarkWarn, "bun install -g "+pkg+" failed")
			return err
		}
	}
	done(MarkChange, "installed "+strings.Join(packages, ", "))
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
	// One at a time, and this is where an unticked box stops being decorative -
	// they used to install regardless of the selector, and then regardless of
	// which of them was ticked.
	wanted := make([]string, 0, len(packages))
	for _, pkg := range packages {
		if a.wantsUnder(Ref{KindMcps, mcpLabel}, KindMcp, pkg) {
			wanted = append(wanted, pkg)
		}
	}
	if len(wanted) == 0 {
		a.Report.Line(MarkSkip, mcpLabel+" (not selected)")
		return
	}
	packages = wanted
	if !a.Runner.Look("npm") {
		a.Report.Line(MarkSkip, "npm is not installed, so no MCP servers")
		return
	}

	// Only the ones npm does not already have. Asked even under --dry-run,
	// because `npm root -g` is a question and the whole point of a preview is to
	// say what would actually change.
	//
	// `npm install -g` on a package that is already there takes about two
	// seconds to decide it has nothing to do, so an install on a set-up machine
	// spent fifteen of them reinstalling seven packages it had. The upgrade it
	// used to perform as a side effect now lives in `doti update`, which is the
	// same split `brew bundle --no-upgrade` already draws: install and update
	// are different operations, and the menu offers both.
	// Asked directly rather than through App.Globals, and that is deliberate:
	// this runs *after* the packages phase, which may just have installed node
	// and npm - so an answer cached before them would be an answer about a
	// machine that had no npm.
	present, err := pkgs.NpmGlobals(ctx, a.Runner, packages)
	if err != nil {
		a.Report.Line(MarkWarn, fmt.Sprintf("could not ask npm what it has: %v", err))
		present = nil
	}
	fresh := make([]string, 0, len(packages))
	for _, pkg := range packages {
		if !present[pkg] {
			fresh = append(fresh, pkg)
		}
	}
	// Counted over what was selected, and said so: "7 already present" when two
	// of seven were ticked was a number about a set nobody had asked about.
	if len(fresh) == 0 {
		a.Report.Line(MarkOK, fmt.Sprintf("%s: %d selected, all present",
			mcpLabel, len(packages)))
		return
	}
	if len(fresh) < len(packages) {
		a.Report.Line(MarkNone, fmt.Sprintf("%s: %d of %d selected already present",
			mcpLabel, len(packages)-len(fresh), len(packages)))
	}
	packages = fresh

	if a.DryRun {
		a.Report.Line(MarkChange, fmt.Sprintf("would npm install -g %s",
			strings.Join(packages, ", ")))
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

// SplitList reads a comma-separated flag value.
//
// Exported because --tools feeds two things now: which missing tools to
// install, and which installed ones to remove. Two spellings of "split on
// commas and trim" is two chances to disagree about " jq, fd".
func SplitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
