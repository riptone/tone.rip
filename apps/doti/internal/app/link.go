package app

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/riptone/tone.rip/apps/doti/internal/stow"
)

// Link links every selected stow package into $HOME.
func (a *App) Link() error {
	ignorer, err := a.Ignorer()
	if err != nil {
		return err
	}
	selected, err := a.Packages()
	if err != nil {
		return err
	}

	// One backup directory per run, stamped, so a restore is "put the newest
	// one back" rather than a merge of several runs.
	backupDir := filepath.Join(a.Home, BackupsDir,
		time.Now().UTC().Format("2006-01-02T15-04-05Z"))

	for _, pkg := range selected {
		ops, err := stow.Plan(filepath.Join(a.Repo, pkg.Name), a.Home, ignorer)
		if err != nil {
			return err
		}
		counts := stow.Count(ops)

		// Reported before the work, so a package that displaces something
		// says so whether or not the apply then fails.
		for _, op := range ops {
			if op.Kind == stow.Relink {
				a.Report.Line(MarkWarn, fmt.Sprintf(
					"%s: backing up %s (%s)", pkg.Name, op.Target, op.Reason))
			}
		}

		if err := stow.Apply(ops, backupDir, a.Home, a.DryRun); err != nil {
			return err
		}

		changed := counts[stow.Link] + counts[stow.Relink] + counts[stow.Unfold]
		mark := MarkOK
		if changed > 0 {
			mark = MarkChange
		}
		a.Report.Line(mark, fmt.Sprintf("%-10s %s", pkg.Name, describe(counts, a.DryRun)))
	}
	return nil
}

// describe turns a plan's tally into one readable clause.
func describe(counts map[stow.Kind]int, dryRun bool) string {
	linked := counts[stow.Link]
	if linked == 0 && counts[stow.Relink] == 0 && counts[stow.Unfold] == 0 {
		return fmt.Sprintf("already linked (%d)", counts[stow.Skip])
	}
	verb := "linked"
	if dryRun {
		verb = "would link"
	}
	out := fmt.Sprintf("%s %d", verb, linked)
	if n := counts[stow.Relink]; n > 0 {
		out += fmt.Sprintf(", replaced %d", n)
	}
	if n := counts[stow.Unfold]; n > 0 {
		out += fmt.Sprintf(", unfolded %d", n)
	}
	if n := counts[stow.Skip]; n > 0 {
		out += fmt.Sprintf(", already %d", n)
	}
	return out
}

// Unlink removes the symlinks this repo owns, optionally restoring what they
// displaced.
func (a *App) Unlink(restore bool) error {
	ignorer, err := a.Ignorer()
	if err != nil {
		return err
	}
	selected, err := a.Packages()
	if err != nil {
		return err
	}

	for _, pkg := range selected {
		removed, err := stow.Unlink(filepath.Join(a.Repo, pkg.Name), a.Home, ignorer, a.DryRun)
		if err != nil {
			return err
		}
		verb := "removed"
		if a.DryRun {
			verb = "would remove"
		}
		mark := MarkChange
		if len(removed) == 0 {
			mark = MarkOK
		}
		a.Report.Line(mark, fmt.Sprintf("%-10s %s %d link(s)", pkg.Name, verb, len(removed)))
	}

	if !restore {
		return nil
	}
	return a.RestoreBackups()
}

// RestoreBackups puts the newest backup run back into $HOME.
//
// The newest one only, never a merge of several: each run of the linker
// writes its own timestamped directory, so "put it back" has exactly one
// meaning and installing twice does not bury the original two levels deep.
func (a *App) RestoreBackups() error {
	root := filepath.Join(a.Home, BackupsDir)
	runs, err := stow.Backups(root)
	if err != nil {
		return err
	}
	a.Report.Phase("restore")
	if len(runs) == 0 {
		a.Report.Line(MarkSkip, "no backups under ~/"+BackupsDir)
		return nil
	}

	newest := runs[0]
	restored, err := stow.Restore(newest, a.Home, a.DryRun)
	if err != nil {
		return err
	}
	verb := "restored"
	if a.DryRun {
		verb = "would restore"
	}
	a.Report.Line(MarkChange, fmt.Sprintf("%s %d file(s) from %s",
		verb, len(restored), filepath.Base(newest)))
	for _, path := range restored {
		a.Report.Line(MarkNone, "  "+path)
	}
	if len(runs) > 1 {
		a.Report.Line(MarkSkip, fmt.Sprintf("%d older backup run(s) left alone", len(runs)-1))
	}
	return nil
}
