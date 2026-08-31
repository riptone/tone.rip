package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// The release check, and what the footer does with it.
//
// Run once when the window opens, in the background, on a short deadline. The
// result is one optional footer hint and nothing downstream depends on it - so
// a failure is silence rather than an error. A menu that shouts about DNS is
// worse than one that never mentions updates.

// updateFoundMsg carries the newer version, or is never sent.
type updateFoundMsg string

// checkUpdate asks whether there is a newer release.
//
// A nil check - a test, a build with no network story - returns no command at
// all, which is cheaper than a command that returns nothing.
func checkUpdate(check CheckFunc) tea.Cmd {
	if check == nil {
		return nil
	}
	return func() tea.Msg {
		version, err := check(context.Background())
		if err != nil || version == "" {
			return nil
		}
		return updateFoundMsg(version)
	}
}
