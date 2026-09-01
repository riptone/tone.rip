package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
	"github.com/riptone/tone.rip/apps/doti/internal/pkgs"
)

// Removing software, which is the one operation here that deletes any.
//
// The README said this was deliberately absent, and the reason still holds:
// deleting somebody's `node` because a manifest changed would be a bad
// surprise. So it exists now under five rules, and the rules are the feature:
//
//   - It removes exactly what it was *named*. Include is the list, and an empty
//     Include removes nothing - there is no spelling of this that means "all"
//     unless somebody typed one.
//   - It offers only what the package manager says it installed. Not what is on
//     PATH: macOS ships /usr/bin/jq, so a machine where `brew uninstall jq` had
//     already run went on offering jq as "installed" for as long as the list was
//     built from `command -v`. See pkgs.Owned.
//   - It refuses anything the manifest does not list. A tool this repository
//     never installed is not this repository's to remove.
//   - It refuses the tools the manifest calls required. `brew`, `git`, `stow`
//     and `zsh` are how the machine gets back to a working state, and a command
//     that can remove its own package manager is a foot-gun with a name.
//   - It does not pass --ignore-dependencies. Homebrew refusing because
//     something depends on a formula is the correct answer, reported rather
//     than overridden.

// Removable is what this tool is willing to remove and the machine still has:
// the manifest's tools that the package manager owns, and the MCP servers npm
// actually has installed.
//
// The selector's list for a removal, which is also why they arrive unticked:
// see MenuItems for the shape.
func (a *App) Removable(ctx context.Context) ([]Component, error) {
	m, err := a.Manifest()
	if err != nil {
		return nil, err
	}
	owned, err := a.Owned(ctx)
	if err != nil {
		return nil, err
	}
	required := a.requiredTools(m)
	// bun keeps its own inventory, and `winget export` knows nothing about it.
	bunOwned := pkgs.BunGlobals(a.Home, a.bunNames(m))

	out := make([]Component, 0, len(m.Tools)+len(m.Mcps))
	for _, tool := range m.Tools {
		if required[tool.Cmd] {
			continue
		}
		src := a.sourceFor(tool)
		if src.name == "" || !a.installedBy(src, owned, bunOwned) {
			// Either the manifest hands this platform's package manager
			// nothing, or the package manager did not install it. A tool that
			// arrived some other way is not this tool's to delete.
			continue
		}
		out = append(out, Component{
			Group: "Packages", Kind: KindTool, Label: tool.Cmd,
			Status: "installed", Done: true, Selected: false,
		})
	}

	installed, err := a.Globals(ctx)
	if err != nil {
		return nil, err
	}
	for _, pkg := range m.Mcps {
		if !installed[pkg] {
			continue
		}
		out = append(out, Component{
			Group: "MCP servers", Kind: KindMcp, Label: pkg,
			Status: "installed", Done: true, Selected: false,
		})
	}
	return out, nil
}

// requiredTools is the set the manifest says has to be present.
//
// health.extra_tools is exactly that list - the things checked for but never
// installed through a package manager, because they are how a machine becomes
// able to run this at all.
func (a *App) requiredTools(m *manifest.Manifest) map[string]bool {
	required := map[string]bool{
		// Belt and braces. A manifest that stopped listing these would not make
		// removing them a good idea.
		"brew": true, "git": true, "stow": true, "zsh": true, "winget": true,
	}
	if m.Health == nil {
		return required
	}
	for _, extra := range m.Health.ExtraTools[a.Platform] {
		required[extra.Cmd] = true
	}
	return required
}

// The three package managers a tool can come from.
const (
	managerBrew   = "brew"
	managerWinget = "winget"
	managerBun    = "bun"
)

// toolSource is the package manager that installs a tool on this platform and
// the name to hand it.
//
// One value rather than a bare string, because the two questions this package
// asks about a tool have different answers per manager: which inventory says it
// is installed, and which command removes it. They used to be inferred from
// a.Platform alone, which held right up until a tool on Windows came from
// neither winget nor brew.
type toolSource struct {
	manager string
	name    string
}

