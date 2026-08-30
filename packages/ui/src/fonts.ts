import fontUrl from "./styles/fonts/hanken-grotesk-400-500.woff2?url";

/**
 * The one self-hosted face, by URL.
 *
 * This exists because the path has to be reached from two places that cannot
 * see each other: `styles/tokens.css` declares the `@font-face` and
 * `BaseHead.astro` preloads it. If they disagree the preload still succeeds -
 * it just warms a file nothing asks for, while the font the page actually
 * wants goes unpreloaded and misses its `font-display: optional` window. A
 * silent, invisible regression, and exactly the one the preload was added to
 * fix.
 *
 * It used to be a hand-written `/fonts/…` literal, which made that agreement
 * something a person had to maintain. Now both sides point at the same file
 * on disk and the bundler decides the URL, emitting one content-hashed copy
 * under `_astro/` that they share - so the preload cannot name a different
 * file from the one the `@font-face` asks for, because there is only one.
 *
 * test/fonts.test.ts checks the two still resolve to the same path, which is
 * the part that can drift now: the filename in either reference.
 */
export const FONT_HREF: string = fontUrl;
