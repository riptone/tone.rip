# ssh-cv

The CV, served over SSH.

```console
$ ssh cv.tone.rip
```

Anyone can connect. What arrives is the *long* version of the CV the website
prints: the same content module, plus the company names and the per-role
detail that [/cv](https://tone.rip/cv) deliberately leaves out. Typing that
command is the only credential it asks for.

## What it looks like

An index and a page, inside a small window.

```
 ╭──────────────────────────────────────────────────────────────╮
 │  ● ● ●                                    tone — cv · en  │
 │  ──────────────────────────────────────────────────────────  │
 │                                                              │
 │  Experience                                                  │
 │                                                              │
 │  › Application Engineer ·                    feb 2026 - now  │
 │    Software Engineer · public-sector AI…  sep 2025 - jan 26  │
 │    Software Engineering Intern · cloud…   aug 2024 - jul 25  │
 │                                                              │
 │  More                                                        │
 │                                                              │
 │    Best at                                                   │
 │    Education                                                 │
 │    Certifications                                            │
 │    Skills                                                    │
 │    Personal                                                  │
 │    Contact                                                   │
 │                                                              │
 │  ↑/↓ move · enter open · q quit · l lang            laptop    │
 ╰──────────────────────────────────────────────────────────────╯
```

Three decisions are worth explaining, because the version before this one made
the opposite choice each time.

**An index, not tabs.** The previous UI had three tabs, each holding every
section of its category stacked end to end, so reading about one role meant
scrolling past two others and a wall of skills. A CV is a document with parts,
and the fastest way to read a part is to open it.

`enter` or `→` opens, `esc` closes, and `→` / `←` step forward and back through
the pages, so the whole CV reads straight through without returning to the list
between sections. `↑` / `↓` move the cursor in the index and scroll inside a
page. `l` switches language, `q` leaves.

**A window, not a full screen.** The card is at most 78 columns wide and only
as tall as the page it is holding, centred in whatever terminal it was given.
Text that runs to the edge of a 200-column terminal is as unreadable as a
website with no `max-width`, and the site this belongs to spends real care on
exactly that measure. The frame never comes off: below 40x14 the card gives up
its margins and takes the whole terminal, but it stays a card. An earlier
version dropped the border and let the document fill the screen, which stopped
looking like the same application at exactly the point it was hardest to
read.

**Black, white, and almost nothing else.** True black behind everything, white
for the content, two greys for the things that are labels rather than text -
plus the three window buttons, which are a quotation, and the cursor, which has
to be found instantly. The `styles` struct in `internal/tui/theme.go` is the
whole vocabulary and nothing builds a style inline, which is what keeps nine
pages looking like one document - and what keeps the black unbroken, see
below.

**A scrollbar in two parts.** A thin line down the right of the body says the
document has a length; a thicker section on it says where in that length you
are. Neither appears when the page fits, because a scrollbar that is always
there says nothing.

The chrome is bilingual with the content, so `l` switches the section names and
the key hints along with the CV. `ssh pt@cv.tone.rip` opens in Portuguese.

## Colour, and the four ways to lose it

The CV should look the same to everybody. Getting there over SSH took four
fixes, each for a failure that looks like a design choice rather than a bug.

**The renderer has to belong to the session.** lipgloss's default renderer is
bound to *this process's* stdout, which under systemd is a pipe, not a
terminal - so its colour profile resolves to `Ascii` and every colour is
silently stripped, for every session, on the deployed box only. The symptom was
a window with three grey buttons. `sessionRenderer` in `main.go` builds one per
session from the client's PTY instead; `Config.Renderer` carries it in, and
`newStyles` builds the vocabulary against it.

**Nothing may resolve against the reader's terminal.** The palette was
`lipgloss.AdaptiveColor` pairs, which pick a value from the *detected*
background - so the same CV came out in different colours for different people,
and in no colours at all when the detection failed. It is fixed hex now.

**Every cell is painted, and the black is `#000000`.** Not "near black": a
`#0f0f12` that reads as black next to a white page reads as grey next to a
terminal that is actually black. `styles.centre` paints the space around the
card by hand rather than with `lipgloss.Place`, which accepts a whitespace
background and then renders it through the *default* renderer - the same trap
as above, and one where the surround came out unpainted for exactly the
sessions that needed it painted.

This is a deliberate trade: painting a background is precisely what a
translucent terminal cannot show through, because it blends what is behind the
window with its *default* background and an explicit `48;…` is an opaque
rectangle over the top of that. Black on every terminal was worth more here
than transparency on some. What painting cannot reach at all is the emulator's
*own* chrome - its tab bar, its status line - so the session also asks the
client to make its default colours black and white for the duration (OSC 11 and
10, put back with 111 and 110 on the way out, in `main.go`'s
`terminalColours`). A terminal that ignores that request is still black inside
the session.

The consequence when editing the UI: **a raw space is a hole in the black.** An
inner style's reset ends the background an outer one started, so
`strings.Repeat(" ", n)` shows the reader's terminal through the window for the
rest of that line. Every gap goes through `styles.pad`, and
`TestTheBlackHasNoHoles` walks each rendered row's escape sequences to prove
it.

**The hex has to survive quantisation.** ssh forwards `TERM` and little else,
so most sessions are 256 colours rather than truecolor, and termenv's quantiser
is coarse: every grey below `#303030` collapses onto one index, and a dark grey
with any tint in it can land on a *cube* colour instead. The border was
`#2a2b31`; it arrived as index 17, which is navy. Both failure modes have a
test - `TestThePaletteSurvivesTwoFiftySixColours` and `TestTheGreysArriveGrey` -
because both look deliberate on the machine where the colour was chosen.

## Small terminals

The frame costs seven rows before a word of CV is on screen, which is most of a
14-row window - so under ten rows of body the two blank spacers inside the card
come out, and the content gets them instead. Below 40x14 the card also gives up
its margins, one side at a time, until it is the whole terminal - but it keeps
its border, its title bar and its footer, because those are what make it the
same application at any size.

The footer is the other thing that has to give. Everything on that row - each
key hint and the line counter - carries a rank, and what will not fit is
dropped by rank rather than by position: a 60-column terminal keeps the counter
and loses `l lang`, a 46-column one keeps `↑/↓ scroll · esc back · q quit` and
loses the counter. What it never does is show the arithmetic and no way out.
Between the scrollbar, the counter and the `↑/↓` hint, a short terminal still
says there is more to read - which is the only thing that matters, because
everything is reachable by scrolling.

One bug worth knowing about, because it is easy to reintroduce: a viewport
keeps its scroll offset across a resize, so a page scrolled to the bottom of a
short window renders from that offset into a tall one - the text ends halfway
up the card with a screenful of nothing under it, until some keypress happens
to clamp it. `refresh` re-clamps the offset on every resize, and
`TestGrowingTheTerminalDoesNotLeaveAGap` holds that line.

One thing that is *not* a bug: `bun run preview` comes out grey if `CI` is set
in your shell, because termenv treats a `CI` variable as "nobody is watching,
do not emit colour". That is the convention working; unset it to look at the
real thing.

One consequence worth knowing when editing the UI: **a raw space inside the
window is a hole in it.** An inner style's reset ends the background the card
started, so `strings.Repeat(" ", n)` shows the reader's terminal through the
surface for the rest of that line. Every gap goes through `styles.pad`, and
`TestTheWindowHasNoHoles` walks each rendered row's escape sequences to prove
it.

## Why this cannot run on Cloudflare Workers

It is the obvious question given the rest of this monorepo, and the answer is
no - not with [`syumai/workers`](https://github.com/syumai/workers), not with
anything.

That project compiles Go to WASM and runs it as a Worker serving **HTTP**. The
problem here is not the language, it is the protocol. A Worker is invoked with
a request; it cannot bind a listening socket, so it cannot accept a TCP
connection on port 22 and cannot perform the server half of an SSH handshake.
Workers do have `connect()`, but that is outbound only.

So this app needs a host with a real IP and port 22: a small VPS, a Fly.io
machine with a raw TCP service, or the tailnet box. **The key allowlist stays
on Workers** as part of `apps/api` - which is the useful split, because it is
the part that changes often.

## The other constraint: SSH has no SNI

TLS sends the hostname, so one IP can serve many domains. SSH does not. The
server never learns whether you typed `cv.tone.rip` or anything else
pointing at the same address; if two names resolve there, both are the same
connection.

That is why the CV is not split across ports (`ssh -p 2222 …`, which nobody
wants to type) and why nothing here is decided by hostname. Every name reaches
one server and every session gets the same CV.

## Layout

```
main.go                       wish server, flags, session wiring
internal/cv/                  the CV, generated from packages/content and embedded
internal/authz/               fingerprint → label and scopes, via apps/api
internal/tui/theme.go         the palette, every style in it, and why the hex is what it is
internal/tui/window.go        geometry, the card, the scrollbar, the footer
internal/tui/doc.go           the typographic vocabulary each page composes
internal/tui/index.go         the section list
internal/tui/sections.go      one page per section
internal/tui/labels.go        section names and key hints, per language
internal/tui/model.go         the bubbletea model
scripts/generate-content.ts   packages/content → internal/cv/cv.json
```

## Content

`internal/cv/cv.json` is generated from `packages/content/src/cv.ts` - the same
module the website renders - so the two cannot drift. It is committed, so
`go build` works in a checkout with no Bun.

```console
$ bun run generate     # regenerate after editing packages/content/src/cv.ts
```

Two fields exist for this surface only, and the content module says so:

- **`company`** - the organisation's name. The website describes an
  organisation by what it does and stops there, which is an editorial choice
  rather than an oversight (see `docs/architecture.md`). This one names it. A
  role whose name has not been filled in reads as its description instead, so
  the missing ones are missing rather than blank.
- **`detail`** - a few sentences on what the work actually involved, under
  *In depth* on the role's page.

Everything else - roles, dates, places, the per-role stack, the bullets,
education, certifications, skills - is rendered by both surfaces from the same
source.

## Keys and labels

Nothing here is gated. The CV is public, so a session with no key gets all of
it; what a recognised key buys is its label in the footer, which is how you
tell which of your own machines you are on. The scopes are still parsed and
resolved, because the mechanism is worth keeping for whatever needs gating
first.

The allowlist lives in a Worker secret, not in this binary, so a key is
recognised or forgotten by editing one value - with no rebuild and no shell on
the SSH host.

`SSH_AUTHORIZED_KEYS` on `apps/api`, one key per line:

```
SHA256:AbCd…  laptop  notes
SHA256:EfGh…  phone
# comments and blank lines are ignored
```

The first field is the fingerprint (`ssh-keygen -lf ~/.ssh/id_ed25519.pub`),
the second a label shown in the UI, and the rest are scopes.

`SSH_GATEWAY_TOKEN` is a shared secret proving to `apps/api` that a request
came from this server. Without it `/ssh/authorize` refuses everything - an open
endpoint would be an oracle for probing which fingerprints are known.

Every failure - API down, 500, malformed response - resolves to *no label and
no scopes*. An outage costs a word in the footer, never the CV.

## The wish SCP advisory

`charmbracelet/wish` ships an SCP middleware with an unfixed path traversal.
`fileSystemHandler.prefixed()` cleans the client's path, notices it does not
already start with the configured root, and joins it to the root anyway - so
`../../../etc/passwd` cleans to itself, joins to `/etc/passwd`, and is served.
The same call sits behind the write and mkdir paths, and the filenames come off
the SCP wire through a regex that accepts any string, so it reads *and* writes.

**There is nothing to upgrade to.** The advisory covers everything through
v1.4.7, which is the newest release `go list -m -versions
github.com/charmbracelet/wish` offers, and names the patched version as none.
The v2 line under `charm.land` carries the same code.

**This server is not affected, and the reason is that it never registers the
middleware.** Its middleware stack is `bubbletea`, `recover`, `activeterm`,
`logging` - there is no file-transfer handler, so there is nothing for a
traversal to traverse. `activeterm` is a second, independent barrier: it
rejects any session without a PTY, which is the shape every SCP session has.

```console
$ ssh -T -p 2222 localhost
Requires an active PTY
```

Both of those are one edit away from stopping being true, and neither edit
would fail to compile or look wrong in review. So `security_test.go` parses
every `.go` file in the module and fails if a `wish/scp` import appears, with
a second test asserting the scan actually reaches `main.go` - a walk that finds
nothing proves nothing.

If file transfer is ever genuinely wanted here, it does not arrive by deleting
that test. It arrives by validating the resolved path against the root in our
own handler, and by writing the advisory's traversal cases as tests that expect
a refusal.

## Running it

Two commands, and the first one is the one you want while changing the UI:

```console
$ bun run preview   # the CV in this terminal. No SSH, no keys, no second window
$ bun run dev       # the real thing: an SSH server on :2222
```

`bun run preview` runs the same model, content and key bindings a session gets
and draws them on the terminal you are already in - the transport is the only
thing missing. Resize the window while it is open; the card follows, and the
frame comes off entirely once there is no room for it. `q` quits. No grant is
passed, so what you see is what a stranger sees.

`bun run dev` is the whole path. Nothing about it needs root, a tailnet or the
Oracle box - the CV is embedded in the binary and the allowlist is a text
file, so it all runs on a high port on your laptop:

```console
$ bun run dev
```

That is the whole command. It generates a throwaway host key, a client key
whose comment is its allowlist entry, and an `authorized_keys` holding it - all
under `.dev/`, which is gitignored - then starts the server and prints the
lines to paste into another terminal:

```console
$ ssh -p 2222 -i .dev/id_ed25519 localhost         # recognised: the footer names the key
$ ssh -p 2222 -o IdentitiesOnly=yes localhost      # everyone else: the same CV
$ ssh -p 2222 -o IdentitiesOnly=yes pt@localhost   # in Portuguese
```

### Running the binary directly

`bun run dev` is a convenience, not the interface. The flags are:

```console
$ bun run build                       # regenerates cv.json, then go build
$ ssh-keygen -t ed25519 -f /tmp/k -N "" -C "laptop"
$ cp /tmp/k.pub /tmp/authorized_keys  # the pub key line *is* the allowlist entry
$ ./bin/ssh-cv --addr localhost:2222 \
    --host-key /tmp/ssh_cv_host_ed25519 \
    --authorized-keys /tmp/authorized_keys
```

If you do, **pass `--host-key` somewhere disposable**. It defaults to
`.ssh/ssh_cv_ed25519` *relative to the working directory* and is generated on
first run, so running from the repo root and from `apps/ssh-cv/` produce two
different server identities and your client warns about a changed host key.
Production wants the opposite: one path, kept forever.

```console
# production
$ SSH_AUTHORIZE_TOKEN=… ./bin/ssh-cv \
    --addr :22 \
    --host-key /var/lib/ssh-cv/host_ed25519 \
    --authorize-url https://api.tone.rip/ssh/authorize
```

| flag | env | meaning |
| --- | --- | --- |
| `--preview` | - | render the CV on this terminal and exit; no server, no keys |
| `--addr` | `SSH_ADDR` | listen address (default `:22`) |
| `--host-key` | `SSH_HOST_KEY` | host key path; generated on first run |
| `--authorize-url` | `SSH_AUTHORIZE_URL` | the `apps/api` endpoint |
| - | `SSH_AUTHORIZE_TOKEN` | gateway bearer token (**never a flag** - flags are visible in `ps`) |
| `--authorized-keys` | `SSH_AUTHORIZED_KEYS_FILE` | local allowlist, for dev instead of the API |
| `--idle-timeout` | - | disconnect after this long with no activity (default 5m) |
| `--max-timeout` | - | hard cap on session duration (default 30m) |

The host key is what gives the server its identity. Generate it once and keep
it: replacing it makes every previous visitor's client warn loudly about a
changed key.

## Checks

```console
$ bun run preview       # look at it
$ bun run check-types   # gofmt -l + go vet
$ bun run test          # go test ./...
```

The tests worth knowing about: the frame is asserted to fit its terminal
exactly at eight sizes from 200x60 down to 20x8 (one row too many and the title
scrolls away, leaving a second footer behind), every index row is asserted to
open onto a page that renders its own title, and `internal/cv` fails if the two
languages stop being symmetric or if a role loses its stack.
