package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"

	"github.com/riptone/tone.rip/apps/doti/internal/stow"
)

// packagesLabel names the tool set as one selectable component. A constant
// because Include is matched against it: the selector offering one string and
// the installer checking another is a checkbox that silently does nothing.
const packagesLabel = "brew packages"

// casksLabel and pluginsLabel name the other two lists `brew bundle` installs.
//
// They were folded into packagesLabel until the selector could tick individual
// tools, at which point they had to come out: rendering a tools-only Brewfile
// because one tool was unticked would have declined every GUI app and both zsh
// plugins as a side effect. Three lists, three rows, three counts.
const (
	casksLabel   = "brew casks"
	pluginsLabel = "zsh plugins"
)

// wingetExtrasLabel is the Windows equivalent of the casks: the GUI apps that
// are not a tool on PATH.
const wingetExtrasLabel = "winget extras"

// mcpLabel names the manifest's global npm packages as one selectable
// component.
//
// They were the one thing an install did that the selector never offered:
// untick every box and seven MCP servers were still installed, because the
// phase that installs them was never asked. Matched against Include like the
// rest, so the constant is shared for the same reason packagesLabel is.
const mcpLabel = "mcp servers"

// gitLocalName is the name the manifest already gives the machine-local git
// config, in system_components.
//
// And it has never been a system *link*: SystemLinks() returns nothing by that
// name on any platform, because writeGitLocal writes the file rather than
// linking it. So the declaration was a checkbox with nothing behind it - the
// last thing an install wrote that the selector could not turn off - and the
// entry has to carry writeGitLocal's own state rather than the "system link"
// the other two get.
//
// A constant because Include is matched against it, for the same reason
// packagesLabel is one.
const gitLocalName = "gitconfig-local"

// Kind is what sort of thing a component is.
//
// Selectors use it to offer a subset: Unlink acts on stow packages and nothing
// else, so a list that also offered `brew packages` and the secrets would be
// three quarters checkboxes that change nothing.
type Kind string

const (
	// KindTools is the manifest's whole tool set, as one component - the parent
	// the individual tools fold under.
	KindTools Kind = "tools"
	// KindTool is one tool.
	KindTool Kind = "tool"
	// KindCasks is the manifest's GUI apps and fonts, as one component.
	KindCasks Kind = "casks"
	// KindCask is one of them.
	KindCask Kind = "cask"
	// KindPlugins is the manifest's zsh plugins, as one component.
	KindPlugins Kind = "plugins"
	// KindPlugin is one of them.
	KindPlugin Kind = "plugin"
	// KindExtra is a manifest extra: the things no package manager covers.
	KindExtra Kind = "extra"
	// KindMcps is the manifest's whole MCP server set, as one component.
	KindMcps Kind = "mcps"
	// KindMcp is one installed MCP server, offered for removal.
	KindMcp Kind = "mcp"
	// KindStow is a stow package: a directory of configs linked into $HOME.
	KindStow Kind = "stow"
	// KindSystem is a link whose target lives outside $HOME.
	KindSystem Kind = "system"
	// KindGitLocal is ~/.gitconfig.local.
	KindGitLocal Kind = "git-local"
	// KindWingetExtras is the manifest's Windows GUI apps, as one component,
	// and KindWingetExtra is one of them.
	KindWingetExtras Kind = "winget-extras"
	KindWingetExtra  Kind = "winget-extra"
	// KindSecret is a file rendered from the vault.
	KindSecret Kind = "secret"
)

// Component is one thing on the machine a selector can include or leave out.
//
// It lives here rather than in internal/tui because this package must not
// import a UI. MenuItems used to return a slice of the window's own type,
// which had the domain depending on the thing that draws it - and made a
// Reporter that sends Bubble Tea messages impossible to write without an
// import cycle.
type Component struct {
	// Group is the heading it sits under: "Packages", "Configs", "Secrets".
	Group string
	// Kind is what sort of thing it is, for the selectors that offer a subset.
	Kind Kind
	// Parent is the label of the component this one belongs to, or "" for a
	// top-level row.
	//
	// The selector folds children away under their parent, which is what makes
	// offering all sixteen tools individually cost nothing on screen: the group
	// reads exactly as it did when it was one row, until somebody opens it.
	Parent string
	Label  string
	// Status is the machine's current state for it - "installed", "linked",
	// "not linked". Shown dim, on the right.
	Status string
	// Done means the machine already has it. It stays selectable, because
	// re-linking is how drift gets repaired.
	Done bool
	// Selected is the checkbox. Defaults on.
	Selected bool
}

