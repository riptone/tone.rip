/**
 * Rasterise favicon.svg into the icons browsers cannot read an SVG for.
 *
 * generate.py stops at the SVG on purpose - it draws pure geometry and takes
 * no renderer dependency - and left a note saying to regenerate the rasters
 * "when the mark changes" with whatever tool was to hand. That instruction is
 * how the wordmark became a headstone in August and every raster icon on both
 * properties stayed a backslash: the SVG was regenerated, the PNGs were not,
 * and nothing failed. A step a person has to remember is a step that drifts.
 *
 * So the rasters are outputs now, from one command:
 *
 *     cd packages/ui && bun run brand
 *
 * Chromium does the rendering, via the Playwright this repo already installs
 * for its end-to-end tests - the alternative was adding librsvg or ImageMagick
 * as a machine prerequisite for changing a logo.
 */

import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
// A default import, then the property, rather than `import { chromium }`.
// @playwright/test resolves to CommonJS under this package's `NodeNext`
// module resolution, and a named import from CJS is exactly what NodeNext
// refuses - it type-checks as "has no exported member".
import playwright from "@playwright/test";

const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url));
const PUBLIC_DIR = join(SCRIPT_DIR, "..", "public");

/**
 * The raster set, and why each one exists.
 *
 * `favicon.ico` is not in this list: it is assembled from the last two entries
 * below, because an .ico is a container rather than an image.
 */
const PNGS: { path: string; size: number; why: string }[] = [
  {
    path: "icons/apple-touch-icon.png",
    size: 180,
    why: "iOS home screen; 180 is what current iPhones ask for",
  },
  {
    path: "icons/android-chrome-192x192.png",
    size: 192,
    why: "Android home screen and the PWA manifest's baseline",
  },
  {
    path: "icons/android-chrome-512x512.png",
    size: 512,
    why: "Android splash and store listings; also the source of truth by eye",
  },
];

/** The two the .ico carries, matching the file it replaces. */
const ICO_SIZES = [16, 32];

/**
 * Strip the `@media (prefers-color-scheme: light)` block.
 *
 * The SVG flips to a white plate in a light UI, which is right for a tab icon
 * and meaningless in a PNG - a raster has one appearance, and picking it at
 * render time by whatever the renderer's default scheme happened to be is how
 * you get a white-on-white home screen icon. Removing the rule pins every
 * raster to the declared fill: black plate, white mark, as before.
 */
function pinToDarkPlate(svg: string): string {
  return svg.replace(/<style>[\s\S]*?<\/style>/, "");
}

async function main(): Promise<void> {
  const svg = pinToDarkPlate(
    await readFile(join(PUBLIC_DIR, "favicon.svg"), "utf8"),
  );

  const browser = await playwright.chromium.launch();
  const rendered = new Map<number, Buffer>();
  try {
    const sizes = [...new Set([...PNGS.map((p) => p.size), ...ICO_SIZES])];
    for (const size of sizes) {
      const page = await browser.newPage({
        viewport: { width: size, height: size },
        // Transparent behind the plate, so its rounded corners are corners
        // rather than white notches on a dark home screen.
        deviceScaleFactor: 1,
      });
      // The SVG carries no width/height, only a viewBox, so it scales to
      // whatever box it is given. `margin: 0` and a flush <html>/<body> keep
      // the plate exactly the viewport - any UA margin would shrink it and
      // leave a transparent rim.
      await page.setContent(
        `<!doctype html><style>html,body{margin:0;padding:0;width:${size}px;height:${size}px}svg{display:block;width:${size}px;height:${size}px}</style>${svg}`,
      );
      rendered.set(
        size,
        await page.locator("svg").screenshot({ omitBackground: true }),
      );
      await page.close();
    }
  } finally {
    await browser.close();
  }

  for (const { path, size, why } of PNGS) {
    const png = rendered.get(size);
    if (!png) throw new Error(`nothing rendered at ${size}px`);
    const dest = join(PUBLIC_DIR, path);
    await mkdir(dirname(dest), { recursive: true });
    await writeFile(dest, png);
    console.log(`  ${path.padEnd(34)} ${size}px  ${png.length}B  - ${why}`);
  }

  const ico = buildIco(
    ICO_SIZES.map((size) => {
      const png = rendered.get(size);
      if (!png) throw new Error(`nothing rendered at ${size}px`);
      return { size, png };
    }),
  );
  await writeFile(join(PUBLIC_DIR, "favicon.ico"), ico);
  console.log(
    `  favicon.ico                        ${ICO_SIZES.join("+")}px  ${ico.length}B`,
  );
}

/**
 * Pack PNGs into an .ico.
 *
 * An .ico is a 6-byte header, a 16-byte directory entry per image, then the
 * payloads. The payloads are PNGs rather than the format's original BMPs -
 * every browser has read PNG-in-ICO since IE11, and it is what the file this
 * replaces already contained.
 */
function buildIco(images: { size: number; png: Buffer }[]): Buffer {
  const header = Buffer.alloc(6);
  header.writeUInt16LE(0, 0); // reserved
  header.writeUInt16LE(1, 2); // 1 = icon
  header.writeUInt16LE(images.length, 4);

  const directory = Buffer.alloc(16 * images.length);
  let offset = header.length + directory.length;
  images.forEach(({ size, png }, i) => {
    const at = i * 16;
    // 0 means 256 in this field; nothing here is that large, but encoding it
    // correctly costs one comparison and a wrong 256 is a corrupt file.
    directory.writeUInt8(size >= 256 ? 0 : size, at);
    directory.writeUInt8(size >= 256 ? 0 : size, at + 1);
    directory.writeUInt8(0, at + 2); // palette size, 0 for truecolour
    directory.writeUInt8(0, at + 3); // reserved
    directory.writeUInt16LE(1, at + 4); // colour planes
    directory.writeUInt16LE(32, at + 6); // bits per pixel
    directory.writeUInt32LE(png.length, at + 8);
    directory.writeUInt32LE(offset, at + 12);
    offset += png.length;
  });

  return Buffer.concat([header, directory, ...images.map((i) => i.png)]);
}

await main();
