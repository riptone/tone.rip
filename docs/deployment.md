# Deploying to Cloudflare

This monorepo deploys three Cloudflare Workers from **one** repo - `apps/web`, `apps/dashboard`, `apps/api` - serving `tone.rip`, `dash.tone.rip`, and `api.tone.rip`. This doc is a runbook for deploying and cutting over DNS - I haven't run any of these steps myself (deploying, changing DNS/custom domains, or deleting the old Workers are all live-infrastructure changes that need your Cloudflare account, and are risky enough that they should be deliberate, not automated by an agent).

## The target end state

| Hostname | Worker (this repo) | Notes |
|---|---|---|
| `tone.rip` | `apps/web` | |
| `www.tone.rip` | `apps/web` (same Worker, second custom domain) | `apps/web/src/middleware.ts` already 301-redirects `www` → apex - **no separate Worker needed for this.** Whatever currently serves `www.tone.rip` can be retired once this domain points at the `web` Worker instead. |
| `dash.tone.rip` | `apps/dashboard` | |
| `api.tone.rip` | `apps/api` | |

So: **3 Workers, all from this repo**, with `www.tone.rip` as a second custom domain on `apps/web` (no separate Worker).

## Why not fewer Workers?

You asked whether `www` could just be folded into `api` - it's simpler to fold it into `web` instead, since `web` already contains the exact redirect logic (ported verbatim from the original middleware) and serving it from `api` would mean `api` needs to know about `web`'s hostname concerns, which is the wrong direction of coupling. One Worker, two custom domains (`tone.rip` + `www.tone.rip`) pointed at it, is the standard Cloudflare pattern for this.

## Order of operations

Do this during low-traffic hours, and don't delete anything old until the new Worker has been serving real production traffic successfully for a while.

### 1. Stand up `apps/api` first (nothing depends on it yet, so it's zero-risk)

```bash
cd apps/api
wrangler secret put GITHUB_TOKEN               # optional - raises the GitHub API rate limit for /projects
wrangler secret put TAILSCALE_OAUTH_CLIENT_ID  # optional - only if /status should report Tailscale device status
wrangler secret put TAILSCALE_OAUTH_CLIENT_SECRET
wrangler secret put TAILSCALE_TAILNET
wrangler secret put TAILSCALE_STATUS_DEVICE
wrangler deploy
```

`wrangler.jsonc` already declares `"routes": [{ "pattern": "api.tone.rip", "custom_domain": true }]`, so `wrangler deploy` provisions the custom domain itself - no dashboard click needed. Verify `curl https://api.tone.rip/status` and `curl https://api.tone.rip/.well-known/api-catalog` before moving on.

### 2. Verify `apps/web` and `apps/dashboard` before touching production DNS

`apps/web/wrangler.jsonc` has `workers_dev: false` and `preview_urls: false` (no publicly-guessable `*.workers.dev` URL bypassing the intended custom domain), so its `*.workers.dev` URL isn't available even after deploying. Verify it locally instead:

```bash
cd apps/web && wrangler dev          # http://localhost:8787 - click through the globe/panels/theme toggle
```

`apps/dashboard` doesn't set `workers_dev: false`, so its preview URL works after a real deploy:

```bash
cd apps/dashboard && wrangler deploy  # check the printed *.workers.dev URL - tiles render, /status is populated from api.tone.rip
```

If you want a pre-cutover check against the real Worker (not just local `wrangler dev`) for `apps/web` too, temporarily bind a throwaway custom domain (e.g. `web-preview.tone.rip`) to it, verify, then remove that binding before the real cutover in step 3.

### 3. Cut over `tone.rip` and `www.tone.rip`

In the dashboard: add `tone.rip` and `www.tone.rip` as custom domains on the **new** `web` Worker. Cloudflare will generally require removing a custom domain from its old Worker before it can be attached to a new one - so this is a brief-downtime swap, not a zero-downtime one, unless you stage it through a maintenance page. Do `tone.rip` and `www.tone.rip` in the same sitting so there's no window where one redirects to the other's old Worker.

### 4. Cut over the dashboard subdomain

Same as above, but for the dashboard subdomain → `dash.tone.rip`, pointed at the new `dashboard` Worker.

### 5. Confirm, then retire the old Workers

Once `tone.rip`, `www.tone.rip`, and the dashboard subdomain have all been serving from the new Workers without issues for a bit: delete any old Workers from the Cloudflare dashboard that previously served these hostnames (if they exist as separate Workers rather than bare DNS redirect rules).

## Zone settings that are not in this repo

Two Cloudflare toggles affect these Workers and live only in the dashboard, so
nothing here can set or assert them. Both were found the same way - by looking
at real response headers from production, not by reading docs.

