/**
 * The one self-hosted face, by URL.
 *
 * This exists because the path has to be written in two places that cannot
 * see each other: `styles/tokens.css` declares the `@font-face` and
 * `BaseHead.astro` preloads it. If they disagree the preload still succeeds -
 * it just warms a file nothing asks for, while the font the page actually
 * wants goes unpreloaded and misses its `font-display: optional` window. A
 * silent, invisible regression, and exactly the one the preload was added to
 * fix.
 *
 * So: one constant, and a test that reads the stylesheet back and checks it
 * still says the same thing (test/fonts.test.ts).
 */
export const FONT_HREF = "/fonts/hanken-grotesk-400-500.woff2";
