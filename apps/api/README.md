# api

The Hono API behind api.tone.rip, on Cloudflare Workers.

```bash
bun run dev
bun run test
bun run deploy
```

## Routes

- `GET /apps` - the self-hosted applications, from the Cloudflare Access Applications API (`src/services/access-apps.ts`). Requires an Access JWT, like `/status`: the list is a map of what is hosted where, and serving it publicly would be enumeration.
- `GET /projects`, `GET /projects/{repo}/readme` - GitHub repos, cached at the edge with ETag revalidation and an in-memory fallback (`src/services/projects-cache.ts`)
- `GET /status` - self-hosted app health probes + Tailscale device status (`src/services/app-health.ts`). Requires an Access JWT, forwarded server-to-server by `apps/dashboard`.
- `POST /csp-report`, `GET /csp-report` - CSP violation report ingestion, Zod-validated
- `GET /info/:slug` - JSX-rendered (or markdown, via `Accept: text/markdown`) info pages for `tone` and `dashboard`, backed by `packages/content`'s `site-info.ts`
- `POST /ssh/authorize` - resolves an SSH key fingerprint to its scopes for `apps/ssh-cv`. Deliberately absent from `/.well-known/api-catalog` and `GET /`: one client, configured with the URL, and advertising it is free reconnaissance.
- `/.well-known/api-catalog` - RFC 9727 catalog of the public ones

`apps/dashboard` reaches `/apps` and `/status` through a Cloudflare service binding rather than over the public internet - see its `wrangler.jsonc` and `src/lib/api.ts`.

## Types

`worker-configuration.d.ts` is generated, not committed - turbo's `types` task
runs `wrangler types` whenever `wrangler.jsonc` changes. To regenerate by hand:

```bash
bun run types
```

`src/env.ts` deliberately does not extend the generated `Env`; the comment at
the top of that file explains why (short version: `wrangler types` reads
`.dev.vars`, which is gitignored, so the generated shape differs by machine).