### Speed Brain: turn it off

**Speed → Optimization → Content Optimization → Speed Brain.**

Speed Brain injects a `Speculation-Rules` header pointing at
`/cdn-cgi/speculation`, which asks the browser to prefetch links. It then
refuses to serve the prefetch it just asked for:

```console
$ curl -sI -H 'Sec-Purpose: prefetch' https://tone.rip/work
HTTP/2 503
cf-speculation-refused: prefetch refused: disabled for worker requests
```

Every route on this zone is a Worker route, so **it can never do anything but
refuse.** What it produced instead was a `503` for every speculative request -
which is where the `GET https://tone.rip/work net::ERR_ABORTED 503` in the
console came from, on hover, once per nav link, because Astro's ClientRouter
prefetched on hover.

The site no longer emits any prefetch hint of its own (ClientRouter is gone and
Astro's `prefetch` is off), so the console is clean either way. Turning the
setting off matters for the other half: with Speed Brain out of the way, a
genuine `<link rel="prefetch">` or a `speculationrules` block would reach the
Worker and actually work, which is worth having now that navigation is a real
page load. Until then, do not add one - it will 503.

### JavaScript Detections: off, if you want Trusted Types

**Security → Bots → JavaScript Detections.**

Cloudflare injects a bootstrap script into every HTML response, *after* the
Worker has run:

```js
var d = b.createElement('script');
d.nonce = '<the nonce off your own CSP>';
d.innerHTML = "window.__CF$cv$params={…}";
```

Two things are worth noticing. It reads the nonce off the response and reuses
it, so a strict `script-src 'self' 'nonce-…'` admits it - that is deliberate
on Cloudflare's part and it means the nonce is not the boundary you might
assume. And it assigns to `innerHTML`, which no Trusted Types policy can
permit without ceasing to be one.

So `require-trusted-types-for 'script'` and this feature cannot both be on.
Enforcing it threw a `TypeError` on every page load in production while
looking perfectly clean locally - `wrangler dev` does not run the edge
injectors, so nothing in this repository can catch it.

The switch is `trustedTypes: true` in `buildSecurityHeaders`
(`packages/hono-middleware/src/core.ts`), off by default. Before flipping it,
turn off JavaScript Detections **and** Speed Brain, then check the deployed
HTML for injected `<script>` tags:

```console
$ curl -s https://tone.rip/ | grep -c '__CF\$cv\$params'
0
```

The site itself has no HTML sinks at all - no `innerHTML`, `outerHTML`,
`insertAdjacentHTML`, `document.write` or `eval` anywhere in the client code -
so the policy costs nothing to satisfy. The only thing standing in the way is
the edge.

### HTML is deliberately uncacheable

Every HTML response carries `Cache-Control: private, max-age=0,
must-revalidate` (`packages/hono-middleware/src/astro-security.ts`) because the
document embeds a per-request CSP nonce. A shared cache handing a stored copy
to a second visitor would be handing them a nonce their response's policy never
allowed, and the page would render unstyled.

This overrides anything a page sets for itself, so **do not add a
`Cache-Control` in a page's frontmatter** - it is discarded one layer up and
reads as a decision that was made when it was not. Cache the *data* instead,
where it can actually be held (see `apps/web/src/services/projects.ts`).

## Ongoing deploys

`.github/workflows/ci.yml` has a `deploy` job that runs after the `ci` job passes, on pushes to `main` only, and runs `wrangler deploy` for `apps/api`, `apps/web`, and `apps/dashboard` via `cloudflare/wrangler-action`. It authenticates with a `CLOUDFLARE_API_TOKEN` repository secret - create one at Cloudflare dashboard → My Profile → API Tokens → "Edit Cloudflare Workers" template scoped to this account, then add it as a secret at GitHub → repo Settings → Secrets and variables → Actions. Until that secret exists, the `deploy` job will fail (the `ci` job is unaffected). Merges to `main` that only touch one app still redeploy all three - cheap enough at this scale not to bother with path filtering.

### Catching a Worker outgrowing its script-size limit before it does

Cloudflare enforces a hard cap on how big a Worker's *bundled script* can be (compressed) - separate from, and much smaller than, the static-assets limit. The `ci` job's "Check Worker bundle sizes" step (`bun run check-bundle-size`, `scripts/check-bundle-size.ts`) dry-run bundles all three Workers the same way `wrangler deploy` would and reads the gzip size it reports, failing the build if any Worker exceeds a 2 MiB budget - current usage is 120-230 KiB per Worker, so this is early warning for an accidentally-bundled dependency, not a tight ceiling. Raise a Worker's budget in that file if legitimate growth trips it; don't raise it to silence a regression you haven't looked at.
