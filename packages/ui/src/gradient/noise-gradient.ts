/* Mounts a noise gradient field into an element.

   Composition, top to bottom:

     host          position: relative, clips its children
     ├─ canvas     the field, rendered small in a worker and scaled up
     └─ grain div  a tiled noise texture at mix-blend-mode: overlay

   The two halves are separate on purpose. The field is smooth, low
   frequency and expensive, so it renders at a quarter size and is stretched
   back - the browser's bilinear upscale is what makes it soft, and it costs
   nothing. The grain is the opposite: it must be pixel-sharp at device
   resolution or it turns to mush, so it is a static tile the compositor
   repeats, generated once and never touched again.

   Trying to do both in the canvas gets you a blurry grain; trying to do
   both in CSS gets you a gradient that bands. */

import { installDefaultTrustedTypesPolicy } from "../trusted-types.js";
import type { FieldState, HostToWorker, WorkerToHost } from "./field.js";
import { renderGrainTile } from "./field.js";
import { RAMPS, type Ramp, type RampId, serializeRamp } from "./ramps.js";

/** Grain tile edge in device pixels. 200 is large enough not to read as a repeat. */
const GRAIN_SIZE = 200;
/** Match the worker's cap so the host never queues state faster than frames come back. */
const MIN_STATE_INTERVAL_MS = 1000 / 30;

export type FeatherEdge = "left" | "right" | "top" | "bottom";

export interface NoiseGradientOptions {
  /** Ramp to render, or the id of a ready-made one (see RAMPS). */
  ramp?: Ramp | RampId;
  /** Noise cells across the field. */
  frequency?: number;
  /** How far noise displaces a pixel along the ramp, 0–1. */
  warp?: number;
  /**
   * Autonomous drift, in noise units per second.
   *
   * Defaults to 0, and think hard before raising it: any non-zero value means
   * the worker renders 30 frames a second for as long as the element is on
   * screen, whether or not anything is happening. That is a real, permanent
   * cost - a busy CPU, a warm phone, a flat battery - bought for motion most
   * people will never consciously notice. At 0 the field renders only when
   * `progress` changes, so an untouched page costs nothing.
   */
  wave?: number;
  /** Fraction of the ramp a full 0→1 scroll walks through. */
  travel?: number;
  /** Gradient direction in degrees; 90 is top-to-bottom. */
  angle?: number;
  /** Wrap rather than clamp the ramp. Only sensible for ramps whose ends match. */
  loop?: boolean;
  /** Fraction of the display size actually rendered, 0.1–1. Lower is softer and cheaper. */
  renderScale?: number;
  /** Opacity of the grain overlay, 0–1. 0 omits it entirely. */
  grainAlpha?: number;
  /** Peak grain deviation from mid-grey, 0–127. */
  grainDepth?: number;
  /** Fraction of the box over which one edge fades to transparent. */
  feather?: number;
  featherFrom?: FeatherEdge;
  /** Initial progress; drive it afterwards with `update({ progress })`. */
  progress?: number;
  /**
   * Called after each painted frame with the frame's averaged colour bands.
   *
   * This is the hook that keeps the rest of the page in step with the field:
   * pass the profile through `rowsToCss` and you have a gradient string
   * matching what is on screen, ready to fill an accent word or a rule.
   */
  onFrame?: (frame: { progress: number; profile: Int16Array }) => void;
}

export interface NoiseGradientHandle {
  /** Merge new options in and re-render. Cheap; call it as often as you like. */
  update(options: Partial<NoiseGradientOptions>): void;
  destroy(): void;
}

// Tuned by eye against real type, not derived from anything.
// Notably: more grain at lower depth than the reference site uses (0.25/50 vs
// 0.16/80), which reads as paper rather than as noise.
const DEFAULTS = {
  ramp: "moss" as Ramp | RampId,
  frequency: 2,
  warp: 0.3,
  wave: 0,
  travel: 1,
  angle: 90,
  loop: false,
  renderScale: 0.25,
  grainAlpha: 0.25,
  grainDepth: 50,
  feather: 0,
  featherFrom: "left" as FeatherEdge,
  progress: 0,
};

