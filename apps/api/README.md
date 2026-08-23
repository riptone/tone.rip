# api

The Hono API behind api.tone.rip, on Cloudflare Workers.

```bash
bun run dev
bun run test
bun run deploy
```

## Routes

- `GET /projects` - GitHub repos, cached at the edge with ETag revalidation and an in-memory fallback (`src/services/projects-cache.ts`)
- `GET /status` - self-hosted app health probes + Tailscale device status (`src/services/app-health.ts`)
- `POST /csp-report`, `GET /csp-report` - CSP violation report ingestion, Zod-validated
- `GET /info/:slug` - JSX-rendered (or markdown, via `Accept: text/markdown`) info pages for `tone` and `dashboard`, backed by `packages/content`'s `site-info.ts`
- `/api/auth/*` - Better Auth (email/password); mounted at this path since Better Auth's handler and ecosystem clients assume the `/api/auth` basePath by default
- `/.well-known/api-catalog` - RFC 9727 catalog of the above

Regenerate `CloudflareBindings` types after touching `wrangler.jsonc`:

```bash
bun run cf-typegen
```
