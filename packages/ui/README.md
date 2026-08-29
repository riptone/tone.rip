# @repo/ui

Shared design system for `apps/web` and `apps/dashboard`: design tokens, theming, a small vanilla-DOM component kit, and the shared `<head>` component. Nothing here is Astro-only - the components are plain functions returning `HTMLElement`s, so any app in this monorepo can use them regardless of how much Astro/React/etc it wraps around them.

## Importing from this package

`package.json`'s `exports` map lists every public entry point by name. It used
to be a single wildcard (`"./*": "./src/*"`), which is why this section could
go years describing modules that had been deleted: a wildcard resolves
anything under `src/`, so nothing - not a build, not `knip` - could tell the
difference between a module two apps depend on and one nobody had imported
since it was written. Naming them made the boundary checkable, and knip
immediately found four exports with no callers.

So: a new shared module is not importable until it is added to that map, and
that is the point. Files without an entry are internal to the package
(`fonts.ts`, `storage.ts`, `trusted-types.ts`, `site/Reveal.astro`, and the
gradient's worker), reachable only by a relative import from a sibling.

```ts
import BaseHead from "@repo/ui/BaseHead.astro";
import Logo from "@repo/ui/brand/Logo.astro";
import Footer from "@repo/ui/site/Footer.astro";
import ContextMenu from "@repo/ui/site/ContextMenu.astro";
import { mountContact, mountContextMenu, mountFilter, mountLang, syncField } from "@repo/ui/site";
import { mountNoiseGradient, rowsToCss } from "@repo/ui/gradient";
import { subscribeScrollProgress } from "@repo/ui/motion/scroll-progress";

import tokensCss from "@repo/ui/styles/tokens.css?inline";  // inlined under a CSP nonce (apps/web's pattern)
import "@repo/ui/styles/tokens.css";                        // or linked as a real stylesheet (apps/dashboard's pattern)
```

Both forms resolve against the same entry - Vite strips the `?inline` query
before consulting `exports` - so a stylesheet needs one entry, not two.

Relative imports *inside* this package need an explicit `.js` extension
(`import { readStored } from "../storage.js"`). That is `node16`/`nodenext`
module resolution, not a typo.

## What's here

### `BaseHead.astro`

The shared `<head>`: favicon/meta tags, canonical, OG/Twitter tags, JSON-LD
(merges a default `WebSite` schema with a per-page `schema` prop), the font
preload, and the page's stylesheet inlined under a CSP nonce. Every page in
both apps is dark-only, so `theme` is a required prop stating which one the
page is rather than a preference read at runtime - see the `Props` interface
at the top of the file, which carries the reasoning for that and for the
inline-CSS trade-off (measured, not assumed).

Every consumer needs `Astro.locals.cspNonce` typed in its own `env.d.ts` - see
the comment in `src/env.d.ts` for why that cannot be inherited across a
package boundary.

### `site/` - the chrome both properties share

`Footer.astro`, `ContextMenu.astro`, `T.astro` and `Reveal.astro` on the Astro
side; `site/index.ts` exports the controllers that mount to them
(`mountContact`, `mountContextMenu`, `mountFilter`, `mountLang`, `syncField`).
The split is deliberate: the markup is server-rendered so it exists for
crawlers and for a visitor with no JavaScript, and the controller only adds
behaviour to what is already on the page.

### `styles/` - the frame

`tokens.css` (the `@font-face` declarations and every design token as a custom
property), `reset.css`, `shell.css` (the reading column and the field beside
it), the 404, and the look of anything appearing in both apps: the menu
surface (`context-menu.css`, `filter.css`, sharing `--menu-*` tokens) and the
cross-document view-transition at-rule.

### `site/NotFound.astro`

The whole 404 page - `<head>`, markup, stylesheet and field - as one
component, because `styles/not-found.css` already lived here and the markup
did not. Each app re-exports it from `src/pages/404.astro`, which is the only
path Astro wires to the adapter's not-found handler, and passes the four things
that differ: title, description, `robots`, and the ramp. It sets
`Astro.response.status = 404` itself, so neither app can render a not-found
page that answers 200.

### `contrast.ts`

WCAG 2.1 relative contrast, plus `flatten()` for compositing an `rgba()` ink
onto a background and `parseHex()`. Exported rather than kept beside its test
because two stylesheets now need the guard: this package's text tokens, and
`apps/dashboard`'s four status colours, which live in hex outside the token
layer. Two copies of the sRGB luminance formula is two chances to get it
subtly wrong in one of them - and the wrong one would be the one that passes.

### `brand/` and `scripts/`

`src/brand/` holds `Logo.astro` and the `mark.svg`/`wordmark.svg` it imports.
The tools that draw them live in `scripts/`, outside the library surface:

```bash
cd packages/ui && bun run brand
```

`scripts/generate-brand.py` draws the wordmark, the mark and `favicon.svg` from
pure geometry - no font dependency, no renderer. `scripts/rasterize-icons.ts`
then rasterises that SVG into `public/icons/*.png` and packs `favicon.ico`,
using the Chromium this repo already installs for its end-to-end tests.

The second half used to be a comment saying to regenerate the rasters by hand
"when the mark changes". That is how the mark became a headstone in August and
every raster icon on both properties stayed the old backslash for a month.

## Adding a new shared component

1. Ask whether it genuinely needs to render the same way in more than one app.
   If it is specific to one app's content or layout it belongs in that app -
   see `docs/architecture.md`'s "does this go in a package?" rule of thumb.
2. Put it in `src/site/` (chrome) or `src/<area>/`, and its CSS in
   `src/styles/`.
3. Add an `exports` entry for it in `package.json`. Without one it is
   internal, which is the right answer for a helper only its siblings use.
4. Add a Vitest unit test in `test/` - this package runs under `jsdom`, see
   `vitest.config.ts` and the shared preset it calls.
5. Update the app(s) that should adopt it, and delete the local copy. The
   same component existing in two places is the thing this package is for.

## `@repo/ui/gradient` - the noise gradient field

A soft, grainy colour field that responds to scroll. Framework-free: it mounts
into a plain element, so both Astro apps can use it without either adopting a
component framework.

```ts
import { mountNoiseGradient, rowsToCss } from "@repo/ui/gradient";
import { subscribeScrollProgress } from "@repo/ui/motion/scroll-progress";

const field = mountNoiseGradient(document.querySelector("#panel"), {
  ramp: "moss",                // a ramp id, or a Ramp
  onFrame: ({ profile }) => {
    // Fill an accent from the colours on screen right now.
    document.documentElement.style.setProperty("--field-ramp", rowsToCss(profile));
  },
});

subscribeScrollProgress(({ progress }) => field.update({ progress }));
```

Import `src/styles/gradient.css` alongside it.

### How it is put together

Five files, each with one job:

| file | job |
| --- | --- |
| `gradient/field.ts` | noise, the per-pixel render, and the worker message contract - pure, no browser APIs |
| `gradient/field-worker.ts` | owns the `OffscreenCanvas`, caps frames at 30fps, ships `ImageBitmap`s |
| `gradient/noise-gradient.ts` | mounts into an element: canvas, grain layer, observers, upscale |
| `gradient/oklab.ts` | sRGB ↔ OKLab/OKLCh with gamut mapping |
| `gradient/ramps.ts` | palettes and sampling |

Noise, the field render and the protocol used to be three files. They are one
thing viewed from three angles - the noise exists only to displace the field,
and the worker message is the field's parameters plus what it takes to ship a
frame - so they now live together. Colour and DOM plumbing stay separate
because they are genuinely independent and separately testable.

Two decisions carry most of the look:

- **The upscale is the softness.** The field renders at `renderScale` (0.25)
  and is stretched back up with `imageSmoothingQuality: "low"`. There is no
  blur filter anywhere; a higher-quality resample would sharpen it back.
- **Grain is a DOM layer, not canvas pixels.** A 200px tile at one noise pixel
  per *device* pixel, `mix-blend-mode: overlay`. In-canvas grain would be
  resampled to mush by that same upscale.

### Palettes

`RAMPS` holds two families:

- **Duotones** (`DUOTONES`) - `tealBlush`, `moss`, `dusk`, `glacier`, `rust`,
  `quiet`. Each crosses a real hue distance rather than shading one colour.
  Build your own with `duotoneRamp(from, to)`.
- **Signature-derived** - one per accent in `SIGNATURE_ACCENTS`, via
  `signatureRamp(accent)`, which fans a single colour across ~60° of hue.

Both put stops on the same absolute OKLab lightness ladder, so switching
palette changes hue and not how heavy the field feels - and both gamut-map
rather than clip, because clipping an out-of-gamut blue silently moves its
lightness *and* its hue.

`duotoneRamp` walks hue the **short way** round the circle. Teal to blush
through magenta is a gradient; the same pair interpolated in sRGB passes
through the desaturated middle, which is the flat, slightly grey look most
generated gradients have.

### Two things worth knowing before you tune it

**`wave` defaults to 0, and should stay there.** Any non-zero value renders 30
frames a second for as long as the element is on screen, whether or not
anything is happening - a warm phone and a flat battery bought for motion
nobody consciously notices. At 0 the field renders only when `progress`
changes, and an untouched page costs nothing.

**The scroll loop is not always-on.** It runs only while there is a gap left
to close, then lets itself die; the passive scroll listener restarts it. A
permanent rAF would keep the compositor - and the gradient worker downstream
of it - awake on a page nobody is touching.

There is no longer a tuning sandbox route - `apps/web/src/pages/v2/` became
the site itself. Tune against a real page and watch the worker in DevTools'
performance panel; the cost that matters is what an idle page spends, which
should be nothing.

### CSP

The worker is a same-origin module chunk, so `worker-src 'self'` covers it
(set in `packages/hono-middleware/src/core.ts`). The grain tile is a `data:`
URL, already allowed by `img-src`.
