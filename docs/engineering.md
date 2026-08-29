# Engineering standards

Technical conventions for this monorepo. For the visual/brand philosophy, see [constitution.md](./constitution.md). For how the apps and packages fit together, see [architecture.md](./architecture.md). For deploying to Cloudflare, see [deployment.md](./deployment.md).

---

# Before opening a pull request

Run the gate CI runs. It is one command, and it is the same command on a
laptop and on a runner — there is no shorter subset that CI will accept:

```bash
bun run ci
```

Which is, in order: Biome format, Biome lint, type-check (`astro check`,
`tsc`, `gofmt` + `go vet`), tests (Vitest everywhere, `go test` for
`apps/ssh-cv`), build, Worker bundle sizes, Playwright end-to-end, `knip` for
unused files/exports/dependencies, `madge` for import cycles, `shellcheck` on
the SSH CV's installer, `govulncheck` on the Go module, and finally a check
that the CV embedded in the Go binary has not drifted from
`packages/content`.

Two of those fail in ways worth recognising:

- **The drift check** (`git diff --exit-code -- apps/ssh-cv/internal/cv/cv.json`)
  fails when the generated CV is regenerated but not committed. It is not a
  test failure; it is telling you to `git add` the generated file. That file
  is committed on purpose, so a plain `go build` works in a checkout with no
  Bun.
- **Playwright** needs its browser once per machine:
  `cd apps/web && bun run playwright install --with-deps chromium`.

---

# Performance

Optimize by default.

Avoid unnecessary:

- Dependencies
- Re-renders
- Animations
- Network requests
- Abstractions

Measure before optimizing.

---

# Language

Use:

- TypeScript only
- Strict mode enabled

Avoid:

any

Prefer:

- Explicit types
- Composition
- Small focused files
- Small components

---

# Validation

Use:

- Zod (`packages/validation`)

Never trust:

- Client input
- External data
- API payloads

Validate at system boundaries - `apps/api`'s routes, not deeper.

---

# Content Security Policy

Production serves `style-src 'self' 'nonce-…'` and `script-src 'self'
'nonce-…'` with no `'unsafe-inline'` (`packages/hono-middleware/src/core.ts`).
**Local dev serves `'unsafe-inline'` instead**, so anything CSP would reject
works fine on `localhost` and fails only once deployed. Assume nothing about
inline styles from having seen a page work in dev.

The distinction that matters, and it is not obvious:

| | under `style-src 'self'` |
| --- | --- |
| `el.style.setProperty(…)`, `el.style.color = …` | **allowed** - CSP does not govern the CSSOM |
| `el.setAttribute("style", …)` | **blocked**, silently, no error |
| `style="…"` written in markup | **blocked** |

So the gradient field writing `--field-ramp` to `documentElement.style` every
frame (`packages/ui/src/site/field.ts`) is fine, even though DevTools shows
the result as a `style` attribute on `<html>` - that attribute is the CSSOM's
serialisation, not something the parser was asked to accept. Rewriting that
line as `setAttribute("style", …)` would look equivalent, pass in dev, and
kill the effect in production with nothing in the console. Verified against a
real strict-CSP document, not inferred.

The same applies to `<style nonce>` blocks: `BaseHead.astro` inlines the
page's CSS with the request nonce, which is why every page passes its
stylesheet down as a string rather than importing it into a `<style>` Astro
would emit unnonced. `apps/web/src/middleware.ts` stamps the nonce onto
anything Astro inlines that the templates did not, as a backstop.

## Per-request nonces and soft navigation do not mix

`apps/web` used Astro's `<ClientRouter />` and does not any more. The reason is
structural rather than a bug anyone could fix:

ClientRouter navigates by fetching the next page and parsing it with
`DOMParser`. **A document created that way inherits the creating document's
CSP**, so the incoming response's `<style nonce>` tags are judged against the
nonce minted for the *previous* response, and never match. Two inline
`style-src` violations per navigation, in production only - and the page then
rendered perfectly, because the violation happens at parse time on a document
that is never inserted.

Things that do not fix it, all tried:

- Restamping the parsed document's nonces before the swap. Measured: the
  violation count stayed at exactly 2, because the block already happened
  during `parseFromString`.
- Astro's `security.csp`. Its own documentation says ClientRouter is
  unsupported, for this reason.
- A stable nonce across responses. That is not a nonce.

The fix was to stop having two documents: `@view-transition { navigation:
auto }` (`packages/ui/src/styles/view-transition.css`) gets the same animated
navigation from the browser, on a real page load, where the policy in force is
the one that arrived with the page.

**So: with a per-request nonce, prefer browser-native cross-document view
transitions over any router that fetches and parses HTML itself.** The
constraint applies to anything with that shape, not just ClientRouter.

---

# Database

No relational database beyond Cloudflare D1's use as Better Auth's storage, for now - no Neon/Postgres, no ORM. Revisit only when there's an actual feature that needs one; don't add one speculatively.

---

# API

Use:

- Hono (`apps/api`)
- RFC 7807 (`application/problem+json`) for every error response - see `packages/hono-middleware`'s `problemJson()`
- RFC 9727 (`/.well-known/api-catalog`) to advertise routes

Prefer REST conventions.

Examples:

```
GET /projects
GET /status
POST /csp-report
GET /info/:slug
```

Routes should be predictable and consistent.

---

# Project Structure

Preferred:

```
apps/
packages/
```

Shared logic belongs inside packages. Never duplicate business logic - if the same thing needs doing in `apps/web` and `apps/dashboard` (or any other pair), it belongs in a package, not copy-pasted.

---

# Git Guidelines

Commits should be:

- Small
- Focused
- Meaningful

Never commit:

- Secrets
- API keys
- Environment credentials

---

# Decision Framework

When uncertain:

Prefer:

- Less UI
- Less complexity
- Fewer dependencies
- Clearer interactions
- Better defaults

Avoid:

- Feature creep
- Decorative complexity
- Trend-driven design
