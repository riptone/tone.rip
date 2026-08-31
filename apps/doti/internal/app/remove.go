package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
	"github.com/riptone/tone.rip/apps/doti/internal/pkgs"
)

// Removing packages, which is the one operation here that deletes software.
//
// The README said this was deliberately absent, and the reason still holds:
// deleting somebody's `node` because a manifest changed would be a bad
// surprise. So it exists now under four rules, and the rules are the feature:
//
//   - It removes exactly what it was *named*. Include is the list, and an empty
//     Include removes nothing - there is no spelling of this that means "all"
//     unless somebody typed one.
//   - It refuses anything the manifest does not list. A tool this repository
//     never installed is not this repository's to remove.
//   - It refuses the tools the manifest calls required. `brew`, `git`, `stow`
//     and `zsh` are how the machine gets back to a working state, and a command
//     that can remove its own package manager is a foot-gun with a name.
//   - It does not pass --ignore-dependencies. Homebrew refusing because
//     something depends on a formula is the correct answer, reported rather
//     than overridden.

// Removable is the manifest's tools that are installed and that this tool is
// willing to remove.
//
// The selector's list for a removal, which is also why they arrive unticked:
// see MenuItems for the shape.
func (a *App) Removable() ([]Component, error) {
	m, err := a.Manifest()
	if err != nil {
		return nil, err
	}
	required := a.requiredTools(m)

	status := pkgs.Inspect(m, a.Runner)
	out := make([]Component, 0, len(status.Present))
	for _, tool := range status.Present {
		if required[tool.Cmd] {
			continue
		}
		if a.packageFor(tool) == "" {
			// Nothing to hand a package manager: it arrived some other way, so
			// removing it is not this tool's business either.
			continue
		}
		out = append(out, Component{
			Group: "Packages", Label: tool.Cmd,
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

// packageFor is the name to hand this platform's package manager, or "" when
// the manifest names none.
func (a *App) packageFor(tool manifest.Tool) string {
	if a.Platform == manifest.Windows {
		return tool.Winget
	}
	return tool.Brew
}

// RemovePackages uninstalls the tools named in Include.
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
	removable, err := a.Removable()
	if err != nil {
		return err
	}

	if len(a.Include) == 0 {
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

	byCmd := map[string]manifest.Tool{}
	for _, tool := range m.Tools {
		byCmd[tool.Cmd] = tool
	}
	allowed := map[string]bool{}
	for _, item := range removable {
		allowed[item.Label] = true
	}
	required := a.requiredTools(m)

	var failed []string
	for _, name := range a.Include {
		tool, known := byCmd[name]
		switch {
		// Required first, and that order is the point: the required set comes
		// from health.extra_tools, which is a *different* list from tools - so
		// `zsh` is named by the manifest and absent from byCmd, and asking
		// "known?" first refused it for the wrong reason.
		case required[name]:
			a.Report.Line(MarkSkip, name+" is required by the manifest and will not be removed")
			continue
		case !known:
			// Not ours to remove. Reported rather than ignored, because a typo
			// silently removing nothing reads as "it was already gone".
			a.Report.Line(MarkWarn, name+" is not a tool this manifest installs")
			failed = append(failed, name)
			continue
		case !allowed[name]:
			a.Report.Line(MarkOK, name+" (not installed)")
			continue
		}

		pkg := a.packageFor(tool)
		if a.DryRun {
			a.Report.Line(MarkChange, fmt.Sprintf("would remove %s (%s)", name, pkg))
			continue
		}
		if err := a.uninstall(ctx, pkg); err != nil {
			// Homebrew refusing because something depends on it is the correct
			// answer, so the message it gave is the useful part.
			a.Report.Line(MarkWarn, fmt.Sprintf("%s: %v", name, err))
			failed = append(failed, name)
			continue
		}
		a.Report.Line(MarkChange, "removed "+name)
	}

	if len(failed) > 0 {
		return fmt.Errorf("%d of %d did not come off: %s",
			len(failed), len(a.Include), strings.Join(failed, ", "))
	}
	return nil
}

// uninstall runs the platform's package manager.
//
// Deliberately without --ignore-dependencies or --force: the whole point of
// asking a package manager rather than deleting files is that it knows what
// else would break.
func (a *App) uninstall(ctx context.Context, pkg string) error {
	if a.Platform == manifest.Windows {
		return a.Runner.Run(ctx, "winget", "uninstall", "--id", pkg,
			"--exact", "--accept-source-agreements")
	}
	return a.Runner.Run(ctx, "brew", "uninstall", pkg)
}
