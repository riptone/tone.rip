/* The gradient field: noise, the per-pixel render, and the worker contract.

   These three things live together because they are one thing viewed from
   three angles - the noise exists only to displace the field, and the worker
   message is the field's parameters plus what it takes to ship a frame. Split
   across separate modules they were three files nobody could read in
   isolation anyway.

   What stays out: colour (./oklab.ts, ./ramps.ts) is genuinely independent
   and separately testable, and the canvas/DOM plumbing (./noise-gradient.ts)
   is a different concern from the maths. Everything here is pure and free of
   browser APIs, so it runs identically in a worker and in a test.

   The whole effect is one expression per pixel:

     t      = axis + scrollOffset + noiseDisplacement + dither
     colour = ramp(smoothstep(clamp(t)))

   `axis` is a normalised distance along the gradient direction. Everything
   else bends it: `travel` decides how much of the ramp a full scroll walks
   through, `warp` decides how far noise can push a pixel away from its
   neighbours, and `dither` is a sub-quantum jitter whose only job is to break
   up banding. */

import type { FlatRampStop, Ramp } from "./ramps.js";
import { sampleRamp } from "./ramps.js";

/* ---------------------------------------------------------------- noise -- */

/** Hermite ease, `3t² - 2t³`. Flat at both ends, so tiles meet without a crease. */
export function smoothstep(t: number): number {
  return t * t * (3 - 2 * t);
}

/**
 * Integer hash → [0, 1). Two odd primes to mix the coordinates, then an
 * xorshift-multiply avalanche so neighbouring cells land far apart.
 */
export function hash2(x: number, y: number): number {
  let n = (x * 374761393 + y * 668265263) | 0;
  n = Math.imul(n ^ (n >>> 13), 1274126177);
  n ^= n >>> 16;
  return (n >>> 0) / 4294967296;
}

/** Bilinear value noise on the integer lattice, smoothstepped between cells. */
export function valueNoise(x: number, y: number): number {
  const xi = Math.floor(x);
  const yi = Math.floor(y);
  const fx = smoothstep(x - xi);
  const fy = smoothstep(y - yi);
  const a = hash2(xi, yi);
  const b = hash2(xi + 1, yi);
  const c = hash2(xi, yi + 1);
  const d = hash2(xi + 1, yi + 1);
  return a + (b - a) * fx + (c - a) * fy + (a - b - c + d) * fx * fy;
}

/**
 * Two-octave fractal noise.
 *
 * The second octave is offset by an irrational-ish translation as well as
 * scaled, so the two lattices never line up and produce a visible grid.
 * Output spans roughly [0, 0.94] with a mean near {@link FBM_MEAN} - see
 * that constant for why the caller subtracts it.
 */
export function fbm2(x: number, y: number): number {
  let value = 0.5 * valueNoise(x, y);
  value += 0.25 * valueNoise(x * 2.03 + 17.13, y * 2.03 + 9.71);
  return value * 1.25;
}

/**
 * Mean of {@link fbm2}, give or take.
 *
 * The field displaces a ramp by `fbm2(...) - FBM_MEAN`, so this needs to be
 * the average for the displacement to be zero-centred. If it were not, the
 * whole ramp would slide toward one end and `progress: 0` would no longer
 * show the ramp's first stop.
 */
export const FBM_MEAN = 0.47;

/* ---------------------------------------------------------------- field -- */

/** Horizontal bands averaged out of each frame and reported back. */
export const PROFILE_ROWS = 25;

/**
 * Sub-quantum noise added per pixel purely to defeat banding.
 *
 * A smooth ramp across a large area steps through 8-bit colour in visible
 * bands. Jittering each pixel by well under one step turns the hard edge
 * into dither the eye integrates away. Too much reads as dirt; this is
 * about a quarter of a step.
 */
const DITHER = 0.006;

export interface FieldParams {
  /** Render-buffer size in px - normally a fraction of the display size. */
  width: number;
  height: number;
  /** Display aspect ratio (w/h). Keeps noise cells square when the box is not. */
  aspect: number;
  ramp: Ramp;
  /** Noise cells across the field. Higher is busier. */
  frequency: number;
  /** How far noise may displace a pixel along the ramp, 0–1. */
  warp: number;
  /** Fraction of the ramp a full 0→1 scroll walks through. */
  travel: number;
  /** Gradient direction in degrees; 90 is top-to-bottom. */
  angle: number;
  /** Smoothed scroll position, 0–1. */
  progress: number;
  /** Autonomous drift in noise units, so the field breathes when nothing scrolls. */
  phase: number;
  /** Wrap the ramp instead of clamping it - only sensible for cyclic ramps. */
  loop: boolean;
}

/**
 * Render one frame into `target` (RGBA, `width * height * 4` bytes) and fill
 * `profile` with {@link PROFILE_ROWS} averaged RGB bands.
 *
 * The profile is accumulated during the main pass rather than by re-reading
 * the buffer afterwards: the colours are already in registers at that point,
 * so it costs an add per pixel on the sampled rows and nothing elsewhere.
 */
