package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/riptone/tone.rip/apps/doti/internal/pkgs"
	"github.com/riptone/tone.rip/apps/doti/internal/stow"
)

// packagesLabel names the tool set as one selectable component. A constant
// because Include is matched against it: the selector offering one string and
// the installer checking another is a checkbox that silently does nothing.
const packagesLabel = "brew packages"

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
	Label string
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
func (a *App) MenuItems() ([]Component, error) {
	m, err := a.Manifest()
	if err != nil {
		return nil, err
	}
	ignorer, err := a.Ignorer()
	if err != nil {
		return nil, err
	}

	status := pkgs.Inspect(m, a.Runner)
	total := len(status.Present) + len(status.Missing)
	items := []Component{{
		Group:    "Packages",
		Label:    packagesLabel,
		Status:   fmt.Sprintf("%d of %d present", len(status.Present), total),
		Done:     len(status.Missing) == 0,
		Selected: true,
	}}
	for _, extra := range m.Extras {
		if len(extra.Platforms) == 0 || slices.Contains(extra.Platforms, a.Platform) {
			items = append(items, Component{
				Group: "Packages", Label: extra.Name, Status: "not checked", Selected: true,
			})
		}
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
			Group: "Configs", Label: pkg.Name, Status: state, Done: done, Selected: true,
		})
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
			Group: "Secrets", Label: secret.Name, Status: state, Done: done, Selected: true,
		})
	}
	return items, nil
}