function resolveRamp(ramp: Ramp | RampId): Ramp {
  return typeof ramp === "string" ? RAMPS[ramp] : ramp;
}

/** Render the grain tile once and hand back a data URL for CSS to repeat. */
function makeGrainUrl(depth: number): string | null {
  const tile = document.createElement("canvas");
  tile.width = GRAIN_SIZE;
  tile.height = GRAIN_SIZE;
  const context = tile.getContext("2d");
  if (!context) return null;
  const image = context.createImageData(GRAIN_SIZE, GRAIN_SIZE);
  renderGrainTile(image.data, GRAIN_SIZE, depth);
  context.putImageData(image, 0, 0);
  return tile.toDataURL();
}

/**
 * Fade one edge of the painted field to transparent.
 *
 * `destination-out` erases what is already drawn, so this is a real alpha
 * ramp in the canvas rather than a gradient overlay on top - which matters
 * because whatever is behind the element shows through, and a black overlay
 * would only look right on a black page.
 */
function featherEdge(
  context: CanvasRenderingContext2D,
  width: number,
  height: number,
  amount: number,
  from: FeatherEdge,
): void {
  const horizontal = from === "left" || from === "right";
  const span = Math.max(1, (horizontal ? width : height) * amount);
  const [x0, y0, x1, y1] =
    from === "left"
      ? [0, 0, span, 0]
      : from === "right"
        ? [width, 0, width - span, 0]
        : from === "top"
          ? [0, 0, 0, span]
          : [0, height, 0, height - span];

  const gradient = context.createLinearGradient(x0, y0, x1, y1);
  for (let i = 0; i <= 12; i++) {
    const t = i / 12;
    // smoothstep, inverted: fully erased at the edge, untouched by `span`.
    gradient.addColorStop(t, `rgb(0 0 0 / ${1 - t * t * (3 - 2 * t)})`);
  }

  context.save();
  context.globalCompositeOperation = "destination-out";
  context.fillStyle = gradient;
  context.fillRect(
    from === "right" ? width - span : 0,
    from === "bottom" ? height - span : 0,
    horizontal ? span : width,
    horizontal ? height : span,
  );
  context.restore();
}

/**
 * Mount a noise gradient into `host`.
 *
 * `host` is emptied and takes over as the positioning context. Returns a
 * handle; call `destroy()` to terminate the worker and release observers.
 *
 * If the field cannot run, `host` is left exactly as it was and the returned
 * handle is inert - see the note on the worker below.
 */
