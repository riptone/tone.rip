// The files and links that are not a stow package: the machine-local git
// config, and the paths an application insists on keeping somewhere else.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// writeGitLocal writes the machine-local git config that the tracked
// .gitconfig includes.
//
// It holds the per-machine credential helper, which is why it cannot be
// tracked: the answer differs by OS, and in a shared repository it would be
// one machine's answer imposed on all of them.
//
// An existing file is never touched. It is the documented place for a
// per-machine identity override, so someone's email is very likely in it.
func (a *App) writeGitLocal() error {
	path := filepath.Join(a.Home, ".gitconfig.local")

	// Yielded, not raced. If a secret declares this path, the secrets phase
	// runs *after* this one and would overwrite whatever is written here -
	// so two mechanisms would be writing one file and the later would win by
	// accident. Whichever the manifest names is the one that owns it.
	if owner := a.secretOwning(path); owner != "" {
		a.Report.Line(MarkSkip, fmt.Sprintf(
			"~/.gitconfig.local (secret %q renders it)", owner))
		return nil
	}

	var helper string
	switch a.Platform {
	case manifest.MacOS:
		helper = "osxkeychain"
	case manifest.Linux:
		helper = "cache --timeout=3600"
	case manifest.Windows:
		helper = "manager"
	}

	if _, err := os.Stat(path); err == nil {
		a.Report.Line(MarkOK, "~/.gitconfig.local (left as it is)")
		return nil
	}
	if a.DryRun {
		a.Report.Line(MarkChange, fmt.Sprintf(
			"would write ~/.gitconfig.local (credential.helper=%s)", helper))
		return nil
	}

	var body strings.Builder
	body.WriteString("# Machine-local git config, written by doti and not tracked.\n")
	body.WriteString("# Safe to edit - a per-machine user.email belongs here.\n")
	if helper != "" {
		fmt.Fprintf(&body, "[credential]\n\thelper = %s\n", helper)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	a.Report.Line(MarkChange, "~/.gitconfig.local")
	return nil
}

// secretOwning returns the name of the secret that renders path, if any.
//
// Compared after expansion, because a manifest writes `~/x` and this code
// holds an absolute path.
func (a *App) secretOwning(path string) string {
	m, err := a.Manifest()
	if err != nil {
		return ""
	}
	for _, secret := range m.Secrets {
		if !secret.WantsPlatform(a.Platform) {
			continue
		}
		if a.Expand(secret.Target) == path {
			return secret.Name
		}
	}
	return ""
}

// SystemLink is a symlink whose target is outside $HOME, so the stow engine
// cannot place it: stow mirrors a package tree into the home directory, and
// these live in Windows' own application-data paths.
type SystemLink struct {
	Name string
	// Source is relative to the repository root.
	Source string
	// Target is absolute, resolved from the environment.
	Target string
}

// SystemLinks are the per-platform links the manifest's system_components
// describe. Empty off Windows, where every config this repo carries is
// reachable from $HOME and therefore already a stow package.
func (a *App) SystemLinks() []SystemLink {
	if a.Platform != manifest.Windows {
		return nil
	}
	var links []SystemLink

	// Windows Terminal stores its settings under its packaged app's
	// LocalState, which is not in $HOME's tree.
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		links = append(links, SystemLink{
			Name:   "windows-terminal",
			Source: filepath.Join("win", "terminal", "settings.json"),
			Target: filepath.Join(localAppData, "Packages",
				"Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json"),
		})
	}
	// $PROFILE for PowerShell 7. Documents is the documented location and is
	// what $PROFILE resolves to on a default install.
	if profile := os.Getenv("USERPROFILE"); profile != "" {
		links = append(links, SystemLink{
			Name:   "powershell-profile",
			Source: filepath.Join("win", "powershell", "profile.ps1"),
			Target: filepath.Join(profile, "Documents", "PowerShell",
				"Microsoft.PowerShell_profile.ps1"),
		})
	}
	return links
}

// installSystemLinks places the links whose targets are outside $HOME.
//
// Reported per link rather than as a group, because on Windows these are the
// two that most often fail: symlinks need Developer Mode or an elevated
// shell, and a silent failure here leaves a terminal with default settings
// and no explanation.
func (a *App) installSystemLinks() error {
	m, err := a.Manifest()
	if err != nil {
		return err
	}
	declared := map[string]bool{}
	for _, component := range m.SystemComponents {
		if len(component.Platforms) == 0 || slices.Contains(component.Platforms, a.Platform) {
			declared[component.Name] = true
		}
	}

	for _, link := range a.SystemLinks() {
		if !declared[link.Name] {
			continue
		}
		source := filepath.Join(a.Repo, link.Source)
		if _, err := os.Stat(source); err != nil {
			a.Report.Line(MarkSkip, fmt.Sprintf("%s: no %s in the repo", link.Name, link.Source))
			continue
		}

		if existing, err := os.Readlink(link.Target); err == nil && existing == source {
			a.Report.Line(MarkOK, link.Name)
			continue
		}
		if a.DryRun {
			a.Report.Line(MarkChange, fmt.Sprintf("would link %s -> %s", link.Name, link.Target))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(link.Target), 0o755); err != nil {
			return fmt.Errorf("%s: creating %s: %w", link.Name, filepath.Dir(link.Target), err)
		}
		// Removed rather than overwritten: os.Symlink refuses an existing
		// path, and whatever is there is either our own older link or a file
		// the user will want back - so it goes to the backup tree first.
		if err := a.displace(link.Target); err != nil {
			return fmt.Errorf("%s: %w", link.Name, err)
		}
		if err := os.Symlink(source, link.Target); err != nil {
			// Not fatal: on Windows this is the Developer Mode failure, and
			// the rest of the install is still worth having.
			a.Report.Line(MarkWarn, fmt.Sprintf(
				"%s: could not link (%v) - Windows needs Developer Mode or an elevated shell",
				link.Name, err))
			continue
		}
		a.Report.Line(MarkChange, link.Name)
	}
	return nil
}

// displace moves whatever is at path into the backup tree, if anything is.
func (a *App) displace(path string) error {
	if _, err := os.Lstat(path); err != nil {
		return nil
	}
	backup := filepath.Join(a.Home, BackupsDir, "system",
		filepath.Base(path)+".displaced")
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		return err
	}
	if err := os.Rename(path, backup); err != nil {
		return fmt.Errorf("backing up %s: %w", path, err)
	}
	a.Report.Line(MarkWarn, "backed up "+path)
	return nil
}
