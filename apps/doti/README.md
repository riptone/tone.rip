# doti

The dotfiles installer, as one binary.

```console
$ curl -fsSL https://raw.githubusercontent.com/riptone/tone.rip/main/apps/doti/scripts/install.sh | bash
```

That is the whole first-machine flow. The configs live in
[riptone/dotfiles](https://github.com/riptone/dotfiles); this is the thing
that installs them.

## What runs, in order

```
curl … install.sh | bash
  │
  ├─ 1. work out the OS and architecture           (uname)
  ├─ 2. make sure git exists                       (needs a package manager,
  │                                                 sometimes sudo - which is
  │                                                 why this step is here and
  │                                                 not in the binary)
  ├─ 3. resolve the newest `doti/v*` release
  ├─ 4. download the binary, verify SHA256SUMS,
  │     then ask *the download* its own version
  ├─ 5. install it to ~/.local/bin
  └─ 6. exec `doti install`
          │
          ├─ clone ~/dotfiles if it is not there
          ├─ read manifest.jsonc
          ├─ generate a Brewfile / packages.json into a temp dir,
          │  then brew bundle install --no-upgrade / winget import
          │  (tools, casks and zsh plugins, each pickable)
          ├─ npm install -g the MCP servers   (each pickable)
          ├─ link every stow package into $HOME
          ├─ write ~/.gitconfig.local if absent
          └─ render secrets from Bitwarden   (allowed to fail)
```

Everything after step 5 is this binary's job, **including the clone** - so the
shell script never learns the repository layout and does not change when it
moves.

## Why it exists

The dotfiles repo used to carry its own installer: `scripts/install.sh`
(1,362 lines), `scripts/Install.ps1` (1,021), `scripts/Main.ps1` (194) and two
bootstraps. **2,815 lines implementing the same forty-odd operations twice**,
in two languages, held together by three "keep them in sync" rules in that
repo's `AGENTS.md`. One binary that cross-compiles removes the class of bug
rather than the instances.

It also drops two dependencies. **GNU Stow** goes, because `Main.ps1` already
reimplemented it - stow does not run on Windows - so there were two
implementations to maintain and now there is one. **jq** goes, because the
manifest is parsed in-process.

## Commands

```console
$ doti                  # the window
$ doti install          # clone if needed, then packages, configs, secrets
$ doti adopt            # scan first, then act only on the gaps
$ doti check --strict   # read-only; non-zero exit when something is missing
$ doti link --only zsh  # one component
$ doti unlink --restore # remove links, put the newest backups back
$ doti uninstall        # what it would remove; nothing, until you name them
$ doti uninstall --tools jq,fd   # remove exactly those (tools or MCP servers)
$ doti sync             # git pull --ff-only, then re-link
$ doti update           # upgrade installed packages
$ doti secrets          # render secret files from Bitwarden
$ doti upgrade          # replace this binary with the newest release
$ doti packages         # print the generated package lists
$ doti validate         # parse and check manifest.jsonc
```

**The window is what a command draws** when somebody is watching, because it is
strictly more informative than lines: the log scrolls, the spinner says a slow
step is not a hang, and the footer says how it ended. A pipe, a file and CI get
lines instead, decided from the streams rather than asked for - an alt screen in
a log is thousands of cursor movements and no output.

`--term` is the escape hatch, for output that has to land in the scrollback as
it happens. It replaced `--tui`, which had this the other way round for one
reason: the window could not own the vault's password prompt. It can now.

A command that draws a window **replays its run to stdout on the way out**, so
the log survives the alt screen being discarded - `doti install --term` and
`doti install` leave the same thing behind. Through the same `PlainReporter`
rather than a second renderer that agrees with it. Only for a command: quitting
a menu you were browsing does not fill the terminal.

`-n` is the dry run on everything that writes - including `bw`'s own state,
which it did not used to be: `bw config server` writes a data file that
outlives the run, so a dry run against a fresh `$HOME` left the CLI pointed at
a deployment it had only been asked about. `--repo DIR` overrides
`$DOTFILES_DIR` (default `~/dotfiles`); `--url` and `$DOTFILES_REPO_URL`
override where a clone comes from. `--verbose` streams subprocess output
instead of capturing it.

**`doti install`, `doti install --term` and the window's Install are one
thing**, not three that agree. All of them reach `App.Do`, which is the single
place an operation's name becomes a call; the only difference is which Reporter
they carry. There used to be two switches - one over the command name, one over
what the window had chosen - and they *did* agree, which is not the same thing:
only one of the two had anywhere to put the components the selector had ticked,
so ticking a box quietly did nothing.

## The parts worth knowing

**`internal/manifest`** parses JWCC with a real parser
([tailscale/hujson](https://github.com/tailscale/hujson)), and that dependency
is the point. The manifest contains

```jsonc
"fresh_machine": "curl -fsSL https://raw.githubusercontent.com/… | bash"
```

and a comment-stripper that does not track string state eats the rest of that
line - leaving a *silently truncated* value, not a parse error. `DisallowUnknownFields`
is on for the same reason: a typo'd key would otherwise be a no-op.

**`internal/stow`** links a package's tree into `$HOME`, preserving stow's
folding: one symlink for a whole subtree where nothing else needs to share it.
The subtlety is **unfolding**. On an empty `$HOME` the first package to want
`~/.config` links the whole directory; without unfolding, every package after
it sees a foreign symlink, backs it up and replaces it - so the last one wins
and the rest silently vanish. Verified against the real repo: `ghostty`,
`opencode` and `ripgrep` all disappeared behind `starship`. `Plan` also
refuses a relative package path, because `os.Symlink` stores the source
verbatim and a relative one resolves against `$HOME` - `--repo dotfiles`
produced `~/.zshrc -> dotfiles/zsh/.zshrc`, which points at nothing and
reports success.

Reading the *other* direction was wrong for a release: GNU Stow writes
**relative** links, so `os.Readlink` returns `../dotfiles/ghostty/.config/ghostty`
where the plan holds an absolute source. Compared literally, every link on a
machine that had ever run `stow` looked foreign - the first `doti install`
backed up and replaced all thirteen of them. Nothing was lost, but a
migration reporting thirty changes it did not need to make is one nobody can
read. Worse, that same text reached `os.Stat`, which resolves a relative path
against the *process's* working directory - so whether a shared `~/.config`
was unfolded or silently replaced depended on where doti was started from, and
it happened to work from `$HOME`, which is where anyone would try it by hand.
Destinations are now resolved against the link's own directory, the way the
kernel reads them. Checked against the tool rather than a fixture: `stow` lays
down all seven packages, then `doti install -n` reports zero changes.

**`internal/secrets`** renders credentials from Bitwarden through the `bw`
CLI, and points that CLI at the right deployment first. `bw` defaults to the
US cloud without saying so, so an EU account fails to log in with *"Invalid
master password"* - an error that sends you looking at your password rather
than at the region. The manifest's `vault.server` is run through
`bw config server` before any unlock is attempted, so a new machine gets it
right without anyone remembering. Switching deployment while signed in is
refused by `bw`, so that case says `run bw logout` instead of letting bw's own
error stand.

It will also **sign in and unlock for you** when a person is watching. `bw`
owns those prompts: doti inherits stdin and stderr and captures only stdout,
which works because `bw unlock --raw` writes "Master password:" to stderr and
the session key to stdout - checked by looking, not assumed. So the master
password is typed into `bw`, and is never in doti's argv, memory or errors. A
failed unlock deliberately drops the captured stdout from its error, because
on that path stdout *is* the session key.

**Inside the window it borrows the terminal back.** The alt screen has the
terminal `bw` needs for its own prompt, so the secrets phase used to defer there
with a line saying to run it from a shell - honest, and still a dead end. Now
the operation sends a request and blocks, the model answers it with `tea.Exec`
(which suspends the program and restores the terminal to the state `bw`
expects), and the reply comes back down the same channel.

So the master-password prompt is **not** drawn by the window: the log says
`handing the terminal to bw unlock`, the window steps aside, `bw` prompts on the
real terminal, and the window comes back with the answer. That is deliberate
rather than a limitation. Drawing the prompt inside the TUI would mean doti
reading the keystrokes, holding the password in its own memory and passing it to
`bw` somehow - which is exactly the thing the stdin/stderr split exists to avoid.
Nothing here should ever be in a position to leak a master password, and the
cheapest way to guarantee that is to never have it.

The detail that rules out `tea.ExecProcess`: it points all three streams at the
terminal, and `bw unlock --raw` writes its prompt to **stderr** and the session
key to **stdout**. A custom `tea.ExecCommand` hands over stdin and stderr and
keeps stdout captured, or the key is printed to the screen and lost. Every wait
is released by both the window closing and the run being cancelled, because this
blocks a goroutine that is holding up an install.

`App.Vault` is the seam that made it possible, and it made the whole secrets
phase reachable from a test at the same time - it used to build its own runner,
so asserting any of it meant a real vault and a real password.

Non-interactively - a pipe, CI, `doti install` from a script - it stays an
actionable error instead, because a script that stops to ask for a password
is a script that hangs.

**A dry run may unlock, and may not sign in.** That distinction had been missed:
`-n` refused both, on the grounds that neither should change anything - which
left Preview able to report only "the vault is locked", not a preview of
anything. Unlocking is a *read* that happens to need a person; the session is
held in memory and written nowhere. Signing in is a change, because `bw login`
writes credentials into the CLI's own data file, and so is `bw config server`.
So `-n` prompts once, reads, and reports `would write creds -> ~/.doti/…`
without writing it. Still gated on somebody watching, so a script is
unaffected.

That check reads **both** streams, and the first release shipped it reading
one. `stdout` being a terminal decides how to *render*; `stdin` being one
decides whether anything can be *asked*. Piped into bash - which is how this
is installed - stdout is still the terminal while stdin is the exhausted
download, so a one-stream test called an unattended install interactive and
`bw unlock` prompted into a closed pipe (`ERR_USE_AFTER_CLOSE`, from inside
node). `install.sh` now hands the binary `/dev/tty` when one can be *opened* -
attempted rather than tested with `-r`, because a session with no controlling
terminal has a `/dev/tty` that passes `-r` and still fails `open()` with
ENXIO. The check itself is `term.IsTerminal`, the same call Bubble Tea makes,
rather than `Mode()&os.ModeCharDevice`: `/dev/null` is a character device, so
the cheap version called `doti install </dev/null` a person too. A whole-file secret whose target ends in `.json` is parsed before it is
written, because those notes are pasted in by hand and a truncated copy still
renders - failing later, somewhere that looks unrelated. It also refuses to write a target that resolves *into the repository*. That
guard is not theoretical: the dotfiles repo declared
`~/.config/opencode/mssql-envs.json`, which reads like a path in `$HOME` — but
stow folds, `~/.config/opencode` was a symlink into the checkout, and
rendering "into $HOME" would have written 18 database passwords into the
working tree. The target is resolved, not compared as text, because a fold
means the string and the destination differ. The session key is held in memory and passed in `bw`'s environment -
never `argv` (visible in `ps`), never disk. `bw sync` runs first, because `bw`
answers from a local cache and skipping it renders a *rotated* credential as
the old value with nothing saying so. A missing field lists the item's field
*names* and never their values, and every fetched value is registered with a
scrubber so an error path cannot leak one.

**`internal/app/remove.go`** is the one operation here that deletes software,
and the rules are the feature:

- **It removes exactly what it was named.** `Include` is the list, and an empty
  one removes nothing — there is no spelling of this that means *all* unless
  somebody typed one. `doti uninstall` on its own prints what it would be
  willing to remove and hands you the `--tools` line that would do it.
- **It offers only what the package manager says it installed** — not what is on
  `PATH`. The two are different questions, and the difference is not
  theoretical: macOS ships `/usr/bin/jq`, so on a machine where `brew uninstall
  jq` had already run the selector went on offering jq as "installed", every
  session, across a reboot, because the list was built from `command -v` and
  `command -v` was telling the truth. `pkgs.Owned` asks `brew list --formula -1`
  plus `--cask -1` (≈40ms for a whole machine) and, on Windows, reads `winget
  export` — the same file `pkgs.WingetPackages` renders, rather than the
  localised table `winget list` prints. Naming such a tool explicitly now says
  which of the two facts is the reason: *"jq is installed, but not by brew —
  left alone"*, where the old answer was "jq (not installed)" about a binary you
  can run.
- **A tap-qualified name is one keg under two spellings.** A manifest may name a
  formula `user/tap/formula`, and for some tools it has to: opencode's own docs
  recommend `anomalyco/tap/opencode` because homebrew-core's copy lags — 1.18.20
  against 1.18.25 upstream when this was written. `brew install` and `brew
  uninstall` take that spelling; `brew list` never gives it back, printing the
  Cellar's short names instead. So the qualified name matched nothing in the
  owned set, and a tap-qualified tool was permanently invisible to the removal
  selector *and* permanently "missing" in the install one — the jq bug arrived at
  from the other side. `brew list --full-name` would print the qualified names
  and was measured at 221–292ms against 12–16ms for the short list on the same
  51 formulae, because it loads tap metadata where the short list reads
  directory names: fifteen times the cost of the thing it fixes, on the path
  that opens the menu. `pkgs.Formula` strips the manifest's side instead, for
  free, and the label stays the manifest's spelling — that is what `brew` gets
  handed.
- **It refuses anything the manifest does not list.** A tool this repository
  never installed is not this repository's to remove — and a typo is reported
  rather than silently removing nothing, which reads as "it was already gone".
- **It refuses the tools the manifest calls required.** `health.extra_tools` is
  exactly the set that gets a machine back to a working state, so `brew`, `git`,
  `stow` and `zsh` are not removable; a command that can remove its own package
  manager is a foot-gun with a name.
- **It does not pass `--ignore-dependencies`.** Homebrew refusing because
  something depends on a formula is the correct answer, reported rather than
  overridden — the whole point of asking a package manager instead of deleting
  files is that it knows what else would break.
- **The MCP servers come off too.** `npm install -g` was the one thing an
  install did that nothing could undo. They are offered individually, under
  their own heading, and only the ones npm actually has — established by asking
  `npm root -g` once and stat-ing each declared package, which is ~120ms where
  `npm ls -g` walks and resolves the whole global tree at ~700ms. That same
  answer is why the install selector now says *"2 of 7 present"* instead of the
  "7 declared" it used to settle for.

**A tool can name three package managers, and bun is the fallback.** A
`tools[]` entry carries `brew`, `winget` and `bun`; the platform's own comes
first and `bun` covers what it has no name for. The case that forced it: winget's
opencode sat on 1.18.21 while the tap, npm and the install script all shipped
1.18.25, so the manifest drops the winget id and names `opencode-ai` instead —
`bun install -g` tracks the registry, and opencode's own `opencode upgrade`
detects a bun install and follows the same route. A tool naming only `bun`
installs on all three platforms.

That is one field in the manifest and four behaviours here, because "which
package manager" turns out to be three separate questions with three separate
answers, and `a.Platform` could only answer the first:

- **Install.** Windows runs the `winget import` *first* and the bun installs
  after, because bun is itself a winget package and on a fresh machine arrives in
  that import. The manifest enforces the same order from the other side: a tool
  naming `bun` must be declared after the tool whose `cmd` is `bun`, or `Parse`
  refuses the file. The generated `packages.json` never sees a bun package —
  there is no identifier to import — and an import with nothing left in it is
  skipped rather than handed to winget empty.
- **Presence.** Only the *missing* ones get installed. `bun install -g` on a
  package that is already there re-resolves it to the latest published version,
  which is an upgrade, and `--no-upgrade` on the brew side exists precisely so an
  install does not move your versions.
- **Ownership.** `winget export` knows nothing about a bun global, so the
  removal selector asks bun's own directory: `$BUN_INSTALL/install/global/
  node_modules`, defaulting to `~/.bun`. No subprocess — bun named that path
  itself, in the error `bun pm bin -g` gives on a machine that has never
  installed one. Without this the tool would have read as "never installed" for
  as long as it existed, and `bun remove -g` would never have been offered.
- **Update.** `winget upgrade --all` does not reach a bun global either, so
  `doti update` runs `bun install -g` over the named packages — install rather
  than `bun update -g`, because tracking latest is the entire reason a tool is
  routed here and an update respecting a recorded semver range would inherit the
  same lag by a different door.

Naming such a tool explicitly says *which* manager: *"opencode is installed, but
not by bun — left alone"*. It said "not by winget" before, which was true of a
thing that was never going to install it.

**The lists are re-read after every run.** They used to be read once, before
the program started, and never again - so a removal that worked left the tools
it had just uninstalled still saying "installed", and an install left the
configs it had just linked still saying "not linked". Every selector was a
description of the machine as it had been when the window opened. Settling a run
now fires a background re-scan, and it replaces the *sources* only: `m.items` is
the working copy an open selector is toggling, and replacing that would throw
away ticks somebody had just made. A failed re-scan is silence, because the
lists it would have replaced were right a moment ago.

The re-scan also calls `App.Forget`, which drops the cached manifest: `sync`
pulls, and a pull can change `manifest.jsonc`. Each operation gets the same
treatment on its own copy, so no run inherits a stale read of the checkout.

In the window it is the one selector that **starts with nothing ticked**, so the
safe action — press enter without thinking — is the one that does nothing, and
the count in the footer turns red the moment it is not zero. The ticking *is*
the confirmation, which beats a yes/no prompt because it is per item.

**`internal/health`** also verifies the links whose targets are *outside*
`$HOME` - the Windows Terminal settings and the PowerShell profile. The
manifest's `health.links` cannot name those, because their targets are a
`%LOCALAPPDATA%` expansion that moves with the machine rather than a path
written down, so they were installed and then never checked: `doti check`
passed on a machine whose terminal settings had been replaced. `Scan` supplies
them as `health.Link`s, and they go through the same `checkLink` as everything
else - missing, a copy rather than a link, broken, or pointing somewhere else.

**`internal/health`** is the read-only half, and separate on purpose: `doti
check` has to be safe from a login shell, and the way to guarantee that is for
it to contain no writing code. It resolves links rather than reading them, so
a path reached through a folded parent passes; a real copy where a symlink
belongs is reported, because that is drift that looks fine.

Extraction is guarded twice, on purpose. Entries are flattened to a base
name, and then `safeJoin` checks the *resolved* path is inside the target
directory before anything is written. The second check is redundant today -
and kept, because CodeQL flagged the extraction as `go/zipslip` precisely
because the sanitisation was inferable from a helper rather than enforced
where the path is used. A scanner cannot see that invariant, and neither can
the next person to touch the loop.

**`internal/app`** is what the commands do, and none of it prints - it
reports. `cmd/doti` is flags, a dispatch table and the window's entry point -
no file over 300 lines, down from one of 773 - which is what makes the whole
surface reachable from a test: a fake Runner and a Recorder let `install` be
asserted without a package manager, a vault, a terminal or a `$HOME`.

The rendering is chosen once, from whether anything is watching. A terminal
gets colour and a spinner with an elapsed counter; a pipe, a file or CI gets
plain lines, because cursor movement in a log is noise. `NO_COLOR` is
honoured. Subprocess output is **captured** rather than streamed, for two
reasons: twenty lines of brew pour progress under every step buries the six
that say what doti did, and captured output goes into the error, where
streamed output has already scrolled past by the time one surfaces.

**`internal/tui`** is Bubble Tea. It shares its whole frame with
`apps/ssh-cv` through `packages/gotui` - the palette, the card, the geometry,
the scrollbar, the footer's drop order, the colour-profile policy and the
keymap - and keeps only the styles for its own content. Its own files are one
screen each: `screen_menu.go`, `screen_select.go`, `screen_run.go`.

**Operations run inside the window.** Choosing Install used to quit the
program, hand an Action back to `main`, and let `main` print onto the terminal
the window had just given up - so picking something from a TUI meant watching
the TUI disappear. Now the same `app.Install` runs on a goroutine reporting
into an `app.StreamReporter`, whose channel the run screen reads as Bubble Tea
messages: the log scrolls, the spinner sits on whatever is slow, `ctrl+c`
cancels the context, and the footer says `done` or `failed` when it is over.
`enter` goes back to the menu. The verdict is coloured — green for `done`, red
for `failed` — because two words in the same grey have to be told apart by
spelling.

**The run screen and the help get a bigger card** than the menu does. A menu is
seven short rows and looks abandoned in a wide frame; a run's log is the
opposite — `git pull` explains a missing upstream in a paragraph, and 58 columns
turn that into a column of fragments. The wide spec is derived from the narrow
one rather than written out, so the padding and the gutter cannot drift from the
ones the card was built with, and it is still a card: margin goes first and it
never exceeds the terminal.

**The cursor wraps, and esc knows where it is.** Up from the first entry is the
last one and down from the last is the first, which is the arithmetic
`apps/ssh-cv` uses - a list you can only leave by pressing the other arrow eight
times is a list that ignores you. `g`/`G` and home/end clamp instead, because
those mean "the first" and "the last". `esc` is bound to both Back and Quit and
the dispatch decides: it closes a screen from inside one and closes the program
from the menu, which is again what the CV does.

**Both lists scroll with the cursor.** They used to hand every row to the frame,
which draws as many as the body has and drops the rest - so on a 24-row terminal
a 30-component selector showed twelve, `G` moved the cursor onto an item nobody
could see, and space then toggled it. The window is scrolled by the smallest
amount that keeps the cursor visible, and the scrollbar is drawn from the same
offset so the two cannot disagree about where in the list you are.

**Every package is on the list, one row at a time.** The Packages group used to
be one row per manifest *list* — "brew packages, 11 of 16 present" — so the only
answers available were all and nothing. Each list is a **parent** now with its
members folded underneath: sixteen tools, five casks, two zsh plugins, seven MCP
servers, each with its own checkbox. On Windows the casks and plugins are
replaced by the `winget_extras`, which are the same idea under another package
manager.

**Folded by default**, which is what makes that cost nothing: the group reads
exactly as it did when each list was one line, until somebody presses `→`. `←`
closes it again — from a child too, which closes the thing it is inside and takes
the cursor up with it, because leaving the cursor on a row that is no longer
drawn is the bug the list scrolling already had once. `tab` does whichever
applies, for the reader who has not worked out which arrow.

```
  Packages                                          Packages
› [x] ▸ brew packages       14 of 15 present      › [x] ▾ brew packages       14 of 15 present
  [x] ▸ brew casks                5 declared            [x] jq                     installed
  [x] ▸ zsh plugins               2 declared            [x] git                    installed
  [x] ▸ mcp servers           6 of 7 present            [~] …
```

A parent's box is the state of its children: `[x]` all, `[ ]` none, `[~]` some —
and space on a partly ticked one fills it, because that is what the hand means.
A child ticks its parent on the way up, since a parent left unticked with a
ticked child under it would report a skipped phase and then install something.
Folding is display only: a hidden child is hidden, not unticked, or closing a
group would be a way to silently change the outcome. The footer counts leaves,
not rows — a parent is a summary of its children, and counting it too would make
"3 of 2" the reading for a fully ticked pair.

**What forced the design honest** was a trap on the way: there used to be two
Brewfile renderers, the whole manifest and a tools-only one for `--tools`.
Rendering the tools-only file because *one tool* was unticked would have declined
every GUI app and both zsh plugins as a side effect — decline `bat`, lose
Ghostty. So there is one renderer, `pkgs.BrewfileOf`, taking the three lists
explicitly, and each list is narrowed by its own caller. `--tools` keeps its
older contract by passing the other two empty, and a selection supersedes it.

**The labels had to become qualified for any of this to be safe.** `Include` is
`[]app.Ref` now — a kind and a label — because the real manifest lists `git`,
`stow` and `starship` as tools it installs *and* as stow packages it links. A
flat list of names could only say "git", and both would read that as theirs; the
collision was unreachable while the whole tool set was one row, and offering the
tools individually is what made it real. An empty `Kind` matches any kind, which
is what `--tools jq` means. Naming a parent and nothing under it means all of it,
so a caller that does not know about the children still installs the group rather
than nothing.

Rendering a real machine's list costs 312µs folded and 485µs open on a 96×40
card — 3% of a frame — so the `O(n)` parent lookups called once per row stay as
they are. Measured, not assumed: `BenchmarkRenderSelector`.

**An install no longer reinstalls the MCP servers it already has.** `npm install
-g` on a package that is present takes about two seconds to decide it has nothing
to do, so an install on a set-up machine spent fifteen of them on seven packages
it had — the same `npm root -g` that fixed the counts answers this too. The
upgrade that reinstall performed as a side effect moved to `doti update`, which
is the split `brew bundle --no-upgrade` already draws: install and update are
different operations and the menu offers both. `npm update -g` there names the
declared packages rather than running bare, because the globals this repository
did not install are not its to move.

**Preview picks too**, which it did not, and that cost more than the
inconsistency: previewing what *packages* would change stopped to ask for the
vault master password, because the secrets phase runs either way and there was no
box to untick. Two halves to that fix — the selector, and the phase itself:
`App.Secrets` chose which secrets to render *after* pointing `bw` at the
deployment, asking for the password and syncing, so unticking every secret still
prompted and then wrote nothing. It decides first now. A master-password prompt
for a phase that was going to do nothing is the worst version of a checkbox that
does not work.

**Adopt shows what is left, and nothing else.** Its selector was byte-for-byte
the Install one — every tool, every cask, every linked config, on a machine that
had them all — which made "install only what is missing" a description of an
operation rather than of a screen. It now drops what the machine already has,
along with any group left with nothing under it, and **opens** the groups it
keeps, because a list built from the gaps is short by construction and folding a
short list hides the answer. A group's own count is a summary; the children are
the truth, so a group stays exactly when something under it is still missing.

```
Install                                    Adopt (same machine)
› [x] ▸ brew packages   15 of 16 present  › [x] ▾ brew packages   15 of 16 present
  [x] ▸ brew casks        5 of 6 present        [x] htop                   missing
  [x] ▸ zsh plugins       2 of 2 present    [x] ▾ brew casks       5 of 6 present
  [x] ▸ mcp servers       7 of 7 present        [x] not-a-real-cask        missing
  [x]   zsh                  not linked    [x]   zsh                  not linked
  …                       40 of 40         …                       11 of 11
```

**Which forced the casks and plugins to get a real answer.** They used to read
"5 declared" with no present/missing state — nobody had asked brew — and a row
with no state can only ever be *shown*, so every GUI app and both plugins turned
up on a list of what was missing. `App.Owned` is the same inventory the removal
reads, and it is exactly the predicate that decides whether the install step is a
no-op: `brew bundle` skips what brew already owns. PATH would have been wrong in
both directions here — a cask puts nothing on it, and a font is not a command.

**Both inventories are asked once per invocation** and dropped by `App.Forget`,
like the manifest and the ignore rules. One screen asks from two places — 
`MenuItems` for the install list, `Removable` for the removal one — and on
Windows the question is `winget export`, which takes seconds rather than
`brew list`'s ~40ms. Caching both halved a real scan of this machine, 165ms to
93ms (`BenchmarkLiveScan`). The one caller that deliberately does *not* use the
cache is `installMcps`: it runs after the packages phase, which may just have
installed npm itself.

**No locally built binary could ever be offered an update.** `bun run
install-local` stamps `<tag>+dev.<sha>`, and `parseVersion` split that on the dot
into *four* parts and refused — so `Newer` returned false whatever was released.
Semver says build metadata takes no part in precedence, and it is dropped now. A
pre-release suffix is still refused rather than guessed at, and a binary with no
version at all still declines every offer: that guard is for somebody's `go
build`, and replacing it is not this tool's call.

**`--tools` reached one of the two paths.** `doti uninstall --tools bat` in a
terminal — the default, which is the window — opened straight onto the run screen
with nothing selected, reported *"name what to remove; nothing was removed"* and
then advised re-running with `--tools`. The same command with `--term` removed it.
`Config.StartChosen` existed, was read, and was never set by any caller; the
window now carries the flags the launched operation was given, which is the whole
point of the one-table-not-two-switches arrangement above.

**Nothing ticked means nothing**, and an empty *list* says so in its own words
rather than suggesting a key that cannot help. It used to mean *everything* on
every screen but the removal, and by accident: an empty selection reaches `internal/app` as an
empty `Include`, and an empty `Include` is how the command line spells "no
narrowing". So unticking every box on an Install and pressing enter installed the
lot — the safest-looking keypress on the screen was the most destructive one
available. Now it refuses and says why, in the lead line, because a key that does
nothing and explains nothing reads as a hang.

**Unlink picks, like Install and Remove.** It used to act on every stow package
there was, which made "take ghostty back off this machine" something you could
only do by unlinking all of them and re-installing the rest. Its selector offers
the *stow packages alone*: `app.Component` carries a `Kind`, an operation
declares which kinds its selector lists, and a list that also offered `brew
packages`, the MCP servers, `gitconfig-local` and the secrets would be three
quarters checkboxes that change nothing about what an unlink does. It starts with
everything ticked, unlike the removal, because an unlink puts back what was there
before.

**`gitconfig-local` finally does something.** It was the last thing an install
wrote that the selector could not turn off — and the checkbox was *already
there*: `system_components` declares `gitconfig-local` on all three platforms,
so the selector drew a box for it and nothing consulted the box. It had never
been a system *link* either; `SystemLinks()` returns nothing by that name on any
platform, because `writeGitLocal` writes the file rather than linking it. So the
manifest's declaration is now the one row about that one file, carrying
`writeGitLocal`'s own state instead of the "system link" the other two get — and
the row is offered whether or not a manifest declares it, because an install
writes it either way and a checkbox that exists only for some manifests is a step
that silently stops happening for the rest. It shows a third state as well as the
two obvious ones, because it is the one where ticking the box does nothing and
the reader deserves to know before they tick it: *rendered from a secret*, when
the manifest names a secret that owns the same path.

**`h` (or `?`) opens the help**, from anywhere nothing is running — it would
take the log off screen mid-install, and the log is why the window exists — and
`esc` returns to whatever asked for it. Its text is built from the same menu
table and keymap the program acts on, so a help screen cannot describe a key
that no longer exists.

Nothing about the operations changed to make that work, which is what the
Reporter seam was for. `internal/app` imports no UI at all - it used to return
the window's own item type, which had the domain depending on the thing that
draws it and made a Reporter that sends Bubble Tea messages impossible to write
without an import cycle.

**Three outcomes, not two.** `ctrl+c` used to give whichever of "done" or
"failed" the timing happened to produce: a phase blocked in `brew bundle` returns
the context error and the footer went red, while one between subprocesses — or
one taking no context at all, like `Link` — returns nil and it went **green**.
Neither is what happened. There is an `interrupted` now, in the amber of the
middle window button, because the traffic lights already had the right three
colours and the middle one is the middle case. It outranks the error, since a
cancelled `brew bundle` failing is a consequence of the reader's decision rather
than a fault of the machine, and the log's last line says
`stopped here: nothing after this ran`. The shell hears about it too: a window
that shows "interrupted" and exits 0 tells the same lie as one that shows a red
line and exits 0.

**And the selectors follow a partial run.** Settling fires the re-scan whether
the run finished, failed or was stopped, so ticking everything and letting one
thing through leaves the lists describing what actually happened — verified live:
after an install narrowed to one stow package, Install reads `zsh  linked` with
the other six `not linked`, and Adopt lists exactly the eight things left. Which
is the payoff of Adopt showing only the gaps: a stopped install becomes a to-do
list.

Three details that are not obvious:

- **The screen settles only when the work *and* its output are both finished.**
  They arrive as two messages from two command goroutines with no ordering
  between them, and settling on the operation's return alone dropped the
  closing lines of every run.
- **The tail stops chasing you.** Scrolling up turns following off, and
  returning to the bottom turns it back on. Yanking a reader back to the bottom
  is the worst thing a log view can do.
- **Rows are wrapped once, not once per line.** Folding one reported line used
  to re-wrap the whole log, which is quadratic in a run's length and threw away
  an identical answer every time. At 400 lines, folding one line costs 112µs
  where a full re-wrap costs 2.0ms; a resize is the one thing that still does
  the expensive version, because it is the one thing that invalidates it.

**The window checks for a newer release** when it opens, in the background, on
a three-second deadline, and offers it in the footer - `u update to v0.2.0` -
only if there is one. A failure is silence: nothing downstream depends on the
answer, and a menu that shouts about DNS is worse than one that never mentions
updates. A finished self-update offers `r restart`, which `execve`s the
replacement in place rather than spawning a second process behind this one.

## Tasks, and why there is no Makefile

```console
$ bun run test           # go test ./...
$ bun run check-types    # gofmt -l + go vet
$ bun run fmt            # gofmt -w + go mod tidy
$ bun run run            # the window, from source
$ bun run install-local  # build onto $PATH, stamped as a dev build
$ bun run release        # cross-compile all six, with SHA256SUMS
```

A Makefile would be a second entry point for tasks that already have one, and
`bun run ci` at the repository root is *the* gate - the one CI runs. Two runners
for one set of tasks means the moment they disagree, one of them is lying about
whether the build passes. Turborepo also caches and orders across packages, which
Make would have to reimplement; and `make` is not on Windows by default, which
for a cross-platform tool is an odd dependency to add to its own development.

The principle a Makefile is usually reached for here - *don't use it to hide
platform differences the program can eliminate* - is the one this app already
applied: 2,815 lines of `.sh` and `.ps1` became one Go binary, and what is left
is 192 lines of `install.sh` and 190 of `install.ps1` whose entire job is
download, verify, exec. There is no parallel implementation left for a Makefile
to stand in front of.

`bun run release` and the release workflow call the same
`scripts/build-release.sh`, so the six targets and the build flags are one list
rather than two, a tag produces what a local run produces, and shellcheck lints
it alongside the installers.

## Checks

```console
$ go test ./...          # or: bun run test
$ bun run check-types    # gofmt -l + go vet
$ go run ./cmd/doti      # look at it
$ go run ./cmd/doti preview --frames /tmp/f   # dump the screens as ANSI
```

The tests that matter: the manifest fixture carries the `//`-in-a-string case;
`internal/stow` has four regression tests for the folding collision above;
`internal/pkgs` holds the generated Brewfile to what the shell installer
produced, byte for byte, so switching installers cannot change what lands on a
machine.

## Releasing

```console
$ git tag doti/v1.0.0 && git push origin doti/v1.0.0
```

`.github/workflows/release-doti.yml` builds six binaries (darwin, linux and
windows × amd64 and arm64), stamps the version with `-ldflags -X`, publishes
them with a `SHA256SUMS`, and fails if the stamp did not land - because
`install.sh` asks the downloaded binary its own version before trusting it.

Tags are namespaced for the reason `ssh-cv/v*` is: in a monorepo, "the newest
doti release" has to be a different question from "the newest release".

## The two bootstrap scripts

`scripts/install.sh` and `scripts/install.ps1` are the one place this project
has two implementations of anything, and the reason is unavoidable: they are
what fetches the binary, so they cannot *be* the binary.

They are allowed to be two files only because **they contain no installer
logic** - no manifest, no package names, no paths inside the dotfiles
repository. Each does the same six steps: platform, git, resolve the release,
download and verify, install onto PATH, exec `doti install`. If a change to
one would need the same change in the other, it belongs in Go instead. That
rule is written at the top of both.

Both are linted: `shellcheck --enable=all` for the shell one, and because
`install.ps1` cannot be linted on Linux, a `windows-latest` CI job parses it
and runs PSScriptAnalyzer. Both also take a `--base-url` / `-BaseUrl`, which
points at a mirror - or at a local server for the test that proves the
checksum step *refuses* a tampered binary rather than merely appearing to
check it. Verified both ways in both scripts.

## Not done yet

- **The Windows font install stops one step short.** The faces are extracted
  into the per-user font directory, but Windows only lists *registered* fonts
  — so it reports what is left to do rather than doing it.
