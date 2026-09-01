package tui

import (
	"fmt"
	"io"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/riptone/tone.rip/apps/doti/internal/app"
)

// What a run costs. A real install reports on the order of forty lines; the
// larger sizes are here because folding one line used to re-wrap all of them,
// which is the shape that only shows up at scale.

func newRun(b *testing.B) Model {
	b.Helper()
	m := New(Config{
		Components: components(),
		Version:    "v1.0.0",
		Width:      80,
		Height:     26,
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Run:        noWork,
	})
	m = press(m, "5", "enter")
	return m
}

// One reported line arriving: parse, render its rows, flow into the viewport.
func BenchmarkFoldOneLine(b *testing.B) {
	for _, n := range []int{40, 400, 4000} {
		b.Run(fmt.Sprintf("after-%d-lines", n), func(b *testing.B) {
			m := newRun(b)
			for i := range n {
				next, _ := m.Update(line(app.MarkOK, fmt.Sprintf("line %d", i)))
				m = next.(Model)
			}
			b.ResetTimer()
			for b.Loop() {
				next, _ := m.Update(line(app.MarkChange, "stow       linked 1"))
				m = next.(Model)
			}
		})
	}
}

// One frame: the card, the body rows, the scrollbar, the footer.
func BenchmarkRenderRunScreen(b *testing.B) {
	m := newRun(b)
	for i := range 400 {
		next, _ := m.Update(line(app.MarkOK, fmt.Sprintf("line %d", i)))
		m = next.(Model)
	}
	b.ResetTimer()
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkRenderMenu(b *testing.B) {
	m := New(Config{
		Components: components(),
		Version:    "v1.0.0",
		Width:      80,
		Height:     26,
		Renderer:   lipgloss.NewRenderer(io.Discard),
		Run:        noWork,
	})
	b.ResetTimer()
	for b.Loop() {
		_ = m.View()
	}
}

// A resize, which is the one thing that legitimately re-wraps everything.
func BenchmarkResizeWithALongRun(b *testing.B) {
	m := newRun(b)
	for i := range 400 {
		next, _ := m.Update(line(app.MarkWarn,
			fmt.Sprintf("zsh: backing up ~/.zsh/plugins/plugin-%d/plugin.zsh", i)))
		m = next.(Model)
	}
	b.ResetTimer()
	width := 80
	for b.Loop() {
		width = 80 + (width+1)%40
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 26})
		m = next.(Model)
	}
}

// The selector, folded and open. Both matter now that a real machine's list is
// forty rows rather than four: isParent and tally each walk the components, and
// the view calls them once per row - so the render is quadratic in the list, and
// the only question worth asking is whether the constant makes that matter.
func BenchmarkRenderSelector(b *testing.B) {
	big := make([]app.Component, 0, 40)
	big = append(big, app.Component{Group: "Packages", Kind: app.KindTools,
		Label: "brew packages", Status: "14 of 15 present", Selected: true})
	for i := range 15 {
		big = append(big, app.Component{Group: "Packages", Kind: app.KindTool,
			Parent: "brew packages", Label: "tool-" + string(rune('a'+i)),
			Status: "installed", Done: true, Selected: true})
	}
	big = append(big, app.Component{Group: "Packages", Kind: app.KindMcps,
		Label: "mcp servers", Status: "7 of 7 present", Selected: true})
	for i := range 7 {
		big = append(big, app.Component{Group: "Packages", Kind: app.KindMcp,
			Parent: "mcp servers", Label: "@scope/server-" + string(rune('a'+i)),
			Status: "installed", Done: true, Selected: true})
	}
	for i := range 8 {
		big = append(big, app.Component{Group: "Configs", Kind: app.KindStow,
			Label: "pkg-" + string(rune('a'+i)), Status: "linked",
			Done: true, Selected: true})
	}

	m := New(Config{
		Components: big, Version: "v1.0.0", Width: 96, Height: 40,
		Renderer: lipgloss.NewRenderer(io.Discard), Run: noWork,
	})
	m = press(m, "enter")

	b.Run("folded", func(b *testing.B) {
		for range b.N {
			_ = m.View()
		}
	})

	open := press(press(m, "right"), "G")
	b.Run("open", func(b *testing.B) {
		for range b.N {
			_ = open.View()
		}
	})
}
