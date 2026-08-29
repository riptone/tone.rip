# Security Policy

This is the security policy for [riptone/tone.rip](https://github.com/riptone/tone.rip) -
the monorepo behind tone.rip (three Cloudflare Workers, an SSH server, and
the packages they share). It is written
to be followed by humans and by the AI agents that help maintain this repo.

## Supported versions

**The three Workers have no versioned releases.** Everything on `main` is
deployed: a push to `main` runs CI, then `wrangler deploy` for `apps/web`,
`apps/dashboard` and `apps/api` - see `.github/workflows/ci.yml` and
`docs/deployment.md`.

**`apps/ssh-cv` does**, because it cannot work the same way: it is a binary on
a box, not a Worker, so it is released from a tag (`ssh-cv/vX.Y.Z`) and the box
pulls. Only the newest release is supported - the box updates itself on a daily
timer, so an older one is a box that has not run its updater, not a version
anybody is expected to maintain. See `docs/ssh-cv-deployment.md`.

| Source | Supported |
| ------ | --------- |
| `main` (the deployed state of the Workers) | ✅ |
| The newest `ssh-cv/v*` release | ✅ |
| An older `ssh-cv/v*` release | ❌ - update first (`sudo ssh-cv-update`), then reproduce |
| Anything else (other tags, forks, local branches) | ❌ - reproduce on `main` before reporting |

Security fixes for the Workers land on `main` and are live as soon as the
deploy job finishes. A fix for `apps/ssh-cv` needs a release cut after it
(`bun run release patch --push`) or no box will ever see it. There is no
backport process for either.

## Scope

**In scope:** code and configuration in this repo - the Workers, the
`packages/*` middleware (CSP, security headers, validation), the API, and the
docs that govern deployment.

**Out of scope (report to Cloudflare, not here):** zone-level dashboard
settings that are not in this repo - Speed Brain, JavaScript Detections,
Managed Transforms, Transform Rules, Cloudflare Access on the dashboard
subdomain. They affect this site but are owned by the Cloudflare account, not
by this repository. See `docs/deployment.md` → "Zone settings that are not in
this repo" for the full list.

## Security model, in brief

Ground truth for what this repo already does, so a report can be checked
against it before it is filed (details in `docs/engineering.md` and
`docs/deployment.md`):

- **CSP**: strict, nonce-based. `script-src 'self' 'nonce-…'`,
  `style-src 'self' 'nonce-…'`, **no `'unsafe-inline'`** in production. HTML
  is deliberately uncacheable (`Cache-Control: private`) because it embeds a
  per-request nonce - a shared cache would hand a visitor a nonce their
  response's policy never allowed.
- **No HTML sinks**: no `innerHTML`, `outerHTML`, `insertAdjacentHTML`,
  `document.write`, or `eval` anywhere in client code. Trusted Types is
  supported (`trustedTypes: true` in `buildSecurityHeaders`) but gated off by
  default because Cloudflare's JavaScript Detections edge injection is
  incompatible with `require-trusted-types-for` - flipping it on requires
  dashboard changes first.
- **Transport/headers**: HSTS (preload), X-Content-Type-Options,
  X-Frame-Options, COOP/COEP/CORP, frame-ancestors, strict Permissions-Policy.
- **API**: Zod validation at every boundary (`packages/validation`), RFC 7807
  `application/problem+json` errors, RFC 9727 `.well-known/api-catalog`.
- **Secrets**: never in the repo. Wrangler secrets and GitHub repo secrets
  only. The `CLOUDFLARE_API_TOKEN` used by CI is a repo secret.
- **Telemetry**: browsers report CSP violations to `POST /csp-report` on
  `api.tone.rip`.

Known deviation from "perfect": `COEP` is `unsafe-none` (no cross-origin
isolation - deliberately, no feature needs it yet). `x-xss-protection` and
`X-Permitted-Cross-Domain-Policies` are **not** set - they are obsolete or
untested by security scanners; do not add them back without a documented
reason.

## Reporting a vulnerability

### Humans

Open a **private** report - either is fine:

1. **GitHub Security Advisory** (preferred): repo → Security → Report a
   vulnerability. Private until triaged.
2. **Email**: `m@tone.rip` (the address published in
   `/.well-known/security.txt`).

What to include:

- Affected URL or endpoint, and what you expected vs. what happened
- Reproduction steps (a failing request/curl is ideal)
- Browser/console errors verbatim - this site's failures can be
  production-only (edge-injected scripts; `wrangler dev` does not run them,
  see `docs/deployment.md`), so screenshots alone may not be enough
- Whether it reproduces on `https://tone.rip` (production), the dashboard
  (`dash.tone.rip`), or `api.tone.rip`

Expectations: this is a single-maintainer project. You will get an
acknowledgment within a week. If you hear nothing in two, email the address
above - the advisory route can bury things. Reports are triaged against the
security model above; duplicates of known Cloudflare-side behavior are closed
with a pointer to `docs/deployment.md`. There is **no bug bounty** - this is a
personal site.

### Agents (AI maintainers and triage tooling)

When a report arrives, process it in this order:

1. **Verify scope**: is the report about repo code or a Cloudflare dashboard
   setting? The latter goes to Cloudflare, close with the `docs/deployment.md`
   pointer.
2. **Check the model first**: grep for the sink in question
   (`innerHTML`, `eval`, `setAttribute("style", …)` - see the CSSOM/CSP table
   in `docs/engineering.md`; `style.setProperty` is allowed, `setAttribute`
   is not). Most "vulnerabilities" are policy behavior, not bugs.
3. **Reproduce locally**: `bunx turbo run test --filter=…`, `wrangler dev`.
   Remember the dev/prod CSP difference - dev serves `'unsafe-inline'`, so a
   clean dev run proves nothing about production.
4. **Check production, not just the code**: `curl -s -D - https://tone.rip/`
   and inspect the served headers; edge injection (Speed Brain, JS Detections)
   can only be seen there.
5. **Fix at the root, not the symptom**: one guard in the shared
   `packages/hono-middleware` function beats a guard in every caller. Validate
   at API boundaries, never deeper (`docs/engineering.md`).
6. **Leave one runnable check** for any non-trivial fix, run `bun run check-cycles`
   and `bun run knip`, and document anything dashboard-dependent in
   `docs/deployment.md` - the repo cannot assert zone settings.

Never commit secrets, tokens, or credentials under any circumstance
(`docs/engineering.md` → Git Guidelines).

## Disclosure

This site has a single maintainer and no users to notify. Fixed issues are
shipped silently on `main` (deploy is automatic). If the report came through
the GitHub Security Advisory, the advisory is closed on release; for email
reports, the reporter gets a one-line confirmation. No embargo policy - if
you disclosed publicly before the fix, say so in the report.