// MenuItems describes the machine to the selector: what is installed, what is
// linked, what has been rendered.
//
// Everything defaults to ticked, because re-running a step is how drift gets
// repaired and the common case is "yes, all of it".
func (a *App) MenuItems(ctx context.Context) ([]Component, error) {
	m, err := a.Manifest()
	if err != nil {
		return nil, err
	}
	ignorer, err := a.Ignorer()
	if err != nil {
		return nil, err
	}

	items, err := a.packageItems(ctx, m)
	if err != nil {
		return nil, err
	}

	// Only is deliberately ignored here: the selector is how you choose, so
	// narrowing the list it offers would be answering the question twice.
	packages, err := (&App{
		Repo: a.Repo, Home: a.Home, Platform: a.Platform,
		Report: a.Report, Runner: a.Runner, manifest: m, ignorer: ignorer,
	}).Packages()
	if err != nil {
		return nil, err
	}
	for _, pkg := range packages {
		state, done := "not linked", false
		if ops, err := stow.Plan(filepath.Join(a.Repo, pkg.Name), a.Home, ignorer); err == nil {
			counts := stow.Count(ops)
			switch {
			case counts[stow.Link] == 0 && counts[stow.Relink] == 0 && counts[stow.Unfold] == 0:
				state, done = "linked", true
			case counts[stow.Skip] > 0:
				state = "partly linked"
			}
		}
		items = append(items, Component{
			Group: "Configs", Kind: KindStow, Label: pkg.Name,
			Status: state, Done: done, Selected: true,
		})
	}

	// The links whose targets live outside $HOME. A manifest list like the
	// extras, so it is offered like them - on Windows, where they exist.
	var sawGitLocal bool
	for _, component := range m.SystemComponents {
		if len(component.Platforms) > 0 && !slices.Contains(component.Platforms, a.Platform) {
			continue
		}
		if component.Name == gitLocalName {
			// Declared here, but written rather than linked. One row about one
			// file, carrying the state of the thing that actually writes it.
			sawGitLocal, items = true, append(items, a.gitLocalComponent())
			continue
		}
		items = append(items, Component{
			Group: "Configs", Kind: KindSystem, Label: component.Name,
			Status: "system link", Selected: true,
		})
	}
	if !sawGitLocal {
		// Offered whether or not the manifest declares it, because an install
		// writes it either way - and a checkbox that exists only for some
		// manifests is a step that silently stops happening for the others.
		items = append(items, a.gitLocalComponent())
	}

	for _, secret := range m.Secrets {
		if !secret.WantsPlatform(a.Platform) {
			continue
		}
		state, done := "not rendered", false
		if _, err := os.Stat(a.Expand(secret.Target)); err == nil {
			state, done = "rendered", true
		}
		items = append(items, Component{
			Group: "Secrets", Kind: KindSecret, Label: secret.Name,
			Status: state, Done: done, Selected: true,
		})
	}
	return items, nil
}

// gitLocalComponent describes ~/.gitconfig.local for the selector.
//
// Three states rather than two, because "a secret renders it" is the one where
// ticking the box does nothing and the reader deserves to know why before they
// tick it: writeGitLocal yields to whichever mechanism the manifest names.
func (a *App) gitLocalComponent() Component {
	path := filepath.Join(a.Home, ".gitconfig.local")
	item := Component{
		Group: "Configs", Kind: KindGitLocal, Label: gitLocalName, Selected: true,
	}
	switch {
	case a.secretOwning(path) != "":
		item.Status, item.Done = "rendered from a secret", true
	case fileExists(path):
		item.Status, item.Done = "written", true
	default:
		item.Status = "not written"
	}
	return item
}

// fileExists is os.Stat with the error thrown away, for the times the only
// question is whether something is there.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
