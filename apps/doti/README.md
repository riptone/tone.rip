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
$ doti                  # interactive menu
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

`-n` is the dry run on everything that writes. `--repo DIR` overrides
`$DOTFILES_DIR` (default `~/dotfiles`); `--url` and `$DOTFILES_REPO_URL`
override where a clone comes from. `--verbose` streams subprocess output
instead of capturing it.

**`doti install` and the menu's Install are the same thing**, not two things
that agree. Both call `app.Install` with the same Reporter, so there is no
second path to keep in step - and `TestTheMenuPathAndTheCommandPathReportIdentically`
compares the two event streams to keep it that way.

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

Non-interactively - a pipe, CI, `doti install` from a script - it stays an
actionable error instead, because a script that stops to ask for a password
is a script that hangs. Same TTY check that picks the renderer. A whole-file secret whose target ends in `.json` is parsed before it is
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
reports. `cmd/doti` is flags and dispatch only (287 lines, down from 773),
which is what makes the whole surface reachable from a test: a fake Runner
and a Recorder let `install` be asserted without a package manager, a vault,
a terminal or a `$HOME`.

The rendering is chosen once, from whether anything is watching. A terminal
gets colour and a spinner with an elapsed counter; a pipe, a file or CI gets
plain lines, because cursor movement in a log is noise. `NO_COLOR` is
honoured. Subprocess output is **captured** rather than streamed, for two
reasons: twenty lines of brew pour progress under every step buries the six
that say what doti did, and captured output goes into the error, where
streamed output has already scrolled past by the time one surfaces.

**`internal/tui`** is Bubble Tea, sharing its window with `apps/ssh-cv`
through `packages/gotui`.

## Checks

```console
$ go test ./...          # or: bun run test
$ bun run check-types    # gofmt -l + go vet
$ go run ./cmd/doti menu # look at it
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

- **`doti check` does not verify the system links.** The Windows Terminal and
  PowerShell profile links are installed but not in `health.links`, so drift
  in them is invisible to `check`.
- **The Windows font install stops one step short.** The faces are extracted
  into the per-user font directory, but Windows only lists *registered* fonts
  — so it reports what is left to do rather than doing it.
- **No `doti uninstall` for packages.** Removing links is one command;
  removing the tools they configure is still `brew uninstall` by hand, on
  purpose — deleting somebody's `node` because a manifest changed would be a
  bad surprise.
