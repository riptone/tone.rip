import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
  AA_NORMAL,
  contrastRatio,
  flatten,
  type Rgb,
} from "../src/contrast.js";

/* The text tokens have to stay legible, and "legible" is a number.
 *
 * The arithmetic is in src/contrast.ts, shared with apps/dashboard's own
 * status-colour test; the reasoning for measuring at all is there too. What
 * is here is the part specific to tokens.css: pulling `rgba()` inks out of
 * the right theme block and compositing them onto that theme's background. */

const tokensCss = readFileSync(
  resolve(process.cwd(), "src/styles/tokens.css"),
  "utf8",
);

/**
 * Read `--name: rgba(r, g, b, a)` out of a block of the stylesheet.
 *
 * Scoped to a block because the same token names are declared twice, once per
 * theme, and reading the first match would silently only ever test the dark
 * one - which is the half that was already correct.
 */
function readToken(block: string, name: string): { ink: Rgb; alpha: number } {
  const match = block.match(new RegExp(`${name}:\\s*rgba\\(([^)]+)\\)`, "i"));
  const raw = match?.[1];
  if (!raw) throw new Error(`${name} not found, or no longer an rgba()`);
  const [r, g, b, alpha] = raw.split(",").map((p) => Number(p.trim()));
  // Not pedantry: a three-part rgb() here would have made `alpha` undefined,
  // `flatten` return NaNs, and every ratio compare false against the
  // threshold - a green suite that had stopped measuring anything.
  if (
    r === undefined ||
    g === undefined ||
    b === undefined ||
    alpha === undefined
  ) {
    throw new Error(`${name} is not a four-part rgba(): ${raw}`);
  }
  return { ink: [r, g, b], alpha };
}

function blockFor(selector: string): string {
  const start = tokensCss.indexOf(selector);
  if (start === -1) throw new Error(`no ${selector} block in tokens.css`);
  const open = tokensCss.indexOf("{", start);
  const close = tokensCss.indexOf("}", open);
  return tokensCss.slice(open, close);
}

const THEMES: { name: string; block: string; background: Rgb }[] = [
  { name: "dark", block: ":root", background: [0, 0, 0] },
  {
    name: "light",
    block: 'html[data-theme="light"]',
    background: [0xf7, 0xf7, 0xf5],
  },
];

describe("text tokens meet WCAG AA", () => {
  for (const theme of THEMES) {
    for (const token of ["--text-muted", "--text-faint"]) {
      it(`${token} is readable in the ${theme.name} theme`, () => {
        const { ink, alpha } = readToken(blockFor(theme.block), token);
        const ratio = contrastRatio(
          flatten(ink, alpha, theme.background),
          theme.background,
        );
        expect(
          Number(ratio.toFixed(2)),
          `${token} (${theme.name}) is ${ratio.toFixed(2)}:1`,
        ).toBeGreaterThanOrEqual(AA_NORMAL);
      });
    }
  }

  it("keeps faint fainter than muted, or the hierarchy is a lie", () => {
    // Raising a failing token is the fix; raising it past its neighbour just
    // moves the problem into the design.
    for (const theme of THEMES) {
      const block = blockFor(theme.block);
      const muted = readToken(block, "--text-muted");
      const faint = readToken(block, "--text-faint");
      expect(faint.alpha, `${theme.name}`).toBeLessThan(muted.alpha);
    }
  });
});
