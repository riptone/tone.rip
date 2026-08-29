# Engineering standards

Technical conventions for this monorepo. For the visual/brand philosophy, see [constitution.md](./constitution.md). For how the apps and packages fit together, see [architecture.md](./architecture.md). For deploying to Cloudflare, see [deployment.md](./deployment.md).

---

# Before opening a pull request

Run the gate CI runs. It is one command, and it is the same command on a
laptop and on a runner — there is no shorter subset that CI will accept:

```bash
bun run ci
```

Which is: one Biome pass that formats and lints (`biome check --write`), then
everything turbo can schedule — type-check (`astro check`, `tsc`, `gofmt` +
`go vet`), tests (Vitest everywhere, `go test` for `apps/ssh-cv`), build,
`knip` for unused files/exports/dependencies, `madge` for import cycles,
Worker bundle sizes, Playwright end-to-end — and finally a check that the CV
embedded in the Go binary has not drifted from `packages/content`.

Not in order, which is the point: turbo runs what is independent in parallel
and skips what has not changed. The four repo-wide checks are declared in
`turbo.json` as root tasks (`//#lint`, `//#knip`, `//#check-cycles`,
`//#check-bundle-size`) with the inputs each actually reads, so editing a
stylesheet does not re-run `knip` and editing a `.md` re-runs nothing at all.

`shellcheck` and `govulncheck` are CI-only: both are on the runner image, and
neither is worth making a prerequisite of a laptop checkout. See
`.github/workflows/ci.yml`, which splits the gate across three parallel jobs
by which toolchain each one needs.

Two of those fail in ways worth recognising:

- **The drift check** (`git diff --exit-code -- apps/ssh-cv/internal/cv/cv.json`)
  fails when the generated CV is regenerated but not committed. It is not a
  test failure; it is telling you to `git add` the generated file. That file
  is committed on purpose, so a plain `go build` works in a checkout with no
  Bun.
- **Playwright** needs its browser once per machine:
  `cd apps/web && bun run playwright install --with-deps chromium`.

---

# Comments in Astro templates

Use `{/* … */}` below the frontmatter fence, never `<!-- … -->`.

Both read the same in source and only one of them stays there. Astro strips a
JSX-expression comment at build time and emits an HTML comment verbatim, so
every `<!-- -->` in a template is shipped to every visitor on every request -
and with cross-document navigation, on every navigation.

This was not theoretical. Thirteen explanatory blocks across `BaseHead.astro`,
`SiteLayout.astro`, `NotFound.astro`, `Reveal.astro` and one page had been
going out with the markup: 4,560 bytes on the home page, 2,468 on the
dashboard. Converting them to `{/* */}` took the home page from 10,000 to
8,035 bytes gzipped (-19.6%) and the dashboard from 4,136 to 2,922 (-29.4%).

For scale, the whole inlined stylesheet on that page is 4,289 bytes gzipped -
a cost this repo argued itself into paying deliberately (see BaseHead.astro).
The comments were half that again, bought nothing, and nobody had counted them.

Above the fence, in the frontmatter, ordinary `//` and `/* */` comments never
reach the output and need no thought.

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
