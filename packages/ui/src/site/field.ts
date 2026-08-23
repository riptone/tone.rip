/* One gradient field per document, and one implementation across apps.

   Each route declares its palette on `<body data-ramp>` and this reads it;
   nothing else about the field varies between pages, which is deliberate.
   It is the one element both properties share, and a per-page knob would be
   an invitation for them to drift.

   The handle lives at module scope and mounting is idempotent. Under
   `<ClientRouter />` that was load-bearing - the element survived a
   navigation via `transition:persist` while the scripts re-ran, so a second
   mount would have put a second worker on a canvas the first already owned.
   With cross-document view transitions each navigation brings a new document
   and therefore a new module instance, so the mount branch is now the one
   that runs and continuity is the browser's job: shell.css names the field
   so the old and new frames cross-fade into each other. The guard stays
   because "call this whenever the ramp might have changed" is the function's
   contract, and a contract that only holds for the first call is a trap. */

import {
  mountNoiseGradient,
  type NoiseGradientHandle,
  RAMPS,
  type RampId,
  rowsToCss,
} from "../gradient/index.js";
import { subscribeScrollProgress } from "../motion/scroll-progress.js";

let handle: NoiseGradientHandle | null = null;

/**
 * Paint the wordmark's trailing cursor cell from the frame's colour bands.
 *
 * The cursor is the one cell in the logo that isn't a letter, so it is also
 * the one that carries the field's colour - the same relationship the
 * accent text has, expressed in SVG. CSS `background-clip` cannot do this on
 * a shape, so the wordmark ships a <linearGradient> whose stops are written
 * here.
 *
 * The stop list is cached: it is the same 25 nodes every frame, and querying
 * for them 30 times a second would be the most expensive thing on the page.
 */
let cursorStops: SVGStopElement[] | null = null;

function paintCursor(profile: Int16Array): void {
  if (!cursorStops) {
    cursorStops = Array.from(
      document.querySelectorAll<SVGStopElement>("#tw-ramp stop"),
    );
    if (cursorStops.length === 0) return;
  }
  for (const [i, stop] of cursorStops.entries()) {
    const r = profile[i * 3] ?? 255;
    const g = profile[i * 3 + 1] ?? 255;
    const b = profile[i * 3 + 2] ?? 255;
    stop.setAttribute("stop-color", `rgb(${r} ${g} ${b})`);
  }
}

function currentRamp(): RampId {
  const declared = document.body.dataset.ramp;
  return declared && declared in RAMPS ? (declared as RampId) : "moss";
}

/**
 * Mount the field if it is not already running, then apply this page's ramp.
 *
 * The only thing that varies between pages is the palette, and that comes
 * from `<body data-ramp>` on every call. Everything else is the same field
 * everywhere on purpose: it is the one element both properties share, and a
 * per-page knob would be an invitation for them to drift.
 */
export function syncField(): void {
  const host = document.querySelector<HTMLElement>("[data-gradient-panel]");
  if (!host) return;

  if (!handle) {
    handle = mountNoiseGradient(host, {
      ramp: currentRamp(),
      onFrame: ({ profile }) => {
        // The accent text is filled from the colours on screen this frame, so
        // it tracks the palette change for free.
        //
        // `style.setProperty`, never `setAttribute("style", …)`. The two look
        // interchangeable and are not: CSP governs the style *attribute* and
        // does not govern the CSSOM, so under this site's production policy
        // (`style-src 'self' 'nonce-…'`, no unsafe-inline) the second form is
        // dropped silently. Local dev serves 'unsafe-inline', so the swap
        // would pass every test you could run here and only break once
        // deployed. See docs/engineering.md.
        document.documentElement.style.setProperty(
          "--field-ramp",
          rowsToCss(profile),
        );
        paintCursor(profile);
      },
    });
    // Never unsubscribed on purpose: the field lives as long as the document
    // does, so the subscription should too, and the document is torn down
    // wholesale on navigation.
    subscribeScrollProgress(({ progress }) => handle?.update({ progress }));
    return;
  }

  // Already running, so this is a re-call within one document. Any wordmark
  // this cache was pointing at may have been replaced since; drop it and let
  // the next frame re-resolve.
  cursorStops = null;
  handle.update({ ramp: currentRamp() });
}
