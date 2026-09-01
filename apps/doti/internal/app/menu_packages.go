// The Packages group of the selector: what a `brew bundle` or a `winget import`
// would install, offered one thing at a time.
//
// It used to be one row per *list* - "brew packages, 11 of 16 present" - so the
// only answer available was all or nothing. Each list is a parent now with its
// members folded underneath, and the group reads exactly as it did until
// somebody opens one.
package app

import (
	"context"
	"fmt"
	"slices"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
	"github.com/riptone/tone.rip/apps/doti/internal/pkgs"
)

// packageItems describes everything the packages phase installs.
//
// A parent is followed immediately by its children, because that adjacency is
// what the fold is drawn from: a child separated from its parent by another row
// would fold away under the wrong one.
func (a *App) packageItems(ctx context.Context, m *manifest.Manifest) ([]Component, error) {
	present := a.toolsPresent(m)
	tools := a.toolsFor(m)
	have := len(present)
	items := []Component{{
		Group: "Packages", Kind: KindTools, Label: packagesLabel,
		Status:   fmt.Sprintf("%d of %d present", have, len(tools)),
		Done:     have == len(tools),
		Selected: true,
	}}
	for _, tool := range tools {
		items = append(items, child(KindTool, packagesLabel, tool.Cmd, present[tool.Cmd]))
	}

	// The other two lists `brew bundle` installs, or the one `winget import`
	// does.
	//
	// Asked about, not merely declared, and that changed for a reason: the Adopt
	// selector is meant to show what is *left*, and a row with no present /
	// missing answer can only ever be shown. So the GUI apps and the plugins
	// were every one of them on a list of "what is missing" on a machine that
	// had them all. App.Owned is the same inventory the removal reads, cached
	// per invocation, and it is exactly the predicate that decides whether the
	// install step is a no-op: `brew bundle` skips what brew already owns.
	owned, err := a.Owned(ctx)
	if err != nil {
		return nil, err
	}
	if a.Platform == manifest.Windows {
		items = append(items, inventoried(KindWingetExtras, KindWingetExtra,
			wingetExtrasLabel, m.WingetExtras, owned)...)
	} else {
		items = append(items, inventoried(KindCasks, KindCask,
			casksLabel, caskNames(a.casksFor(m)), owned)...)
		items = append(items, inventoried(KindPlugins, KindPlugin,
			pluginsLabel, pluginNames(m.ZshPlugins), owned)...)
	}

	for _, extra := range m.Extras {
		if len(extra.Platforms) > 0 && !slices.Contains(extra.Platforms, a.Platform) {
			continue
		}
		if !installableExtras[extra.Name] {
			// A manifest may declare any name; only these have code behind
			// them. Offering the rest would be a checkbox that does nothing,
			// which is the thing every other gate in this file removed.
			continue
		}
		count, known := a.extraPresent(extra.Name)
		items = append(items, extraComponent(extra.Name, count, known))
	}

	mcps, err := a.mcpItems(ctx, m)
	if err != nil {
		return nil, err
	}
	return append(items, mcps...), nil
}

// installableExtras are the manifest extras this binary knows how to install.
//
// A list rather than a hardcoded name at the one call site, so the selector and
// the installer cannot disagree about which extras exist: install.go used to ask
// for "nerd-font" by name while the selector offered a row for every declared
// extra, so a second one would have been ticked and then ignored.
var installableExtras = map[string]bool{"nerd-font": true}

// extraPresent reports how many of an extra this machine already has, and
// whether the question can be answered at all.
func (a *App) extraPresent(name string) (count int, known bool) {
	if name == "nerd-font" {
		// On macOS the font is a cask, and the cask row answers for it.
		if a.Platform == manifest.MacOS {
			return 0, false
		}
		return a.nerdFontFaces(), true
	}
	return 0, false
}

// extraComponent is one extra, with a real state where there is one.
func extraComponent(name string, count int, known bool) Component {
	item := Component{
		Group: "Packages", Kind: KindExtra, Label: name,
		Status: "not checked", Selected: true,
	}
	switch {
	case !known:
		return item
	case count > 0:
		item.Status, item.Done = fmt.Sprintf("%d faces installed", count), true
	default:
		item.Status = "missing"
	}
	return item
}

