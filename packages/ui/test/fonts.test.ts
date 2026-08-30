import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { FONT_HREF } from "../src/fonts.js";

/* Read off disk rather than imported: the point is to check what the files
   say, and importing CSS through Vite hands back something Vite has already
   processed. `import.meta.url` is not a file URL under the jsdom environment,
   so resolve from the package root - vitest runs with the cwd there. */
const read = (path: string): string =>
  readFileSync(resolve(process.cwd(), path), "utf8");

const tokensCss = read("src/styles/tokens.css");
const baseHead = read("src/BaseHead.astro");

/* The preload in BaseHead.astro and the @font-face in tokens.css have to name
   the same file, and nothing at build or run time notices when they don't:
   the preload happily warms a URL, the font engine happily requests a
   different one, and the only symptom is that the face misses its
   `font-display: optional` window again - which is invisible in dev, where
   everything is already in cache.

   So the drift is caught here instead. */
describe("the preloaded font", () => {
  it("is the one tokens.css actually declares", () => {
    const declared = tokensCss.match(/@font-face[^}]*src:\s*url\("([^"]+)"\)/s);
    expect(declared, "no @font-face src found in tokens.css").not.toBeNull();
    expect(declared?.[1]).toBe(FONT_HREF);
  });

  it("promises only the weights the shipped file can draw", () => {
    /* The face is cut down to a weight range by scripts/subset-font.py, and
       the filename carries that range because `public/_headers` serves fonts
       `immutable` for a year - a new cut has to be a new URL.

       Which makes the filename the one honest record of what is in the file,
       and this the place the `@font-face` descriptor is checked against it.
       Widening the descriptor without re-running the script is the failure
       worth catching: the browser then believes it can ask for a weight the
       file has no data for, and answers its own request by synthesising one.
       That looks like a slightly wrong font rather than like a bug. */
    const src = tokensCss.match(/src:\s*url\("[^"]*?-(\d+)-(\d+)\.woff2"\)/);
    expect(src, "font filename does not carry a weight range").not.toBeNull();
    const declared = tokensCss.match(
      /@font-face[^}]*font-weight:\s*(\d+)\s+(\d+)/s,
    );
    expect(declared, "no @font-face font-weight range found").not.toBeNull();
    expect([declared?.[1], declared?.[2]]).toEqual([src?.[1], src?.[2]]);
  });

  it("is declared `optional`, which is what makes the preload load-bearing", () => {
    // Under `swap` or `fallback` a missed window costs a flash, not the whole
    // page's typography. Under `optional` it costs the typeface for the life
    // of the document - so if this ever relaxes, revisit the comment in
    // BaseHead.astro before assuming the preload is still critical.
    expect(tokensCss).toMatch(/font-display:\s*optional/);
  });

  it("is preloaded with crossorigin, or the browser fetches it twice", () => {
    // Fonts are fetched in CORS mode even same-origin. A preload without
    // `crossorigin` is a different request from the one the font engine
    // makes, so it neither satisfies nor speeds it up.
    const preload = baseHead.match(/<link\s+rel="preload"[\s\S]*?\/>/);
    expect(preload, "no font preload in BaseHead.astro").not.toBeNull();
    expect(preload?.[0]).toContain('as="font"');
    expect(preload?.[0]).toContain("crossorigin");
  });
});
