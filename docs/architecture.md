# Architecture

How the three apps and five packages fit together, and why things live where they do. For the visual/brand philosophy, see [constitution.md](./constitution.md). For technical conventions, see [engineering.md](./engineering.md). For deploying this, see [deployment.md](./deployment.md).

## The three apps

- **`apps/web`** - the public site, tone.rip. Astro, server-rendered, its own Cloudflare Worker.
- **`apps/dashboard`** - the self-hosted-services launcher, dash.tone.rip. Astro, its own Cloudflare Worker, gated behind a Cloudflare Access policy (not app-level auth code).
- **`apps/api`** - api.tone.rip. Hono, Cloudflare Workers, the single source of truth for anything both apps (or an external agent) might need.
- **`apps/ssh-cv`** - the CV over SSH (`ssh cv.tone.rip`). Go, not Workers, for the reason below. Serves the same CV content the website renders, at more depth: the company names and the per-role detail `/cv` leaves out.

Both Astro apps are deliberately thin: they render markup and ship the client-side interaction (globe, panels, filters, theme toggle). Neither has its own API routes for data that isn't page-specific - that's what `apps/api` is for.

## Why the API is centralized

Before this monorepo existed, the site and dashboard were separate repos, each with its own Cloudflare Worker doing its own GitHub-proxy caching, its own Tailscale OAuth probing, its own CSP-report ingestion. Centralizing all of that in `apps/api` means:

- One cache/ETag-revalidation implementation for the GitHub repos proxy (`GET /projects`), not two.
- One Tailscale-OAuth + app-health-probing implementation (`GET /status`), reusable by both `apps/web` (if it ever needs live status) and `apps/dashboard`.
- One CSP-report ingestion endpoint (`POST /csp-report`), validated with Zod (`packages/validation`) instead of hand-parsed JSON.

## Why `apps/ssh-cv` is not on Cloudflare Workers

Every other app here is a Worker. This one cannot be, and the reason is the
protocol rather than the language - so no amount of Go-on-Workers tooling
(`syumai/workers` and friends) changes it. A Worker is *invoked with a
request*; it cannot bind a listening socket, so it cannot accept a TCP
connection on port 22 or perform the server half of an SSH handshake.
`connect()` exists but is outbound only.

So the SSH server needs a host with a real IP and port 22 (a small VPS, a
Fly.io machine with a raw TCP service, the tailnet box). What *does* stay on
Workers is the part that changes often: `apps/api`'s `POST /ssh/authorize`
resolves a key fingerprint to its scopes from an allowlist held in a Worker
secret, so access is granted or revoked by editing one value - no Go rebuild,
no shell on the SSH host. That endpoint is deliberately absent from the RFC
9727 catalog and `GET /`: it is a control-plane endpoint for one client, not a
public API.

A second constraint shapes the UX: **SSH has no SNI.** The server never learns
which hostname you dialled, so two names pointing at one address are
indistinguishable there. Nothing is therefore decided by hostname, and nothing
is split across ports (`ssh -p 2222 …` is not a thing anyone wants to type):
every name reaches one server and every session gets the whole CV. A
recognised key changes one word in the footer - its label - and gates nothing,
because the CV is as public as the website. See `apps/ssh-cv/README.md`.

## Why `apps/dashboard` is gated by Cloudflare Access, not app code

`apps/dashboard` sits entirely behind a Cloudflare Access application (an edge-level login wall tied to the account's identity providers) rather than any homegrown session/login system - there's exactly one internal-facing app that needs gating, so paying for a Hono/Astro auth stack (sessions, a login form, password storage) would be solving a problem Cloudflare's edge already solves for free. `packages/hono-middleware/src/cloudflare-access.ts` exports `requireCloudflareAccess()`, a small Hono middleware that verifies a Cloudflare Access JWT against the team's JWKS - it exists because `apps/api`'s `/status` route is reached cross-hostname via a server-to-server proxy (`apps/dashboard/src/pages/api/status.ts` forwards its own already-Access-verified request's JWT to `api.tone.rip`), a hop Access's own edge gating doesn't cover since that gating is scoped to `dash.tone.rip`, not `api.tone.rip`.

## Why the security/CSP logic is a plain function, not just Hono middleware

