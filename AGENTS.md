## Project Constitution

This document defines where to find the philosophy, design language, and engineering standards for this project. All agents, contributors, and future development tools should read the relevant doc below before creating, modifying, or reviewing code.

The goal is not just to build features. The goal is to preserve a consistent product identity.

- **[docs/constitution.md](./docs/constitution.md)** - brand personality, visual system, motion, typography, accessibility, the "why does this exist?" component test. Read this before touching `apps/web` or `apps/dashboard`'s UI.
- **[docs/engineering.md](./docs/engineering.md)** - language/validation/API/git conventions. Read this before writing any code.
- **[docs/architecture.md](./docs/architecture.md)** - how the apps and packages fit together, and why things live where they do. Read this before deciding whether something belongs in a package or an app.
- **[docs/deployment.md](./docs/deployment.md)** - the Cloudflare Workers cutover runbook.

## Quick facts (so you don't have to open the docs above just to know these)

- Stack: Bun + Turborepo, Astro (`apps/web`, `apps/dashboard`), Hono on Cloudflare Workers (`apps/api`), Biome (not ESLint), Vitest everywhere. `apps/dashboard` is gated by Cloudflare Access, not app-level auth code. No Neon/Postgres/Drizzle, no mobile app - out of scope until there's an actual need.
- No god-files, no circular dependencies. Run `bun run check-cycles` (madge) and `bun run knip` before considering a change done.
- `bun run ci` runs the whole gate locally - one Biome pass (`biome check --write`), then turbo schedules check-types, test, build, knip, check-cycles, check-bundle-size and e2e in parallel, then the generated-CV drift check. The one command to run before considering a change done. `shellcheck` and `govulncheck` are CI-only.
- `bun run test` enforces per-package coverage thresholds (each `vitest.config.ts` passes them to `defineTestConfig` from `packages/config/vitest-config`), `bun run check-bundle-size` guards each Worker's bundled-script size, and `bun run e2e` (Playwright, `packages/config/playwright-config`) smoke-tests `apps/web`/`apps/dashboard` against a real `wrangler dev` - all three run in CI; see [docs/deployment.md](./docs/deployment.md) for the bundle-size budget.
- `bun outdated` (aliased as `bun run outdated`) lists outdated dependencies natively - no need for `npm-check-updates` or similar.
- Shared logic belongs in a package (see architecture.md's package map) - never duplicate business logic between `apps/web` and `apps/dashboard`.

## Final Principle

The best interface is the one users stop noticing. The product should feel effortless, intentional, calm, and crafted. Every decision should move the product closer to that feeling.
