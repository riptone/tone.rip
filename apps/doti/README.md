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
          ├─ npm install -g the MCP servers
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
`enter` goes back to the menu.

Nothing about the operations changed to make that work, which is what the
Reporter seam was for. `internal/app` imports no UI at all - it used to return
the window's own item type, which had the domain depending on the thing that
draws it and made a Reporter that sends Bubble Tea messages impossible to write
without an import cycle.

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
- **No `doti uninstall` for packages.** Removing links is one command;
  removing the tools they configure is still `brew uninstall` by hand, on
  purpose — deleting somebody's `node` because a manifest changed would be a
  bad surprise.
