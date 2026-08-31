package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/riptone/tone.rip/apps/doti/internal/pkgs"
	"github.com/riptone/tone.rip/apps/doti/internal/stow"
	"github.com/riptone/tone.rip/apps/doti/internal/tui"
)

// MenuItems describes the machine to the selector: what is installed, what is
// linked, what has been rendered.
//
// Everything defaults to ticked, because re-running a step is how drift gets
// repaired and the common case is "yes, all of it".
func (a *App) MenuItems() ([]tui.Item, error) {
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
	items := []tui.Item{{
		Group:    "Packages",
		Label:    "brew packages",
		Status:   fmt.Sprintf("%d of %d present", len(status.Present), total),
		Done:     len(status.Missing) == 0,
		Selected: true,
	}}
	for _, extra := range m.Extras {
		if len(extra.Platforms) == 0 || slices.Contains(extra.Platforms, a.Platform) {
			items = append(items, tui.Item{
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
		items = append(items, tui.Item{
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
		items = append(items, tui.Item{
			Group: "Secrets", Label: secret.Name, Status: state, Done: done, Selected: true,
		})
	}
	return items, nil
}
