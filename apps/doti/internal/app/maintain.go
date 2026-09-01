package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/riptone/tone.rip/apps/doti/internal/health"
	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
	"github.com/riptone/tone.rip/apps/doti/internal/secrets"
)

// Secrets renders every secret the manifest declares.
func (a *App) Secrets(ctx context.Context) error {
	m, err := a.Manifest()
	if err != nil {
		return err
	}
	if len(m.Secrets) == 0 {
		a.Report.Line(MarkSkip, "no secrets declared in manifest.jsonc")
		return nil
	}

	// Which secrets this run is for, decided *before* the vault is touched.
	//
	// It used to be decided after, and the cost of that was the whole phase:
	// untick every secret on the selector and doti still pointed `bw` at the
	// deployment, still asked for the master password, and still synced - then
	// rendered nothing. A master-password prompt for a phase that was going to
	// do nothing is the worst version of a checkbox that does not work.
	wanted := make([]manifest.Secret, 0, len(m.Secrets))
	for _, secret := range m.Secrets {
		// Both filters, and the platform one matters as much as the selection:
		// a manifest whose secrets are all declared for other platforms left
		// `wanted` non-empty on a Mac, so this opened the vault, prompted for a
		// master password and then rendered nothing but "not for macos". Which
		// is exactly what the check below exists to prevent.
		if secret.WantsPlatform(a.Platform) && a.wants(KindSecret, secret.Name) {
			wanted = append(wanted, secret)
		}
	}
	if len(wanted) == 0 {
		a.Report.Line(MarkSkip, "no secrets selected")
		return nil
	}

	// BW_SESSION from the environment, never from a file: a session key on
	// disk is a vault with the lock left open.
	client := secrets.New(a.vault(), os.Getenv("BW_SESSION"))

	// Before the unlock check, because pointing the CLI at the wrong
	// deployment fails as "Invalid master password" - an error that sends you
	// looking at your password rather than at the region.
	if m.Vault != nil {
		changed, err := client.EnsureServer(ctx, m.Vault.Server, a.DryRun)
		if err != nil {
			return err
		}
		switch {
		case changed && a.DryRun:
			a.Report.Line(MarkChange, "would point bw at "+m.Vault.Server)
			// And then stop: reading the vault through the deployment it is
			// pointed at now would answer about the wrong account, and
			// pointing it at the right one is the change -n declined to make.
			return fmt.Errorf("bw is pointed at another deployment - re-run without -n to move it to %s",
				m.Vault.Server)
		case changed:
			a.Report.Line(MarkChange, "pointed bw at "+m.Vault.Server)
		case m.Vault.Server != "":
			a.Report.Line(MarkOK, "bw is pointed at "+m.Vault.Server)
		}
	}

	if err := a.openVault(ctx, client); err != nil {
		return err
	}
	// `bw` answers from a local cache, so without this a rotated credential
	// renders as the old value and nothing says so.
	done := a.Report.Working("bw sync")
	if err := client.Sync(ctx); err != nil {
		done(MarkWarn, "vault sync failed")
		return fmt.Errorf("syncing vault: %w", err)
	}
	done(MarkOK, "vault in sync")

	renderer := &secrets.Renderer{
		Client: client, RepoRoot: a.Repo, Home: a.Home,
		Platform: a.Platform, DryRun: a.DryRun,
	}
	results, err := renderer.RenderAll(ctx, wanted)
	// Report what did land before returning the failure - a partial run is
	// worth knowing about.
	for _, result := range results {
		switch {
		case result.Skipped:
			a.Report.Line(MarkSkip, fmt.Sprintf("%s (%s)", result.Name, result.Reason))
		case result.Changed && a.DryRun:
			a.Report.Line(MarkChange, fmt.Sprintf("would write %s -> %s", result.Name, result.Target))
		case result.Changed:
			a.Report.Line(MarkChange, fmt.Sprintf("%s -> %s", result.Name, result.Target))
		default:
			a.Report.Line(MarkOK, result.Name+" (unchanged)")
		}
	}
	return err
}

// vault is the `bw` runner for this run.
func (a *App) vault() secrets.Runner {
	if a.Vault != nil {
		return a.Vault
	}
	return secrets.ExecRunner{}
}

