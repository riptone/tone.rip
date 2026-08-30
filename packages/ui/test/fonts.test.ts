import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { describe, expect, it } from "vitest";

/* Read off disk rather than imported: the point is to check what the files
   say, and importing CSS through Vite hands back something Vite has already
   processed. `import.meta.url` is not a file URL under the jsdom environment,
   so resolve from the package root - vitest runs with the cwd there. */
const read = (path: string): string =>
  readFileSync(resolve(process.cwd(), path), "utf8");

const TOKENS = "src/styles/tokens.css";
const FONTS_TS = "src/fonts.ts";

const tokensCss = read(TOKENS);
const fontsTs = read(FONTS_TS);
const baseHead = read("src/BaseHead.astro");

/**
 * The file a source reference points at, as an absolute path.
 *
 * Both references are relative now - the `@font-face` resolves one from
 * tokens.css and the `?url` import resolves one from fonts.ts - so comparing
 * the strings would compare two paths written from different directories.
 * Resolving each against its own file is what makes them comparable.
 */
function resolveFrom(sourceFile: string, reference: string): string {
  return resolve(dirname(resolve(process.cwd(), sourceFile)), reference);
}

/* The preload in BaseHead.astro and the @font-face in tokens.css have to name
   the same file, and nothing at build or run time notices when they don't:
   the preload happily warms a URL, the font engine happily requests a
   different one, and the only symptom is that the face misses its
   `font-display: optional` window again - which is invisible in dev, where
   everything is already in cache.

   So the drift is caught here instead. */
describe("the preloaded font", () => {
  it("is the same file the @font-face declares", () => {
    /* Both sides now point at a file on disk rather than at a hand-written
       `/fonts/…` literal, and the bundler emits one content-hashed copy under
       _astro/ that they share - so they can no longer name different URLs.
       What can still drift is which file each one names, which is this. */
    const declared = tokensCss.match(/@font-face[^}]*src:\s*url\("([^"]+)"\)/s);
    expect(declared?.[1], "no @font-face src found in tokens.css").toBeTruthy();
    const imported = fontsTs.match(/import\s+\w+\s+from\s+"([^"]+)\?url"/);
    expect(
      imported?.[1],
      "fonts.ts no longer imports a font ?url",
    ).toBeTruthy();

    const fromCss = resolveFrom(TOKENS, declared?.[1] ?? "");
    const fromTs = resolveFrom(FONTS_TS, imported?.[1] ?? "");
    expect(fromTs).toBe(fromCss);
    expect(existsSync(fromCss), `${fromCss} does not exist`).toBe(true);
  });

  it("is emitted by the bundler, not served from public/", () => {
    /* The `@font-face` used to name an absolute `/fonts/…` path, which meant
       a hand-named file in public/ and a `_headers` rule granting it a year
       of `immutable` caching on trust. A relative path makes the bundler emit
       it under _astro/ with a content hash, where that year is safe because a
       different cut is a different URL.

       Asserted because reverting it is one character - a leading slash - and
       the page would look perfectly fine afterwards while quietly going back
       to a cache lifetime nothing enforces. */
    const declared = tokensCss.match(/@font-face[^}]*src:\s*url\("([^"]+)"\)/s);
    expect(declared?.[1]).toMatch(/^\.{1,2}\//);
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
