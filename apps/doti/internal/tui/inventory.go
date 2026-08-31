package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
)

// Keeping the selectors' lists true after a run has changed the machine.
//
// They used to be read once, before the program started, and never again - so a
// removal that worked left the tools it had just uninstalled still saying
// "installed", and an install left the configs it had just linked still saying
// "not linked". Every list was a description of the machine as it was when the
// window opened.

// Inventory is what the selectors offer: the general list, and the removable
// one. Two lists because they answer different questions - what is on this
// machine, and what this tool is willing to delete.
type Inventory struct {
	Components []app.Component
	Removable  []app.Component
}

// ScanFunc re-reads the machine. main wires this to internal/app; a nil one
// means the lists never change, which is what a test wants.
type ScanFunc func(ctx context.Context) (Inventory, error)

// inventoryMsg carries a fresh scan.
type inventoryMsg Inventory

// rescan reads the machine again, in the background.
//
// A failure is silence. The lists it would have replaced are the ones already on
// screen, and they were right a moment ago - a menu that reports a failed
// re-scan is louder than the staleness it is complaining about, and the next
// run reports the truth either way.
func rescan(scan ScanFunc) tea.Cmd {
	if scan == nil {
		return nil
	}
	return func() tea.Msg {
		inventory, err := scan(context.Background())
		if err != nil {
			return nil
		}
		return inventoryMsg(inventory)
	}
}

// adopt replaces the sources the selectors are built from.
//
// Only the sources: m.items is the working copy an open selector is toggling,
// and replacing that would throw away ticks somebody had just made. The next
// openSelector picks the new data up, which is the moment it matters.
func (m Model) adopt(inventory Inventory) Model {
	m.components = inventory.Components
	m.removable = inventory.Removable
	return m
}