// openVault gets the vault into a readable state.
//
// Interactively when somebody is watching: bw owns the prompt, so the master
// password is typed into bw and never passes through this process - not
// through argv, not through memory. Non-interactively it stays an actionable
// error, because a script that stops to ask for a password is a script that
// hangs.
func (a *App) openVault(ctx context.Context, client *secrets.Client) error {
	status, err := client.Status(ctx)
	if err != nil {
		return err
	}
	if status.State == secrets.Unlocked {
		return nil
	}
	if !a.Interactive {
		return &secrets.UnavailableError{State: status.State}
	}

	if status.State == secrets.Unauthenticated {
		// Signing in is a change: `bw login` writes credentials into the CLI's
		// own data file, and those outlive the run.
		if a.DryRun {
			a.Report.Line(MarkSkip, "would sign in to "+status.ServerURL)
			return &secrets.UnavailableError{State: status.State}
		}
		a.Report.Line(MarkNone, "signing in to "+status.ServerURL)
		if err := client.Login(ctx); err != nil {
			return err
		}
	}

	// Unlocking is *not* a change, and the earlier rule that lumped it in with
	// signing in was wrong in a way that mattered: it left Preview unable to
	// answer the one question it exists to answer. The session is held in
	// memory and deliberately written nowhere, so unlocking is a read that
	// happens to need a person - and a dry run that refuses it can only ever
	// report "the vault is locked", which is not a preview of anything.
	//
	// Still gated on somebody watching, so a script is unaffected.
	if err := client.Unlock(ctx); err != nil {
		return err
	}
	a.Report.Line(MarkChange, "vault unlocked for this run")
	// Worth saying: the key doti holds is not in their shell, so the next
	// command would ask again. A child process cannot export into its parent.
	a.Report.Line(MarkSkip,
		"to reuse it: export BW_SESSION=$(bw unlock --raw)")
	return nil
}

// Scan is the read-only look at the machine that Check prints and Adopt acts
// on.
func (a *App) Scan() (health.Report, error) {
	m, err := a.Manifest()
	if err != nil {
		return health.Report{}, err
	}
	// The links whose targets live outside $HOME. They were installed and then
	// never verified, so `doti check` passed on a machine whose Windows
	// Terminal settings had been replaced by the installer's own.
	links := make([]health.Link, 0, 2)
	for _, link := range a.SystemLinks() {
		links = append(links, health.Link{
			Name: link.Name, Target: link.Target, Source: link.Source,
		})
	}

	return health.Check(health.Options{
		Manifest: m, Platform: a.Platform,
		Repo: a.Repo, Home: a.Home, Detect: a.Runner,
		Links: links,
	}), nil
}

// Check prints the report and changes nothing.
//
// --strict is what makes this usable from a login shell or a cron job: the
// default exit code is 0 even with drift, because "tell me" and "fail" are
// different questions and only the caller knows which one it is asking.
func (a *App) Check(strict bool) error {
	report, err := a.Scan()
	if err != nil {
		return err
	}
	passed, total := report.Counts()
	a.Report.Phase(fmt.Sprintf("%d of %d checks passed", passed, total))
	for _, finding := range report.Missing() {
		a.Report.Line(MarkWarn, fmt.Sprintf("%-6s %-32s %s",
			finding.Kind, finding.Name, finding.Detail))
	}
	if report.OK() {
		a.Report.Line(MarkOK, "nothing missing")
	}
	if strict && !report.OK() {
		return fmt.Errorf("%d check(s) failed", len(report.Missing()))
	}
	return nil
}

// Adopt is install for a machine already in use: scan, say what is already
// there, then act only on the gaps.
//
// The scan is the whole point. Someone running this has tools they installed
// by hand and configs they wrote years ago, and the question they want
// answered before anything is touched is "what are you about to do".
func (a *App) Adopt(ctx context.Context) error {
	if !a.Cloned() {
		// Nothing to scan against yet, so this is just an install.
		return a.Install(ctx)
	}
	report, err := a.Scan()
	if err != nil {
		return err
	}
	passed, total := report.Counts()
	a.Report.Phase(fmt.Sprintf("scan: %d of %d already in place", passed, total))
	for _, finding := range report.Missing() {
		a.Report.Line(MarkWarn, fmt.Sprintf("%-6s %-32s %s",
			finding.Kind, finding.Name, finding.Detail))
	}
	if report.OK() {
		a.Report.Summary("nothing to do")
		return nil
	}
	// `brew bundle` and the link planner are both already idempotent - they
	// skip what exists - so acting on the gaps is the same call as install.
	// What adopt adds is the report above, before anything happens.
	return a.Install(ctx)
}