// toolsPresent is which of the tools this platform can install are already
// there, keyed by command name.
//
// Over toolsFor rather than over the whole tool list, and shared with
// InstallPackages so the selector and the run report the same number. They did
// not: `code` is declared with a winget id and no brew formula, so on a Mac the
// run said "16 of 16 tools present" one line above "the bundle covers 15
// tool(s)" while the selector said "15 of 15". Three numbers, one fact.
func (a *App) toolsPresent(m *manifest.Manifest) map[string]bool {
	installed := map[string]bool{}
	for _, tool := range pkgs.Inspect(m, a.Runner).Present {
		installed[tool.Cmd] = true
	}
	present := map[string]bool{}
	for _, tool := range a.toolsFor(m) {
		if installed[tool.Cmd] {
			present[tool.Cmd] = true
		}
	}
	return present
}

// toolsFor is the manifest's tools that this platform has a package name for.
//
// A tool the manifest hands this platform nothing to install is not a choice to
// offer: the box would be one that does nothing, which is the thing this whole
// change exists to remove.
func (a *App) toolsFor(m *manifest.Manifest) []manifest.Tool {
	out := make([]manifest.Tool, 0, len(m.Tools))
	for _, tool := range m.Tools {
		if a.packageFor(tool) != "" {
			out = append(out, tool)
		}
	}
	return out
}

// casksFor is the casks declared for this platform.
func (a *App) casksFor(m *manifest.Manifest) []manifest.Cask {
	out := make([]manifest.Cask, 0, len(m.BrewCasks))
	for _, cask := range m.BrewCasks {
		if cask.Brew == "" {
			continue
		}
		if len(cask.Platforms) > 0 && !slices.Contains(cask.Platforms, a.Platform) {
			continue
		}
		out = append(out, cask)
	}
	return out
}

// mcpItems is the global npm packages.
func (a *App) mcpItems(ctx context.Context, m *manifest.Manifest) ([]Component, error) {
	if len(m.Mcps) == 0 {
		return nil, nil
	}
	// A real count, where this used to say "N declared" because `npm ls -g` was
	// too slow for a screen somebody is waiting on. It still is; pkgs.NpmGlobals
	// asks `npm root -g` once and stats instead.
	installed, err := a.Globals(ctx)
	if err != nil {
		return nil, err
	}
	items := []Component{{
		Group: "Packages", Kind: KindMcps, Label: mcpLabel,
		Status:   fmt.Sprintf("%d of %d present", len(installed), len(m.Mcps)),
		Done:     len(installed) == len(m.Mcps),
		Selected: true,
	}}
	for _, pkg := range m.Mcps {
		items = append(items, child(KindMcp, mcpLabel, pkg, installed[pkg]))
	}
	return items, nil
}

// inventoried is a parent and its children, with presence read from a package
// manager's own inventory rather than from PATH.
//
// PATH is the wrong question here in both directions: a cask puts nothing on it
// (`command -v ghostty` fails on a machine where Ghostty is running), and a font
// is not a command at all.
func inventoried(parentKind, childKind Kind, label string, names []string,
	owned map[string]bool,
) []Component {
	if len(names) == 0 {
		return nil
	}
	var have int
	for _, name := range names {
		if owned[name] {
			have++
		}
	}
	items := []Component{{
		Group: "Packages", Kind: parentKind, Label: label,
		Status:   fmt.Sprintf("%d of %d present", have, len(names)),
		Done:     have == len(names),
		Selected: true,
	}}
	for _, name := range names {
		items = append(items, child(childKind, label, name, owned[name]))
	}
	return items
}

// child is one member of a list whose presence the machine was asked about.
func child(kind Kind, parent, label string, present bool) Component {
	status := "missing"
	if present {
		status = "installed"
	}
	return Component{
		Group: "Packages", Kind: kind, Parent: parent, Label: label,
		Status: status, Done: present, Selected: true,
	}
}

func caskNames(casks []manifest.Cask) []string {
	out := make([]string, 0, len(casks))
	for _, cask := range casks {
		out = append(out, cask.Brew)
	}
	return out
}

func pluginNames(plugins []manifest.ZshPlugin) []string {
	out := make([]string, 0, len(plugins))
	for _, plugin := range plugins {
		if plugin.Brew != "" {
			out = append(out, plugin.Brew)
		}
	}
	return out
}