// sourceFor is which package manager installs a tool here.
//
// The platform's own first, bun as the fallback. opencode names both the brew
// tap and `opencode-ai`, so it gets the tap on macOS and Linux and bun on
// Windows, where winget's copy lags; a tool that names only bun installs on all
// three.
func (a *App) sourceFor(tool manifest.Tool) toolSource {
	native, manager := tool.Brew, managerBrew
	if a.Platform == manifest.Windows {
		native, manager = tool.Winget, managerWinget
	}
	if native != "" {
		return toolSource{manager: manager, name: native}
	}
	if tool.Bun != "" {
		return toolSource{manager: managerBun, name: tool.Bun}
	}
	return toolSource{}
}

// packageFor is the name to hand this platform's package manager, or "" when
// the manifest names none.
func (a *App) packageFor(tool manifest.Tool) string { return a.sourceFor(tool).name }

// installedBy asks the inventory that belongs to a tool's source.
//
// One question, three answers: `brew list` for a formula or cask, `winget
// export` for a winget id, and bun's own global directory for a bun package.
// Asking the wrong one is not a near miss - `winget export` knows nothing about
// a bun global, so opencode on Windows would have read as "never installed" for
// as long as it existed.
func (a *App) installedBy(src toolSource, owned, bunOwned map[string]bool) bool {
	if src.manager == managerBun {
		return bunOwned[src.name]
	}
	// Formula, because a manifest may name a formula tap-qualified while
	// `brew list` answers short. A winget id has no slash and comes through
	// untouched.
	return owned[pkgs.Formula(src.name)]
}

// bunNames is every tool this platform installs with bun.
//
// Collected in one pass so the inventory is read once per call rather than once
// per tool - the same reason Owned asks `brew list` for the whole machine.
func (a *App) bunNames(m *manifest.Manifest) []string {
	var out []string
	for _, tool := range m.Tools {
		if src := a.sourceFor(tool); src.manager == managerBun {
			out = append(out, src.name)
		}
	}
	return out
}

// RemovePackages uninstalls the tools and MCP servers named in Include.
//
// Named, not selected-by-default: an empty Include is reported as the no-op it
// is, with the list of what could have been named. That is the whole safety
// story - `doti uninstall` on its own tells you what it would be willing to
// remove and removes nothing.
func (a *App) RemovePackages(ctx context.Context) error {
	m, err := a.Manifest()
	if err != nil {
		return err
	}
	removable, err := a.Removable(ctx)
	if err != nil {
		return err
	}

	if len(a.Include) == 0 {
		return a.reportNothingNamed(removable)
	}

	byCmd := map[string]manifest.Tool{}
	for _, tool := range m.Tools {
		byCmd[tool.Cmd] = tool
	}
	mcps := map[string]bool{}
	for _, pkg := range m.Mcps {
		mcps[pkg] = true
	}
	// Present, as opposed to owned: the two differ, and the difference is the
	// only honest way to say why a named tool is being left alone.
	present := map[string]bool{}
	for _, tool := range pkgs.Inspect(m, a.Runner).Present {
		present[tool.Cmd] = true
	}
	allowed := map[string]bool{}
	for _, item := range removable {
		allowed[item.Label] = true
	}
	required := a.requiredTools(m)

	var failed []string
	for _, ref := range a.Include {
		// The label, because a removal is named one thing at a time and the
		// kind is only there to disambiguate a name that means two things -
		// which a removal list never holds, having no stow packages in it.
		name := ref.Label
		tool, known := byCmd[name]
		switch {
		// Required first, and that order is the point: the required set comes
		// from health.extra_tools, which is a *different* list from tools - so
		// `zsh` is named by the manifest and absent from byCmd, and asking
		// "known?" first refused it for the wrong reason.
		case required[name]:
			a.Report.Line(MarkSkip, name+" is required by the manifest and will not be removed")
			continue
		case mcps[name]:
			if !allowed[name] {
				a.Report.Line(MarkOK, name+" (not installed)")
				continue
			}
			if err := a.removeMcp(ctx, name); err != nil {
				a.Report.Line(MarkWarn, fmt.Sprintf("%s: %v", name, err))
				failed = append(failed, name)
				continue
			}
			continue
		case !known:
			// Not ours to remove. Reported rather than ignored, because a typo
			// silently removing nothing reads as "it was already gone".
			a.Report.Line(MarkWarn, name+" is not a tool this manifest installs")
			failed = append(failed, name)
			continue
		case !allowed[name]:
			// Absent, or present and foreign. Two different facts, and saying
			// "not installed" for the second one is what made a system jq look
			// like a bug in the removal.
			if present[name] {
				// Which manager, not just "the package manager": for a tool
				// that comes from bun on this platform, saying "not by winget"
				// would be true of a thing that was never going to install it.
				manager := a.sourceFor(tool).manager
				if manager == "" {
					manager = a.packageManager()
				}
				a.Report.Line(MarkSkip, fmt.Sprintf("%s is installed, but not by %s - left alone",
					name, manager))
				continue
			}
			a.Report.Line(MarkOK, name+" (not installed)")
			continue
		}

		src := a.sourceFor(tool)
		if a.DryRun {
			a.Report.Line(MarkChange, fmt.Sprintf("would remove %s (%s)", name, src.name))
			continue
		}
		if err := a.uninstall(ctx, src); err != nil {
			// Homebrew refusing because something depends on it is the correct
			// answer, so the message it gave is the useful part.
			a.Report.Line(MarkWarn, fmt.Sprintf("%s: %v", name, err))
			failed = append(failed, name)
			continue
		}
		a.Report.Line(MarkChange, "removed "+name)
	}

	if len(failed) > 0 {
		// Of the names, not of the attempts: a typo was never attempted and a
		// tool that was already gone needed no attempt, and both belong in a
		// fraction the reader can check against what they typed. Saying which
		// set the denominator is costs four words and removes the ambiguity.
		return fmt.Errorf("%d of the %d named did not come off: %s",
			len(failed), len(a.Include), strings.Join(failed, ", "))
	}
	return nil
}