// Sync brings the repo forward and re-links.
func (a *App) Sync(ctx context.Context) error {
	a.Report.Phase("sync")
	if a.DryRun {
		a.Report.Line(MarkChange, fmt.Sprintf("would run git -C %s pull --ff-only", a.Repo))
	} else {
		done := a.Report.Working("git pull --ff-only")
		// --ff-only rather than a merge: this runs unattended, and a sync
		// that stops to ask about a merge conflict is a sync that hangs.
		if err := a.Runner.Run(ctx, "git", "-C", a.Repo, "pull", "--ff-only"); err != nil {
			done(MarkWarn, "pull failed - resolve it by hand, then re-run")
			return err
		}
		done(MarkChange, "up to date")
	}
	a.Report.Phase("configs")
	return a.Link()
}

// Update upgrades what the package manager installed, and nothing else.
func (a *App) Update(ctx context.Context) error {
	a.Report.Phase("update")
	if a.DryRun {
		a.Report.Line(MarkChange, "would upgrade installed packages")
		return nil
	}
	// The MCP servers, on both platforms - they come from npm either way.
	//
	// This is where their upgrade lives now. An install used to perform one as a
	// side effect of reinstalling every declared package; it no longer does, and
	// an upgrade with nowhere to happen would be a regression rather than a
	// saving.
	a.updateMcps(ctx)
	// And the tools routed through bun, which neither `winget upgrade --all`
	// nor `brew upgrade` knows anything about.
	a.updateBunTools(ctx)

	if a.Platform == manifest.Windows {
		done := a.Report.Working("winget upgrade --all")
		if err := a.Runner.Run(ctx, "winget", "upgrade", "--all",
			"--accept-package-agreements", "--accept-source-agreements"); err != nil {
			done(MarkWarn, "winget upgrade failed")
			return err
		}
		done(MarkChange, "packages upgraded")
		return nil
	}

	if !a.Runner.Look("brew") {
		return fmt.Errorf("homebrew is not installed - see https://brew.sh")
	}
	done := a.Report.Working("brew update")
	if err := a.Runner.Run(ctx, "brew", "update"); err != nil {
		done(MarkWarn, "brew update failed")
		return err
	}
	done(MarkOK, "formulae refreshed")

	done = a.Report.Working("brew upgrade")
	if err := a.Runner.Run(ctx, "brew", "upgrade"); err != nil {
		done(MarkWarn, "brew upgrade failed")
		return err
	}
	done(MarkChange, "packages upgraded")
	return nil
}

// updateMcps upgrades the manifest's global npm packages.
//
// Named rather than `npm update -g` bare: that would upgrade every global
// package on the machine, and the ones this repository did not install are not
// this repository's to move.
//
// Not narrowed by Include, and deliberately: this operation is wholesale.
// `brew upgrade` and `winget upgrade --all` cannot be pointed at a subset of a
// manifest, so the menu gives Update no selector - and narrowing only the npm
// third of it would be an arbitrary half-answer to a question nobody asked. It
// did have that gate, which was unreachable and therefore untestable, which is
// its own reason to remove it.
//
// Best-effort, like the install: none of them is load-bearing for a working
// shell, and a failed npm should not stop `brew upgrade`.
func (a *App) updateMcps(ctx context.Context) {
	m, err := a.Manifest()
	if err != nil || len(m.Mcps) == 0 {
		return
	}
	if !a.Runner.Look("npm") {
		return
	}
	done := a.Report.Working(fmt.Sprintf("npm update -g (%d MCP servers)", len(m.Mcps)))
	args := append([]string{"update", "-g"}, m.Mcps...)
	if err := a.Runner.Run(ctx, "npm", args...); err != nil {
		done(MarkWarn, fmt.Sprintf("npm update failed: %v", err))
		return
	}
	done(MarkChange, fmt.Sprintf("%d MCP servers up to date", len(m.Mcps)))
}

