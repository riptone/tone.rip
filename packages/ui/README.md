# @repo/ui

Shared design system for `apps/web` and `apps/dashboard`: design tokens, theming, a small vanilla-DOM component kit, and the shared `<head>` component. Nothing here is Astro-only - the components are plain functions returning `HTMLElement`s, so any app in this monorepo can use them regardless of how much Astro/React/etc it wraps around them.

## Importing from this package

`package.json`'s `exports` map has a wildcard fallback (`"./*": "./src/*"`) that covers any import specifier which already includes its own file extension - `@repo/ui/BaseHead.astro`, `@repo/ui/styles/tokens.css?inline`. Extension-less `.ts` modules (`@repo/ui/dom`, not `@repo/ui/dom.ts`) need their own explicit entry instead, because TypeScript's `node16`/`nodenext` module resolution (which this package's `tsconfig.json` uses) requires relative imports inside the package to carry an explicit `.js` extension, and the wildcard can't infer one for a bare specifier - see the `"./dom"`/`"./components"`/`"./storage"`/`"./theme-bootstrap"` entries for the pattern. Examples of what exists today:

```ts
import BaseHead from "@repo/ui/BaseHead.astro";
import { btnLink, chips, codeBlock, panelHead, tag } from "@repo/ui/components";
import { h, clear } from "@repo/ui/dom";
import { readStored, writeStored } from "@repo/ui/storage";
import type { ToneThemeHelpers, Theme } from "@repo/ui/theme-bootstrap";

import resetCss from "@repo/ui/styles/reset.css?inline";   // inlined under a CSP nonce (apps/web's pattern)
import tokensCss from "@repo/ui/styles/tokens.css?inline";
import componentsCss from "@repo/ui/styles/components.css?inline";
import "@repo/ui/styles/tokens.css";                        // or linked as a real stylesheet (apps/dashboard's pattern)
```

## What's here

### `BaseHead.astro`

The shared `<head>`: favicon/meta tags, canonical/hreflang, OG/Twitter tags, JSON-LD (merges a default `WebSite` schema with a per-page `schema` prop), and two inline nonce'd scripts - a pre-paint theme bootstrap (reads `localStorage.theme`, sets `documentElement.dataset.theme` before first paint to avoid a flash) and the `window.tone` theme helpers described below. See the `Props` interface at the top of the file for the full prop list.

Every consumer needs `Astro.locals.cspNonce` typed in its own `env.d.ts` - see the comment in `src/env.d.ts` for why that can't be inherited across a package boundary.

### Theming (`styles/tokens.css`, `styles/reset.css`, `theme-bootstrap.ts`)

- `tokens.css` - `@font-face` declarations + every design token as a CSS custom property (`--bg`, `--text*`, `--accent*`, `--font-*`, spacing/radius/motion scales, …), with a `html[data-theme="light"]` block overriding the semantic tokens for light mode. Dark is the implicit default.
- `reset.css` - box-sizing, focus rings, `.sr-only`, and other app-agnostic base styles. Deliberately does **not** set `body { overflow: hidden }` - that's a layout choice specific to apps/web's locked-viewport "desktop" page, so it stays in `apps/web/src/styles/desktop/base.css`.
- `theme-bootstrap.ts` - just the `Theme`/`ToneThemeHelpers` types for the `window.tone` object `BaseHead.astro`'s inline script installs at runtime (`getStoredTheme`, `applyTheme`, `readTheme`, `syncTheme`, `setStoredTheme`, plus a `tone:themechange` event for cross-widget sync). Each app's own theme-toggle script (`apps/web/src/scripts/desktop/theme.ts`, `apps/dashboard/src/scripts/theme.ts`) types against this instead of hand-declaring its own copy.

### Design-system primitives (`components.ts` + `styles/components.css`)

Vanilla-DOM component builders - no framework, no virtual DOM, just `HTMLElement` factories built on top of `dom.ts`'s tiny `h()` hyperscript helper. Class names (`vire-btn`, `vire-tag`, `vire-code*`) are paired 1:1 with the CSS in `styles/components.css`.

| Export | Renders |
|---|---|
| `tag(text, tone?)` | `<span class="vire-tag">` (or `vire-tag--accent`) |
| `btnLink(text, href, primary?)` | `<a class="vire-btn">` styled as a button, opens in a new tab |
| `chips(items, tone?)` | a `<div class="vp__chips">` of `tag()`s |
| `codeBlock(filename, code)` | a `<div class="vire-code">` with a line-numbered gutter, mirroring a small code-editor chrome |
| `panelHead(eyebrow, title, src?)` | a `<header class="vp__head">` with an eyebrow line, a title, and an optional trailing element |
| `openExternal(href)` | safely `window.open`s an `http(s)` URL in a new tab, no-ops otherwise |

`dom.ts` (`h(tag, attrs, ...children)`, `clear(node)`) and `storage.ts` (`readStored`/`writeStored`, a try/catch-guarded `localStorage` wrapper) are the two small utilities these primitives - and any future ones - are built on.

## Adding a new shared component

1. Ask: does this genuinely need to render the same way in more than one app? If it's specific to one app's content or layout, it belongs in that app instead (see `docs/architecture.md`'s "does this go in a package?" rule of thumb).
2. Drop the function in `src/<name>.ts` (or a new file), export it, and add its CSS (if any) to `styles/<name>.css` - or a new stylesheet if it's a big enough addition to warrant one. Any relative import between two `src/*.ts` modules needs an explicit `.js` extension (e.g. `import { h } from "./dom.js"`) - that's `node16`/`nodenext` module resolution, not a typo.
3. Add a `"./<name>": "./src/<name>.ts"` entry to `package.json`'s `exports` map (the wildcard alone won't resolve an extension-less specifier - see above) and document it in the table above.
4. Add a Vitest unit test in `test/<name>.test.ts` (see `test/dom.test.ts`/`test/components.test.ts` for the pattern - this package runs its tests under `jsdom`, see `vitest.config.ts`).
5. Update whichever app(s) should adopt it to import from `@repo/ui/<name>` instead of a local copy - don't let the same component exist in two places.


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

`apps/web/src/pages/v2/` is the sandbox for tuning all of it, with a live
frames-per-second readout so idle cost is visible. It is noindex'd and
excluded from the sitemap.

### CSP

The worker is a same-origin module chunk, so `worker-src 'self'` covers it
(set in `packages/hono-middleware/src/core.ts`). The grain tile is a `data:`
URL, already allowed by `img-src`.