export function mountNoiseGradient(
  host: HTMLElement,
  options: NoiseGradientOptions = {},
): NoiseGradientHandle {
  let settings = { ...DEFAULTS, ...options };

  /* The worker is built first, before a single DOM mutation, because it is
     the one step here that can fail outright - and this function runs first
     in the entries that use it. apps/web's SiteLayout calls `syncField()`
     ahead of the contact, context-menu, filter and language mounts, and they
     minify into one comma expression, so an exception escaping this function
     takes all four down with it. A decorative background is then able to
     disable the language switch, which is not a trade anything here would
     make deliberately.

     Not hypothetical: an embedded runtime returned a `Worker` stub with no
     `postMessage`, so construction succeeded and the first `send()` threw
     `TypeError: o.postMessage is not a function` - which is why the guard
     below checks the shape rather than only catching the constructor. A
     `try` around `new Worker` alone would have caught nothing.

     Failing here leaves `host` as the server rendered it. The stylesheets
     already expect that: both `var(--field-ramp, …)` sites carry a fallback,
     and site-footer.css spells out what happens without a field running. */
  let worker: Worker;
  try {
    // Before the Worker, which is a Trusted Types sink under the production CSP.
    installDefaultTrustedTypesPolicy();
    /* The `new Worker(new URL(…, import.meta.url), { type: "module" })` shape is
       load-bearing and must stay literal: Vite matches it as an AST pattern to
       decide that field-worker.ts is a worker entry and bundle it as one.
       Lifting the URL into a variable or a helper is enough to lose the match,
       and then Vite copies the raw .ts file to dist as a static asset instead -
       which builds cleanly, ships, and 404s the worker in production. Verified
       by doing exactly that: dist held `field-worker.CiJLJbue.ts`. */
    worker = new Worker(new URL("./field-worker.ts", import.meta.url), {
      type: "module",
    });
    // The two methods this module goes on to call. Checked together so the
    // stub case fails here, once, rather than at whichever call site reaches
    // its missing method first - `terminate()` in `destroy()` is the other.
    if (
      typeof worker.postMessage !== "function" ||
      typeof worker.terminate !== "function"
    ) {
      throw new TypeError("Worker is missing postMessage or terminate");
    }
  } catch (error) {
    // `warn`, not `error`: the page works without the field, and both e2e
    // suites fail on console errors so that the real ones stay worth reading.
    console.warn("[noise-gradient] field unavailable:", error);
    return {
      update() {},
      destroy() {},
    };
  }

  host.classList.add("ntg");
  host.setAttribute("aria-hidden", "true");
  host.replaceChildren();

  const canvas = document.createElement("canvas");
  canvas.className = "ntg__canvas";
  host.appendChild(canvas);

  const grain = document.createElement("div");
  grain.className = "ntg__grain";
  host.appendChild(grain);

  const context = canvas.getContext("2d");

  let visible = true;
  let reducedMotion = false;
  let destroyed = false;
  let pending = 0;
  let lastSentAt = -1;
  let grainDepth = -1;

  const reduced = window.matchMedia("(prefers-reduced-motion: reduce)");

  function applyGrain(): void {
    if (settings.grainAlpha <= 0) {
      grain.style.display = "none";
      return;
    }
    if (grainDepth !== settings.grainDepth) {
      const url = makeGrainUrl(settings.grainDepth);
      if (!url) {
        grain.style.display = "none";
        return;
      }
      grainDepth = settings.grainDepth;
      grain.style.backgroundImage = `url(${url})`;
    }
    // One tile pixel per *device* pixel: any other scale resamples the noise
    // and it stops looking like grain.
    const dpr = window.devicePixelRatio || 1;
    const size = GRAIN_SIZE / dpr;
    grain.style.display = "";
    grain.style.backgroundSize = `${size}px ${size}px`;
    grain.style.opacity = String(settings.grainAlpha);
  }

  function send(): void {
    if (destroyed) return;
    const width = host.clientWidth;
    const height = host.clientHeight;
    if (!width || !height) return;
    const state: FieldState = {
      width,
      height,
      dpr: window.devicePixelRatio || 1,
      renderScale: settings.renderScale,
      ramp: serializeRamp(resolveRamp(settings.ramp)),
      frequency: settings.frequency,
      warp: settings.warp,
      wave: settings.wave,
      travel: settings.travel,
      loop: settings.loop,
      angle: settings.angle,
      progress: settings.progress,
      visible,
      reducedMotion,
    };
    lastSentAt = performance.now();
    worker.postMessage({ type: "state", state } satisfies HostToWorker);
  }

  /** Coalesce bursts (scroll frames, resize storms) down to the frame budget. */
  function schedule(): void {
    if (destroyed || pending) return;
    const wait =
      lastSentAt < 0
        ? 0
        : Math.max(0, MIN_STATE_INTERVAL_MS - (performance.now() - lastSentAt));
    pending = window.setTimeout(() => {
      pending = 0;
      send();
    }, wait);
  }

  worker.onmessage = ({ data }: MessageEvent<WorkerToHost>) => {
    if (data.type === "error") {
      console.error("[noise-gradient] worker:", data.message);
      return;
    }
    if (!context || destroyed) {
      data.bitmap.close();
      return;
    }

    const width = host.clientWidth;
    const height = host.clientHeight;
    const dpr = window.devicePixelRatio || 1;
    // A frame that raced a resize would be stretched to the wrong box; drop
    // it and ask for one at the size the host is actually at now. The ask is
    // the important half: without it the field depends on some *other*
    // observer having already queued a re-render, and if that notification
    // never comes (a dpr change leaves the CSS size alone) the last thing
    // drawn stays on screen forever.
    if (data.width !== width || data.height !== height || data.dpr !== dpr) {
      data.bitmap.close();
      schedule();
      return;
    }

    const pixelWidth = Math.round(width * dpr);
    const pixelHeight = Math.round(height * dpr);
    if (canvas.width !== pixelWidth) canvas.width = pixelWidth;
    if (canvas.height !== pixelHeight) canvas.height = pixelHeight;
    context.setTransform(pixelWidth / width, 0, 0, pixelHeight / height, 0, 0);
    context.clearRect(0, 0, width, height);
    context.imageSmoothingEnabled = true;
    // "low" is the point: the upscale from the quarter-size render is doing
    // the blurring, and a higher-quality resample would sharpen it back.
    context.imageSmoothingQuality = "low";
    context.drawImage(
      data.bitmap,
      0,
      0,
      data.bitmap.width,
      data.bitmap.height,
      0,
      0,
      width,
      height,
    );
    data.bitmap.close();

    if (settings.feather > 0) {
      featherEdge(
        context,
        width,
        height,
        settings.feather,
        settings.featherFrom,
      );
    }

    settings.onFrame?.({ progress: data.progress, profile: data.profile });
  };

  const resizeObserver = new ResizeObserver(() => {
    applyGrain();
    schedule();
  });
  resizeObserver.observe(host);

  const intersectionObserver = new IntersectionObserver((entries) => {
    const entry = entries[0];
    if (!entry) return;
    visible = entry.isIntersecting;
    schedule();
  });
  intersectionObserver.observe(host);

  const onReducedMotionChange = (): void => {
    reducedMotion = reduced.matches;
    schedule();
  };
  reducedMotion = reduced.matches;
  reduced.addEventListener("change", onReducedMotionChange);

  /* Device pixel ratio, watched on its own.

     A ResizeObserver fires on CSS-pixel size, and dpr moves independently of
     that: drag the window onto a monitor with a different scale factor, or
     zoom to a level that happens to leave the layout size alone, and the
     element is the same box at a different resolution. Both the canvas
     backing store and the grain tile are sized from dpr, and the frame check
     above rejects anything rendered at the old one - so with nothing watching
     it the field would quietly stop updating.

     `matchMedia` is the only event for this, and the query names the ratio it
     is watching, so it has to be rebuilt every time the answer changes. */
  let dprQuery: MediaQueryList | null = null;

  function onPixelRatioChange(): void {
    watchPixelRatio();
    applyGrain();
    schedule();
  }

  function watchPixelRatio(): void {
    dprQuery?.removeEventListener("change", onPixelRatioChange);
    const dpr = window.devicePixelRatio || 1;
    dprQuery = window.matchMedia(`(resolution: ${dpr}dppx)`);
    dprQuery.addEventListener("change", onPixelRatioChange);
  }

  watchPixelRatio();

  applyGrain();
  send();

  return {
    update(next) {
      if (destroyed) return;
      settings = { ...settings, ...next };
      applyGrain();
      schedule();
    },
    destroy() {
      if (destroyed) return;
      destroyed = true;
      if (pending) clearTimeout(pending);
      worker.terminate();
      resizeObserver.disconnect();
      intersectionObserver.disconnect();
      reduced.removeEventListener("change", onReducedMotionChange);
      dprQuery?.removeEventListener("change", onPixelRatioChange);
      host.replaceChildren();
      host.classList.remove("ntg");
    },
  };
}
