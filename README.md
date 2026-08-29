<div align="center">

<br>

# tone.rip

**A personal site, a self-hosted services dashboard, the API behind both, and a CV you can `ssh` into.**

<br>

[![CI](https://github.com/riptone/tone.rip/actions/workflows/ci.yml/badge.svg)](https://github.com/riptone/tone.rip/actions/workflows/ci.yml)
![Bun](https://img.shields.io/badge/Bun-000000?logo=bun&logoColor=white)
![Turborepo](https://img.shields.io/badge/Turborepo-EF4444?logo=turborepo&logoColor=white)
![Astro](https://img.shields.io/badge/Astro-7-BC52EE?logo=astro&logoColor=white)
![Hono](https://img.shields.io/badge/Hono-E36002?logo=hono&logoColor=white)
![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)
![Cloudflare Workers](https://img.shields.io/badge/Cloudflare_Workers-F38020?logo=cloudflare&logoColor=white)

<br>

[What's in here](#whats-in-here) ·
[The SSH CV](#the-ssh-cv) ·
[The gradient field](#the-gradient-field) ·
[Running it](#running-it) ·
[Docs](#docs)

<br>

</div>

---

## What's in here

Four applications and six shared packages, in one repository. Three of the
applications are Cloudflare Workers. One is a Go binary, because the protocol
it speaks [leaves no choice](#the-ssh-cv).

```
apps/
  web         tone.rip               Astro, server-rendered on Workers
  dashboard   dash.tone.rip          Astro, behind Cloudflare Access
  api         api.tone.rip           Hono on Workers
  ssh-cv      ssh cv.tone.rip        Go - Charm Wish + Bubble Tea

packages/
  ui                 design tokens, the gradient field, the wordmark
  content            CV, site info, app registry - one source of truth per fact
  validation         Zod schemas and an RFC 7807 failure hook
  hono-middleware    security headers, CSP nonces, api-catalog, Access JWTs
  config/            shared tooling configs (typescript, playwright, webview pilot)
    typescript-config  shared tsconfig presets
    playwright-config  shared end-to-end test setup
    webview-config     Bun WebView pilot mirroring Playwright — until cutover
```

One rule explains most of the layout: **if the same logic would otherwise be
written twice, it belongs in a package.** `packages/content/src/cv.ts` is the
clearest case — the website's CV page, the server-rendered block crawlers
read, and the SSH session all render that one module, and CI fails if the
generated copy the Go binary embeds has drifted from it.

`apps/api` exists for the same reason at the service level: one GitHub proxy
with one cache, one Tailscale probe, one CSP-report sink. The front-ends
render markup and ship interaction. They do not each keep their own copy of
the truth.

---

## The SSH CV

```console
$ ssh cv.tone.rip
```

Anyone can connect, and what arrives is the long version of the CV the website
prints — the same content module, plus the company names and per-role detail
that `/cv` leaves out. It reads as an index of sections and a page for each,
inside a window at most 78 columns wide, and switches language with `l`.

It is a Go binary rather than a Worker because a Worker is *invoked with a
request*: it cannot bind a listening socket, so it cannot accept TCP on port
22. Cloudflare Containers do not change that (their SSH is Wrangler-only
administration) and Spectrum is a proxy that still needs an origin. So the SSH
server runs on a small box, while **the authorization endpoint stays on
Workers** — the half that changes often. The key allowlist is a Worker secret,
so access is granted or revoked by editing one value, with no rebuild.

SSH has no SNI, so the server never learns which hostname you dialled. Nothing
is decided by hostname and nothing is split across ports: every name reaches
one server and every session gets the whole CV. A recognised key buys its
label in the footer and gates nothing.

---

## The gradient field

The soft, grainy colour panel behind both sites is `@repo/ui/gradient` —
framework-free, so either Astro app can use it without adopting a component
framework.

```ts
import { mountNoiseGradient, rowsToCss } from "@repo/ui/gradient";

const field = mountNoiseGradient(document.querySelector("#panel"), {
  ramp: "moss",
  onFrame: ({ profile }) => {
    // Fill an accent from the colours on screen *this frame*.
    document.documentElement.style.setProperty("--field-ramp", rowsToCss(profile));
  },
});
```

It renders at a quarter size in a Web Worker and is stretched back up — the
upscale *is* the softness, and there is no blur filter anywhere. Palettes are
built in OKLCh with gamut mapping, so switching one changes hue rather than
weight. The loop self-cancels once it converges, so an idle page costs zero
frames a second.

How it works, and the four decisions that carry the look:
[`packages/ui/README.md`](./packages/ui/README.md).

---

## Running it

```bash
bun install
bun run dev
```

That starts every application at once. Individually:

| | |
| --- | --- |
| `cd apps/web && bun run dev` | the public site, `localhost:4321` |
| `cd apps/dashboard && bun run dev` | the services dashboard |
| `cd apps/api && bun run dev` | the API, on Workers locally |
| `cd apps/ssh-cv && bun run dev` | the SSH CV, then `ssh -p 2222 localhost` |

Nothing here needs credentials to run. The SSH CV generates its own throwaway
host key and allowlist under `.dev/`; the API falls back to unauthenticated
GitHub requests; the dashboard renders its tiles without live status.

| command | what it does |
| --- | --- |
| `bun run dev` | every app at once |
| `bun run build` | build all apps and packages |
| `bun run test` | Vitest everywhere, `go test` for `apps/ssh-cv` |
| `bun run lint` | Biome — formatting and lint in one pass |
| `bun run check-types` | `astro check` / `tsc` / `gofmt` + `go vet` |
| `bun run ci` | the full gate, exactly as CI runs it |

Contributing, and what the full gate actually checks:
[`docs/engineering.md`](./docs/engineering.md).

---

## Deploying

`main` deploys itself: merging runs the gate, then `wrangler deploy` for each
Worker.

`apps/ssh-cv` cannot work that way — it is a binary on a box, and nothing in
Cloudflare can push to it — so it is *released* rather than deployed, and the
box pulls. A tag builds `linux/amd64` and `linux/arm64`, stamps the version
in, and publishes both with checksums; the box takes it from there on a daily
timer, verifying the checksum and rolling back if the service does not come
up.

Runbooks: [`docs/deployment.md`](./docs/deployment.md) for the Workers,
[`docs/ssh-cv-deployment.md`](./docs/ssh-cv-deployment.md) for the box.

---

## Docs

| | |
| --- | --- |
| [`AGENTS.md`](./AGENTS.md) | the project constitution — read first |
| [`docs/architecture.md`](./docs/architecture.md) | how the apps and packages fit, and why |
| [`docs/engineering.md`](./docs/engineering.md) | language, validation, API and git conventions |
| [`docs/constitution.md`](./docs/constitution.md) | brand, visual system, motion, typography |
| [`docs/deployment.md`](./docs/deployment.md) | the Cloudflare Workers runbook |
| [`docs/ssh-cv-deployment.md`](./docs/ssh-cv-deployment.md) | putting `ssh cv.tone.rip` on the internet |
| [`docs/SECURITY.md`](./docs/SECURITY.md) | reporting a vulnerability, and the security model |
| [`packages/ui/README.md`](./packages/ui/README.md) | the design system and the gradient field |

---

<div align="center">
<sub>No god-files. No circular dependencies. <code>bun run ci</code> is the gate.</sub>
</div>
