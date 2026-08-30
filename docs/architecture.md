# Architecture

How the apps and packages fit together, and why things live where they do. For the visual/brand philosophy, see [constitution.md](./constitution.md). For technical conventions, see [engineering.md](./engineering.md). For deploying this, see [deployment.md](./deployment.md).

## The four apps

- **`apps/web`** - the public site, tone.rip. Astro, server-rendered, its own Cloudflare Worker.
- **`apps/dashboard`** - the self-hosted-services launcher, dash.tone.rip. Astro, its own Cloudflare Worker, gated behind a Cloudflare Access policy (not app-level auth code).
- **`apps/api`** - api.tone.rip. Hono, Cloudflare Workers, the single source of truth for anything both apps (or an external agent) might need.
- **`apps/ssh-cv`** - the CV over SSH (`ssh cv.tone.rip`). Go, not Workers, for the reason below. Serves the same CV content the website renders, at more depth: the company names and the per-role detail `/cv` leaves out.

Both Astro apps are deliberately thin: they render markup and ship the client-side interaction (the gradient field, the filter, the language switch, the right-click menu). Neither has its own API routes for data that isn't page-specific - that's what `apps/api` is for.

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

That proxy, and the dashboard's server-side render of the tile list, reach `apps/api` through a Cloudflare **service binding** (`API` in `apps/dashboard/wrangler.jsonc`) rather than over the public internet - both Workers are on the same account, and the round trip through DNS, TLS and Cloudflare's edge was sitting in the dashboard's TTFB. The binding skips the edge, and therefore skips the Access gate on `api.tone.rip` - which changes nothing, because that gate never protected those routes from this caller in the first place. `requireCloudflareAccess()` inside `apps/api` is what does, and it still verifies the JWT the dashboard forwards. See `apps/dashboard/src/lib/api.ts`.

## Why every app runs the same Hono middleware

`apps/web` and `apps/dashboard` are each their own Cloudflare Worker, so for a long time they could not run `apps/api`'s Hono middleware: they had to reimplement it against Astro's `middleware.ts` signature. That produced a second security middleware (`astro-security.ts`) and four hand-written copies of middleware that already existed in the package - the www redirect, the dev robots.txt, the RFC 9727 catalog and the markdown negotiation, three of which had no other consumer at all.

Astro 7 removed the constraint. `src/fetch.ts` is a Hono app, and `astro/hono` exposes Astro's own pipeline as Hono middleware (`astro()` runs routing, sessions, middleware, redirects, actions and pages), so all three apps now compose the *same* middleware from `packages/hono-middleware`. `core.ts` still holds the policy as plain framework-agnostic functions (`buildSecurityHeaders`, `buildApiCatalogBody`) and `securityHeaders()`/`apiCatalog()` still wrap them - but there is now one wrapper rather than one per framework.

The one piece that stays per-app is the bridge from the Hono context to `Astro.locals.cspNonce`, because `BaseHead.astro` reads the nonce from there. It is three lines in each `fetch.ts`.

It cost about 18 KiB gzip per Astro Worker (the Hono runtime plus the `astro/hono` pipeline), measured; page weight and Lighthouse are unchanged, since none of it reaches the browser.

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
| `packages/ui` | Everything both front ends share: `BaseHead.astro`, the design tokens and reset, the frame (`shell.css`), the chrome (`site/`: `Footer.astro`, `ContextMenu.astro`, the filter, the language switch, the field controller), the brand mark, the noise gradient field (`gradient/`, `motion/`), the 404 page (`site/NotFound.astro`), the WCAG contrast helpers both colour guards use (`contrast.ts`), and the static assets those reference (`public/`: the font, the favicons, the icon set) - see `packages/ui/README.md` | `apps/web`, `apps/dashboard` |
| `packages/content` | The CV (`cv.ts`), GitHub-repo simplification, CSP-report summarizing, per-site info/markdown, the `Person` schema, and the contact facts (`contact.ts`) every surface states | `apps/api`, `apps/dashboard`, `apps/web`, `apps/ssh-cv` |
| `packages/validation` | Zod schemas, an RFC 7807 validation-failure hook for `@hono/zod-validator` | `apps/api`, `apps/web`, `apps/dashboard` |
| `packages/net` | `fetchWithTimeout` / `withTimeout` - the AbortController-plus-timer every reachability check needs | `apps/api`, `apps/web`, `apps/dashboard` |
| `packages/hono-middleware` | Composable Hono middleware + the framework-agnostic core both Astro apps call directly, including `requireCloudflareAccess()` | `apps/api`, `apps/web`, `apps/dashboard` |
| `packages/config/typescript-config` | Shared tsconfig presets (`base`, `library`, `astro`, `hono-jsx`) | every app/package |
| `packages/config/vitest-config` | `defineTestConfig()` - the coverage/include boilerplate every suite had written out by hand, plus the jsdom localStorage polyfill that asking for `environment: "jsdom"` now brings with it | every package with tests |
| `packages/config/playwright-config` | The e2e webServer/CSP-console-collector wiring both Astro apps' `playwright.config.ts` need identically | `apps/web`, `apps/dashboard` |
| `packages/config/webview-config` | The Bun WebView webServer/CSP-console-collector wiring mirroring the Playwright one — pilot alongside it until cutover | `apps/web` |

`packages/ui` used to occupy four rows of this table - one for the package and
one each for `site/`, `styles/` and `gradient/`. That was the table noticing
something the package.json was hiding: `exports` was a single wildcard
(`"./*": "./src/*"`), so every file under `src/` was public and the package
had no stated shape at all. It is one row now because the wildcard is gone and
the entry points are named, which is also what let `knip` find the four
exports nobody imported. Splitting the package was considered and rejected:
two consumers do not pay for four workspaces.

## Generated files, and which ones are committed

Two files in this repo are generated from something else, and they are treated
oppositely on purpose.

`apps/ssh-cv/internal/cv/cv.json` **is** committed. The Go binary embeds it, so
a checkout with no Bun still builds - and CI fails if it has drifted from
`packages/content`.

`apps/*/worker-configuration.d.ts` is **not**, any more. Three files, 46,653
lines, 1.67MB - 58% of everything tracked here - and nothing consumes them
without an install anyway. Two of the three were silently out of date when this
was written; `apps/web`'s had been generated against a compatibility date its
own `wrangler.jsonc` left behind a year earlier, so it was type-checking
against a runtime it did not run. Regenerating one produced a 25,923-line diff.

The deciding fact was that a committed copy cannot be correct for everyone:
`wrangler types` reads `.dev.vars` as well as `wrangler.jsonc`, and `.dev.vars`
is gitignored - so a file generated on a machine with credentials types six
secrets as required `string`s, and the same command in CI does not. That also
rules out `wrangler types --check` as a gate. They are generated by turbo's
`types` task instead, keyed on `wrangler.jsonc`, and `apps/api/src/env.ts`
states what the code expects rather than inheriting the unstable shape.

## Rule of thumb for "does this go in a package?"

If the same logic would otherwise need writing twice - once in `apps/web`, once in `apps/dashboard` - it belongs in a package. If it's genuinely specific to one app's content or layout, it stays local to that app.