// updateBunTools upgrades the tools the manifest routes through bun.
//
// `bun install -g <pkg>` rather than `bun update -g`: install re-resolves to the
// latest published version, and tracking latest is the entire reason a tool is
// routed here - winget's opencode lagged upstream, and an update that respected
// a recorded semver range would inherit the same lag by a different route.
//
// Named packages only. bun's globals are not all this repository's, for the same
// reason updateMcps does not run bare.
//
// Best-effort, like the MCP servers: a failed bun should not stop
// `winget upgrade --all`.
func (a *App) updateBunTools(ctx context.Context) {
	m, err := a.Manifest()
	if err != nil {
		return
	}
	packages := a.bunNames(m)
	if len(packages) == 0 || !a.Runner.Look("bun") {
		return
	}
	done := a.Report.Working(fmt.Sprintf("bun install -g (%d tool(s))", len(packages)))
	for _, pkg := range packages {
		if err := a.Runner.Run(ctx, "bun", "install", "-g", pkg); err != nil {
			done(MarkWarn, fmt.Sprintf("bun install -g %s failed: %v", pkg, err))
			return
		}
	}
	done(MarkChange, fmt.Sprintf("%d bun tool(s) up to date", len(packages)))
}

// SelfUpdate replaces this binary with the newest release.
//
// It re-runs the installer rather than reimplementing the download: that
// script already resolves the newest release, verifies the checksum and asks
// the download its own version before trusting it, and a second
// implementation of that in Go would be the one that goes stale.
//
// --no-install, because upgrading the tool is not a reason to re-run an
// install the caller did not ask for.
func (a *App) SelfUpdate(ctx context.Context, currentVersion string) error {
	a.Report.Phase("self-update")
	a.Report.Line(MarkNone, "installed: "+currentVersion)

	if a.Platform == manifest.Windows {
		if a.DryRun {
			a.Report.Line(MarkChange, "would re-run install.ps1 to fetch the newest release")
			return nil
		}
		shell := "pwsh"
		if !a.Runner.Look(shell) {
			shell = "powershell"
		}
		if !a.Runner.Look(shell) {
			return fmt.Errorf("neither pwsh nor powershell is available to run the installer")
		}
		done := a.Report.Working("fetching the newest release")
		// The same script the documented Windows install uses, so a
		// self-update takes exactly the same verified path as a first
		// install. -NoInstall: upgrading the tool is not a reason to re-run
		// an install nobody asked for.
		err := a.Runner.Run(ctx, shell, "-NoProfile", "-Command",
			fmt.Sprintf("& ([scriptblock]::Create((irm %s))) -NoInstall", installerPS1))
		if err != nil {
			done(MarkWarn, "self-update failed")
			return err
		}
		done(MarkChange, "binary replaced - run `doti version` to confirm")
		return nil
	}
	if !a.Runner.Look("curl") {
		return fmt.Errorf("curl is required to self-update")
	}
	if a.DryRun {
		a.Report.Line(MarkChange, "would re-run the installer to fetch the newest release")
		return nil
	}

	// Piped to sh rather than downloaded and executed in two steps, which is
	// what the documented install does - so self-update takes exactly the
	// same path as a first install, including the checksum verification.
	script := installerURL
	done := a.Report.Working("fetching the newest release")
	err := a.Runner.Run(ctx, "sh", "-c",
		fmt.Sprintf("curl -fsSL %s | bash -s -- --no-install", shellQuote(script)))
	if err != nil {
		done(MarkWarn, "self-update failed")
		return err
	}
	done(MarkChange, "binary replaced - run `doti version` to confirm")
	return nil
}

// The installers, built from the one repository constant rather than spelled
// out - these and the release API used to name it three times.
const (
	rawBase      = "https://raw.githubusercontent.com/" + RepoSlug + "/main/apps/doti/scripts/"
	installerURL = rawBase + "install.sh"
	installerPS1 = rawBase + "install.ps1"
)

// shellQuote makes a string safe to embed in a single-quoted shell word.
//
// The URL above is a constant, so this is belt and braces - but the argument
// is one flag away from being user-supplied, and a URL that closes the quote
// would be command injection into a shell this code spawns.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
