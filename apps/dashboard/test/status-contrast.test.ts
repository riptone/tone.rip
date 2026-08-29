import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import {
  AA_NORMAL,
  contrastRatio,
  parseHex,
  type Rgb,
} from "@repo/ui/contrast";
import { describe, expect, it } from "vitest";

/* The four status colours are the only colours in this repo that live outside
 * the token layer, and they were the only ones nobody measured.
 *
 * @repo/ui's own test computes WCAG ratios for the text tokens, on the
 * argument that "a colour that is slightly too dim looks like a design
 * decision right up until someone runs an audit". That argument applies here
 * more than there: these carry meaning rather than emphasis - green is `up`,
 * red is `down` - so a reader who cannot resolve them loses information, not
 * polish. They just happened to sit in a different file.
 *
 * They pass today, but not all of them comfortably: `checking` in the light
 * theme is 4.89:1 against a 4.5 floor. That is the case this exists for -
 * nudging that amber a shade warmer to look nicer is a one-character change
 * with no visible consequence until it is below the line.
 *
 * Read from the stylesheet rather than restated here, so the test cannot pass
 * against a copy of the colours that the page no longer uses. */

const css = readFileSync(
  resolve(process.cwd(), "src/styles/dashboard.css"),
  "utf8",
);

/**
 * `--bg` for each theme, from @repo/ui's tokens - the surface a tile sits on.
 *
 * Hardcoded rather than parsed: these two are the definition of the themes
 * and they are asserted below, so a change to either fails here with the
 * reason rather than silently re-baselining every ratio against a new
 * background.
 */
const BACKGROUNDS: Record<string, Rgb> = {
  light: [0xf7, 0xf7, 0xf5],
  dark: [0, 0, 0],
};

const STATUSES = ["up", "down", "checking", "vpn"] as const;

/**
 * Pull the `--status` value out of the rule for one status in one theme.
 *
 * Dark is the base rule and light is the `html[data-theme="light"]` override,
 * matching tokens.css. The dark pattern therefore has to refuse a match that
 * follows a descendant selector - without the lookbehind it would also match
 * the light rule, and both themes would silently test the same four colours
 * and pass.
 */
function statusColor(status: string, theme: "light" | "dark"): Rgb {
  const selector =
    theme === "light"
      ? `html\\[data-theme="light"\\] \\.status\\[data-status="${status}"\\]`
      : `(?<!\\] )\\.status\\[data-status="${status}"\\]`;
  const match = css.match(
    new RegExp(`${selector}\\s*\\{[^}]*?--status:\\s*(#[0-9a-fA-F]{3,6})`),
  );
  if (!match?.[1]) {
    throw new Error(
      `no ${theme} colour for status "${status}" in dashboard.css`,
    );
  }
  return parseHex(match[1]);
}

describe("status colours meet WCAG AA", () => {
  for (const theme of ["light", "dark"] as const) {
    for (const status of STATUSES) {
      it(`${status} is readable in the ${theme} theme`, () => {
        const background = BACKGROUNDS[theme];
        if (!background) throw new Error(`no background for ${theme}`);
        const ratio = contrastRatio(statusColor(status, theme), background);
        expect(
          Number(ratio.toFixed(2)),
          `${status} (${theme}) is ${ratio.toFixed(2)}:1`,
        ).toBeGreaterThanOrEqual(AA_NORMAL);
      });
    }
  }

  it("gives every status a colour in both themes", () => {
    // A status added to dashboard.ts but only styled for one theme renders as
    // inherited body text in the other - legible, and indistinguishable from
    // every other status.
    for (const status of STATUSES) {
      expect(() => statusColor(status, "light"), status).not.toThrow();
      expect(() => statusColor(status, "dark"), status).not.toThrow();
    }
  });
});