export function renderField(
  target: Uint8ClampedArray,
  params: FieldParams,
  profile: Int16Array = new Int16Array(PROFILE_ROWS * 3),
): Int16Array {
  const { width, height, aspect, ramp, frequency, warp, travel, progress } =
    params;

  // Exactly, not "at least". Pixels are addressed at `(y * width + x) * 4`,
  // so `width` *is* the stride - a buffer wider than the frame would be
  // written contiguously here and read back at its own stride by whatever
  // blits it, shearing the image into diagonal garbage. That was a real bug:
  // the worker kept an oversized, geometrically-grown buffer, so every
  // *shrink* of the field (narrowing the window, crossing into the mobile
  // rail, a phone rotating) produced a corrupt frame that then stuck around.
  // Cheap to check once per frame; silent corruption is not cheap to find.
  if (target.length !== width * height * 4) {
    throw new RangeError(
      `renderField: target must be exactly ${width * height * 4} bytes for ${width}\u00d7${height}, got ${target.length}`,
    );
  }

  // `travel` compresses the ramp so scrolling slides a window along it
  // without ever running off the end: at travel=1 the visible slice is half
  // the ramp, and scrolling walks it across the other half.
  const scale = 1 / (1 + travel);
  const scrollOffset = travel * scale * progress;
  const warpAmount = warp * scale;
  const period = travel * scale;

  // Project (x, y) onto the gradient direction, then renormalise so the
  // projection still spans exactly 0–1 whatever the angle.
  const radians = (params.angle * Math.PI) / 180;
  const dx = Math.cos(radians) * aspect;
  const dy = Math.sin(radians);
  const origin = Math.min(0, dx) + Math.min(0, dy);
  const extent = Math.abs(dx) + Math.abs(dy) || 1;

  // Which profile band, if any, each row feeds. Resolved once per frame so
  // the inner loop only ever tests an integer.
  const bandOfRow = new Int32Array(height).fill(-1);
  for (let band = 0; band < PROFILE_ROWS; band++) {
    const row = Math.round((band / (PROFILE_ROWS - 1)) * (height - 1));
    bandOfRow[row] = band;
  }
  profile.fill(0);

  for (let y = 0; y < height; y++) {
    const yn = y / height;
    const noiseY = yn * frequency;
    const band = bandOfRow[y] ?? -1;
    let sumR = 0;
    let sumG = 0;
    let sumB = 0;

    for (let x = 0; x < width; x++) {
      const xn = x / width;
      const axis = ((xn * dx + yn * dy - origin) / extent) * scale;
      const displacement =
        (fbm2(xn * frequency * aspect + params.phase, noiseY) - FBM_MEAN) *
        warpAmount;
      const dither = (hash2(x, y + 7919) - 0.5) * DITHER;

      let t = axis + scrollOffset + displacement + dither;
      if (params.loop && period > 0) {
        t = (((t % period) + period) % period) / period;
      } else if (t < 0) {
        t = 0;
      } else if (t > 1) {
        t = 1;
      }

      const [r, g, b] = sampleRamp(ramp, smoothstep(t));
      const i = (y * width + x) * 4;
      target[i] = r;
      target[i + 1] = g;
      target[i + 2] = b;
      target[i + 3] = 255;

      if (band >= 0) {
        sumR += r;
        sumG += g;
        sumB += b;
      }
    }

    if (band >= 0) {
      profile[band * 3] = Math.round(sumR / width);
      profile[band * 3 + 1] = Math.round(sumG / width);
      profile[band * 3 + 2] = Math.round(sumB / width);
    }
  }

  return profile;
}

/**
 * A tile of monochrome noise for the grain overlay, as RGBA.
 *
 * This is *not* the field's noise. The field's noise is smooth, low
 * frequency, and lives in colour space; this is per-pixel white noise that
 * sits on top at `mix-blend-mode: overlay`, which is what stops a large
 * smooth gradient from reading as vector art. `depth` is the peak deviation
 * from mid-grey, 0–127.
 */
export function renderGrainTile(
  target: Uint8ClampedArray,
  size: number,
  depth: number,
): void {
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const i = (y * size + x) * 4;
      // Offset the lattice well away from the origin: hash2 has least to
      // avalanche near (0, 0), where the multiply barely moves the bits.
      const value = 128 + (hash2(x + 4096, y + 4096) - 0.5) * 2 * depth;
      target[i] = value;
      target[i + 1] = value;
      target[i + 2] = value;
      target[i + 3] = 255;
    }
  }
}

/* ------------------------------------------------------------- protocol -- */

/** Everything the worker needs to render a frame. Plain data - structured-cloneable. */
export interface FieldState {
  /** CSS pixel size of the host element. */
  width: number;
  height: number;
  /** `devicePixelRatio` at the time of the request. */
  dpr: number;
  /** Fraction of the display size actually rendered, 0.1–1. */
  renderScale: number;
  ramp: FlatRampStop[];
  frequency: number;
  warp: number;
  /** Autonomous drift speed in noise units per second. 0 renders only on change. */
  wave: number;
  travel: number;
  loop: boolean;
  angle: number;
  progress: number;
  /** False while the host is scrolled out of view - the worker idles instead of rendering. */
  visible: boolean;
  reducedMotion: boolean;
}

interface StateMessage {
  type: "state";
  state: FieldState;
}

interface FrameMessage {
  type: "frame";
  bitmap: ImageBitmap;
  /** Echoed back so the host can drop a frame that raced a resize. */
  width: number;
  height: number;
  dpr: number;
  progress: number;
  /** Averaged RGB bands, flat. See `PROFILE_ROWS`. */
  profile: Int16Array;
}

interface ErrorMessage {
  type: "error";
  message: string;
}

/* The two the rest of the module graph names. The three shapes above are
   only ever reached through these, so they stay private to this file - see
   noise-gradient.ts and field-worker.ts, which import nothing else. */
export type HostToWorker = StateMessage;
export type WorkerToHost = FrameMessage | ErrorMessage;
