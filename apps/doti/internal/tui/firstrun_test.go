package tui

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
)

// The one row a machine with no checkout can honestly be described by.
func repoOnly() []app.Component {
	return []app.Component{{
		Group: "Repository", Kind: app.KindRepo, Label: "~/dotfiles",
		Status: "not cloned", Selected: true,
	}}
}

func firstRun(asks bool) Model {
	return New(Config{
		Components: repoOnly(),
		Version:    "v1.0.0",
		Width:      80,
		Height:     26,
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Run:        noWork,
		Start:      ActionInstall,
		StartAsks:  asks,
	})
}

// Bare `doti` on a fresh machine opens the install screen. It used to *be* an
// install: clone, packages, symlinks over $HOME, with no screen and no keypress
// between the reader and any of it.
func TestAFirstRunOpensTheSelectorRatherThanInstalling(t *testing.T) {
	m := firstRun(true)

	if m.screen != ScreenSelect {
		t.Fatalf("a first run opened screen %v, not the selector", m.screen)
	}
	if m.Launched() {
		t.Error("it started a run")
	}
	body := plain(m)
	for _, want := range []string{"Repository", "~/dotfiles", "not cloned", "enter confirm"} {
		if !strings.Contains(body, want) {
			t.Errorf("the screen does not say %q:\n%s", want, body)
		}
	}
}

// And `doti install`, which was asked for by name, still runs on sight -
// including the one scripts/install.sh ends with.
func TestANamedInstallStillRunsImmediately(t *testing.T) {
	m := firstRun(false)

	if m.screen == ScreenSelect {
		t.Error("a named install stopped to ask")
	}
	if !m.Launched() {
		t.Error("a named install did not start")
	}
}

// Enter on that one row hands the operation the row, which is the whole of what
// there was to choose - and internal/app drops it after the clone, because a
// selection made against a machine with no manifest cannot narrow one.
func TestEnterOnTheFirstRunStartsTheInstall(t *testing.T) {
	m := tap(firstRun(true), "enter")

	if !m.Launched() {
		t.Fatalf("enter did nothing: screen %v", m.screen)
	}
	if m.run.action != ActionInstall {
		t.Errorf("enter started %q", m.run.action)
	}
}