`apps/web` and `apps/dashboard` are each their own Cloudflare Worker - they don't run through `apps/api`'s Hono app, so they can't use Hono middleware directly. But the CSP-nonce-generation and security-header logic needs to be identical everywhere (that's the whole point of centralizing it). The fix: `packages/hono-middleware/src/core.ts` exports plain, framework-agnostic functions (`buildSecurityHeaders`, `buildApiCatalogBody`). `apps/api` wraps them as Hono middleware (`securityHeaders()`, `apiCatalog()`); `apps/web`/`apps/dashboard` call the same core functions directly from their own Astro `middleware.ts`. One implementation, two thin adapters.

## Why site content lives in `packages/content`, not each app

The CV is the clearest case. `packages/content/src/cv.ts` holds it once,
bilingually, and three surfaces render it: `apps/web`'s CV panel, the
server-rendered homepage block that crawlers and LLMs read, and `apps/ssh-cv`
over SSH. The Go binary cannot import TypeScript, so
`apps/ssh-cv/scripts/generate-content.ts` compiles the module to a JSON file
that Go embeds - and CI fails if the committed copy has drifted from the
source.

Organisations are described by what they do rather than named - an editorial
choice, applied at the source so every surface inherits it. The SSH CV is the
one exception, and it is deliberate: `Experience.company` and
`Experience.detail` are authored in the same module and rendered only there.
Typing `ssh cv.tone.rip` is a deliberate act by someone who wants the long
version; a search result is not. Which is also why `packages/content`'s
`buildPersonSchema` still omits `worksFor` - JSON-LD describes the page it
sits on, and structured data claiming more than the markup shows is a spam
signal.

`packages/content/src/site-info.ts` defines each site's name, tagline, description, links, and agent-readable markdown once. `apps/api`'s `/info/:slug` route serves that same record three ways - as JSON, as markdown (via `Accept: text/markdown` content negotiation), and as server-rendered HTML via `hono/jsx` - so there's one source of truth instead of three copies of the same "here's who we are" text drifting apart.

## Package map

| Package | What it holds | Consumed by |
|---|---|---|
| `packages/ui` | `BaseHead.astro` (meta tags, theme bootstrap, OG/schema.org), design tokens + reset CSS, a vanilla-DOM component kit (`components.ts`/`styles/components.css`) - see `packages/ui/README.md` | `apps/web`, `apps/dashboard` |
| `packages/ui` → `site/` | The chrome both properties share: `Footer.astro`, `Reveal.astro`, `ContextMenu.astro`, the filter, the language switch, the field controller | `apps/web`, `apps/dashboard` |
| `packages/ui` → `styles/` | The frame (`shell.css`), the 404, and the look of anything that appears in both apps - the menu surface (`context-menu.css`, `filter.css`, sharing `--menu-*` tokens) and the view-transition at-rule | `apps/web`, `apps/dashboard` |
| `packages/content` | Self-hosted app registry, GitHub-repo simplification, CSP-report summarizing, per-site info/markdown, **the CV** (`cv.ts`) | `apps/api`, `apps/dashboard`, `apps/web`, `apps/ssh-cv` |
| `packages/validation` | Zod schemas, an RFC 7807 validation-failure hook for `@hono/zod-validator` | `apps/api` |
| `packages/ui` → `gradient/`, `motion/` | Noise gradient field (canvas + worker) and smoothed scroll progress - see `packages/ui/README.md` | `apps/web` |
| `packages/hono-middleware` | Composable Hono middleware + the framework-agnostic core both Astro apps call directly, including `requireCloudflareAccess()` | `apps/api`, `apps/web`, `apps/dashboard` |
| `packages/typescript-config` | Shared tsconfig presets (`base`, `astro`, `hono-jsx`) | every app/package |
| `packages/playwright-config` | The e2e webServer/CSP-console-collector wiring both Astro apps' `playwright.config.ts` need identically | `apps/web`, `apps/dashboard` |

## Rule of thumb for "does this go in a package?"

If the same logic would otherwise need writing twice - once in `apps/web`, once in `apps/dashboard` - it belongs in a package. If it's genuinely specific to one app's content or layout, it stays local to that app.