// reportNothingNamed is `doti uninstall` with no selection: the list of what
// could have been named, and nothing removed.
func (a *App) reportNothingNamed(removable []Component) error {
	if len(removable) == 0 {
		a.Report.Line(MarkOK, "nothing this repository installed is still present")
		return nil
	}
	names := make([]string, 0, len(removable))
	for _, item := range removable {
		names = append(names, item.Label)
	}
	a.Report.Line(MarkSkip, "name what to remove; nothing was removed")
	a.Report.Line(MarkNone, "removable: "+strings.Join(names, ", "))
	a.Report.Summary("re-run with --tools " + strings.Join(names, ",") +
		", or pick them in the window")
	return nil
}

// packageManager names the thing that would have had to install a tool for this
// to be willing to remove it. For the message, so it says `brew` on a Mac and
// `winget` on Windows rather than "the package manager".
func (a *App) packageManager() string {
	if a.Platform == manifest.Windows {
		return "winget"
	}
	return "brew"
}

// removeMcp uninstalls one global npm package.
//
// Reported per package rather than as one batch, unlike the install: a removal
// is named one at a time, so "removed X" for each is the same granularity the
// reader asked in.
func (a *App) removeMcp(ctx context.Context, pkg string) error {
	if a.DryRun {
		a.Report.Line(MarkChange, fmt.Sprintf("would remove %s (npm -g)", pkg))
		return nil
	}
	if !a.Runner.Look("npm") {
		a.Report.Line(MarkSkip, "npm is not installed, so "+pkg+" cannot be removed")
		return nil
	}
	if err := a.Runner.Run(ctx, "npm", "uninstall", "-g", pkg); err != nil {
		return err
	}
	a.Report.Line(MarkChange, "removed "+pkg)
	return nil
}

// uninstall runs the platform's package manager.
//
// Deliberately without --ignore-dependencies or --force: the whole point of
// asking a package manager rather than deleting files is that it knows what
// else would break.
func (a *App) uninstall(ctx context.Context, src toolSource) error {
	switch src.manager {
	case managerBun:
		return a.Runner.Run(ctx, "bun", "remove", "-g", src.name)
	case managerWinget:
		return a.Runner.Run(ctx, "winget", "uninstall", "--id", src.name,
			"--exact", "--accept-source-agreements")
	}
	return a.Runner.Run(ctx, "brew", "uninstall", src.name)
}
