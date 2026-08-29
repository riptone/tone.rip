/* WCAG contrast, as a number.
 *
 * `--text-faint` shipped at rgba(255,255,255,0.4) for a long time, which is
 * 3.66:1 on black - under AA's 4.5:1, and every use of it was normal-size
 * text. Nothing caught it, because a colour that is slightly too dim looks
 * like a design decision right up until someone runs an audit. So the ratios
 * are computed from the stylesheets themselves, and a colour that drops below
 * the threshold fails the build instead of the next audit.
 *
 * This lives in `src/` rather than beside the test that first needed it
 * because a second stylesheet now needs the same guard: apps/dashboard paints
 * four status colours per theme, in hex, outside the token layer. Two copies
 * of the sRGB luminance formula is two chances to get it subtly wrong in one
 * of them - and the wrong one would be the one that passes.
 */

export type Rgb = [number, number, number];

function channel(value: number): number {
  const c = value / 255;
  return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
}

function luminance([r, g, b]: Rgb): number {
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

/** WCAG 2.1 relative contrast between two opaque colours. 1:1 to 21:1. */
export function contrastRatio(a: Rgb, b: Rgb): number {
  // Math.max/min rather than sorting and destructuring, which under
  // `noUncheckedIndexedAccess` types both halves as possibly undefined - true
  // of an array index in general, and not worth a non-null assertion here.
  const la = luminance(a);
  const lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

/** What an `rgba()` ink actually looks like once composited onto a background. */
export function flatten(ink: Rgb, alpha: number, background: Rgb): Rgb {
  return ink.map((c, i) =>
    Math.round(c * alpha + (background[i] ?? 0) * (1 - alpha)),
  ) as Rgb;
}

/** `#rgb` or `#rrggbb`. Throws rather than guessing - a silent 0,0,0 would pass. */
export function parseHex(hex: string): Rgb {
  const h = hex.replace("#", "").trim();
  const full =
    h.length === 3
      ? h
          .split("")
          .map((c) => c + c)
          .join("")
      : h;
  if (!/^[0-9a-f]{6}$/i.test(full)) throw new Error(`not a hex colour: ${hex}`);
  return [
    Number.parseInt(full.slice(0, 2), 16),
    Number.parseInt(full.slice(2, 4), 16),
    Number.parseInt(full.slice(4, 6), 16),
  ];
}

/** WCAG 2.1 AA for normal-size text. */
export const AA_NORMAL = 4.5;
