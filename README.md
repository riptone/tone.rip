<div align="center">

<br>

# tonil

**One repo behind [tone.rip](https://tone.rip), its self-hosted services dashboard, the API tying them together - and a CV you can `ssh` into.**

<br>

[![CI](https://github.com/no-tone/tonil/actions/workflows/ci.yml/badge.svg)](https://github.com/no-tone/tonil/actions/workflows/ci.yml)
![Bun](https://img.shields.io/badge/Bun-000000?logo=bun&logoColor=white)
![Turborepo](https://img.shields.io/badge/Turborepo-EF4444?logo=turborepo&logoColor=white)
![Astro](https://img.shields.io/badge/Astro-7-BC52EE?logo=astro&logoColor=white)
![Hono](https://img.shields.io/badge/Hono-E36002?logo=hono&logoColor=white)
![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)
![Cloudflare Workers](https://img.shields.io/badge/Cloudflare_Workers-F38020?logo=cloudflare&logoColor=white)
![Biome](https://img.shields.io/badge/Biome-60A5FA?logo=biome&logoColor=white)

<br>

[Quick start](#quick-start) ·
[What's in here](#whats-in-here) ·
[The gradient field](#the-gradient-field) ·
[The SSH CV](#the-ssh-cv) ·
[Commands](#commands) ·
[Docs](#docs)

<br>

</div>

---

## Quick start

```bash
bun install
bun run dev
```

That starts every app at once. Individually:

| | |
| --- | --- |
| `cd apps/web && bun run dev` | the public site, `localhost:4321` |
| `cd apps/dashboard && bun run dev` | the services dashboard |
| `cd apps/api && bun run dev` | the API, on Workers locally |
| `cd apps/ssh-cv && bun run dev` | the SSH CV, then `ssh -p 2222 localhost` |

Before opening a PR, run the same gate CI does:

```bash
bun run lint && bun run check-types && bun run test && bun run knip && bun run check-cycles
```

---

## What's in here

Four apps and six packages. Three of the apps are Cloudflare Workers; one is a
Go binary, for [a reason the protocol forces](#the-ssh-cv).

```
apps/
  web         tone.rip               Astro, SSR on Workers
  dashboard   dash.tone.rip          Astro, behind Cloudflare Access
  api         api.tone.rip           Hono on Workers
  ssh-cv      ssh cv.tone.rip        Go - Charm Wish + Bubble Tea

packages/
  ui                 design tokens, BaseHead, the gradient field, the wordmark
  content            CV, site info, app registry - one source of truth per fact
  validation         Zod schemas + an RFC 7807 failure hook
  hono-middleware    security headers, CSP nonce, api-catalog, Access JWTs
  typescript-config  shared tsconfig presets
```

The rule that keeps it honest: **if the same logic would otherwise be written
twice, it belongs in a package.** `packages/content/src/cv.ts` is the clearest
case - the website panel, the server-rendered block crawlers read, and the SSH
session all render the same module, and CI fails if the generated copy the Go
binary embeds has drifted from it.

`apps/api` exists because everything that is not page-specific lives in one
place: one GitHub proxy with one cache, one Tailscale probe, one CSP-report
sink. The frontends render markup and ship interaction. They do not each keep
their own copy of the truth.

---

## The gradient field

The soft, grainy colour panel on `/v2` is `@repo/ui/gradient`. It is
framework-free, so both Astro apps can use it without either adopting a
component framework.

```ts
import { mountNoiseGradient, rowsToCss } from "@repo/ui/gradient";
import { subscribeScrollProgress } from "@repo/ui/motion/scroll-progress";

const field = mountNoiseGradient(document.querySelector("#panel"), {
  ramp: "moss",
  onFrame: ({ profile }) => {
    // Fill an accent from the colours on screen *this frame*.
    document.documentElement.style.setProperty("--field-ramp", rowsToCss(profile));
  },
});

subscribeScrollProgress(({ progress }) => field.update({ progress }));
```

Four decisions carry the whole look:

- **The scroll is smoothed, not tracked.** `progress` chases the real position
  with frame-rate-independent exponential decay over ~260 ms, so the field
  arrives a beat late and settles rather than stopping. Binding straight to
  `scrollY` is what makes motion feel welded to the wheel.
- **The upscale is the softness.** The field renders at a quarter size in a
  Web Worker and is stretched back up. There is no blur filter anywhere.
- **Grain is a DOM layer, not canvas pixels.** A 200 px tile at one noise
  pixel per *device* pixel, `mix-blend-mode: overlay`. In-canvas grain would
  be resampled to mush by that same upscale.
- **Nothing renders while nothing moves.** The rAF loop self-cancels once it
  converges and the worker idles. An untouched page costs zero frames a
  second.

Palettes are built in OKLCh with gamut mapping, so every ramp lands on the
same lightness ladder and switching one changes hue rather than weight.
`duotoneRamp(from, to)` mixes any two colours, walking hue the short way round
the circle - the long way is what drags a gradient through grey.

Full write-up in [`packages/ui/README.md`](./packages/ui/README.md). Tune it
live at `/v2`, which has a frames-per-second readout so idle cost is visible.

---

## The SSH CV

```console
$ ssh cv.tone.rip
```

Anyone can connect and read the CV - the long version of it: the same content
module the website renders, plus the company names and the per-role detail
`/cv` leaves out. It reads as an index of sections and one page each, inside a
window at most 78 columns wide, and switches language with `l`.

It is a Go binary rather than a Worker because a Worker is *invoked with a
request* - it cannot bind a listening socket, so it cannot accept TCP on port
22. Cloudflare Containers do not change that (their SSH is Wrangler-only
administration), and Spectrum is a proxy that still needs an origin. So the
SSH server runs on a small box and **the authorization endpoint stays on
Workers**, which is the half that changes often: the key allowlist lives in a
Worker secret, so access is granted or revoked by editing one value.

SSH has no SNI, so the server never learns which hostname you dialled. Nothing
is therefore decided by hostname and nothing is split across ports: every name
reaches one server and every session gets the whole CV. A recognised key buys
its label in the footer and gates nothing.

Setup and hardening: [`docs/ssh-cv-deployment.md`](./docs/ssh-cv-deployment.md).

---

## Commands

Run from the repo root; Turborepo fans them out.

| command | what it does |
| --- | --- |
| `bun run dev` | every app at once |
| `bun run build` | build all apps and packages |
| `bun run lint` | Biome - formatting and lint in one pass |
| `bun run check-types` | `astro check` / `tsc` / `gofmt` + `go vet` |
| `bun run test` | Vitest everywhere, `go test` for `apps/ssh-cv` |
| `bun run knip` | unused files, exports and dependencies |
| `bun run check-cycles` | madge - import cycles, of which there are none |
| `bun run format` | Biome, writing fixes |

---

## Deploy

`main` deploys itself. Merging runs the full gate, then `wrangler deploy` for
each Worker.

`apps/ssh-cv` cannot work that way - it is a binary on a box in Oracle Cloud,
and nothing in Cloudflare can push to it - so it is released rather than
deployed, and the box pulls:

```bash
cd apps/ssh-cv && bun run release patch --push   # tags it; CI builds and publishes
```

A tag builds `linux/amd64` and `linux/arm64`, stamps the version in, and
publishes both with checksums. The box takes it from there on a daily timer,
verifying the checksum and rolling back if the service does not come up. See
its [runbook](./docs/ssh-cv-deployment.md).

Secrets are Worker secrets, set with `wrangler secret put`, never committed.
`.dev.vars` is gitignored.

---

## Docs

| | |
| --- | --- |
| [`AGENTS.md`](./AGENTS.md) | the project constitution - read first |
| [`docs/constitution.md`](./docs/constitution.md) | brand, visual system, motion, typography |
| [`docs/engineering.md`](./docs/engineering.md) | language, validation, API and git conventions |
| [`docs/architecture.md`](./docs/architecture.md) | how the apps and packages fit, and why |
| [`docs/deployment.md`](./docs/deployment.md) | the Cloudflare Workers runbook |
| [`docs/ssh-cv-deployment.md`](./docs/ssh-cv-deployment.md) | putting `ssh cv.tone.rip` on the internet |
| [`packages/ui/README.md`](./packages/ui/README.md) | the design system and the gradient field |

---

<div align="center">
<sub>No god-files. No circular dependencies. <code>bun run knip</code> and <code>bun run check-cycles</code> before you call it done.</sub>
</div>
