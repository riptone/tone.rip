// Package health answers "is this machine actually set up", without changing
// anything.
//
// It is the read-only half of the installer, and it is deliberately separate
// from the code that does the linking: `doti check` has to be safe to run
// from a login shell or a script, and the way to guarantee that is for it to
// have no writing code in it at all.
package health

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// Kind separates the two things that can be wrong.
type Kind string

const (
	// KindTool is a binary that should be on PATH.
	KindTool Kind = "tool"
	// KindLink is a path in $HOME that should resolve into the repo.
	KindLink Kind = "link"
)

// Finding is one checked thing.
type Finding struct {
	Kind Kind
	Name string
	OK   bool
	// Detail says what is wrong, and is empty when nothing is.
	Detail string
}

// Report is everything checked, in the order it was checked.
type Report struct {
	Findings []Finding
}

// Missing is the findings that failed.
func (r Report) Missing() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if !f.OK {
			out = append(out, f)
		}
	}
	return out
}

// OK reports whether everything checked out.
func (r Report) OK() bool { return len(r.Missing()) == 0 }

// Counts returns how many passed and how many were checked.
func (r Report) Counts() (passed, total int) {
	for _, f := range r.Findings {
		if f.OK {
			passed++
		}
	}
	return passed, len(r.Findings)
}

// Options is what Check needs to look at a machine.
type Options struct {
	Manifest *manifest.Manifest
	Platform manifest.Platform
	Repo     string
	Home     string
	// Look reports whether a command is on PATH. Injected so tests never
	// depend on what happens to be installed.
	Look func(string) bool
}

// Check inspects the machine and reports what it finds.
func Check(opts Options) Report {
	var report Report
	m := opts.Manifest

	for _, tool := range m.Tools {
		report.Findings = append(report.Findings, checkTool(opts, tool.Cmd))
	}

	// Tools the manifest wants present but does not install through a
	// package manager - zsh, brew itself, stow. Checked per platform because
	// `brew` on Windows is not a gap, it is a category error.
	if m.Health != nil {
		for _, name := range m.Health.ExtraTools[opts.Platform] {
			if slices.ContainsFunc(m.Tools, func(t manifest.Tool) bool { return t.Cmd == name }) {
				continue
			}
			report.Findings = append(report.Findings, checkTool(opts, name))
		}

		targets := make([]string, 0, len(m.Health.Links[opts.Platform]))
		for target := range m.Health.Links[opts.Platform] {
			targets = append(targets, target)
		}
		// Map iteration is random and this output is read by a human and
		// diffed in tests.
		slices.Sort(targets)
		for _, target := range targets {
			report.Findings = append(report.Findings,
				checkLink(opts, target, m.Health.Links[opts.Platform][target]))
		}
	}

	return report
}

func checkTool(opts Options, name string) Finding {
	if opts.Look(name) {
		return Finding{Kind: KindTool, Name: name, OK: true}
	}
	return Finding{Kind: KindTool, Name: name, Detail: "not on PATH"}
}

// checkLink resolves a target and compares where it lands.
//
// Resolved rather than read: a package can be linked through a *folded*
// parent - ~/.config/ghostty is reached via a link at ~/.config/ghostty, but
// ~/.zsh/aliases.zsh is reached through a link at ~/.zsh. Reading the link at
// the leaf would call the second case broken when it is exactly right.
func checkLink(opts Options, target, source string) Finding {
	finding := Finding{Kind: KindLink, Name: target}

	path := target
	if strings.HasPrefix(path, "~/") {
		path = filepath.Join(opts.Home, filepath.FromSlash(strings.TrimPrefix(path, "~/")))
	}
	want := filepath.Join(opts.Repo, filepath.FromSlash(source))

	if _, err := os.Lstat(path); err != nil {
		finding.Detail = "missing"
		return finding
	}

	// Asked before "does it point at the right file", because a real copy
	// resolves to its own path and would otherwise be reported as pointing
	// somewhere odd - which is true but unhelpful. A file where a link
	// belongs is drift: the repo moves on and the machine does not follow.
	if !linkedSomewhere(path, opts.Home) {
		finding.Detail = "is a copy, not a link"
		return finding
	}

	got, err := filepath.EvalSymlinks(path)
	if err != nil {
		finding.Detail = "is a broken link"
		return finding
	}
	wantResolved, err := filepath.EvalSymlinks(want)
	if err != nil {
		finding.Detail = "the repo has no " + source
		return finding
	}
	if got != wantResolved {
		finding.Detail = "points at " + got
		return finding
	}

	finding.OK = true
	return finding
}

// linkedSomewhere reports whether path, or any parent up to home, is a
// symlink - which is what "reached through a fold" looks like on disk.
func linkedSomewhere(path, home string) bool {
	for current := path; len(current) > len(home); current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return false
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}
