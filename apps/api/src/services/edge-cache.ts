/**
 * The Workers edge cache, or undefined where there isn't one.
 *
 * Three services wanted this and each carried its own copy of the same line,
 * so it lives here once. The awkward shape of that line is the interesting
 * part.
 *
 * `caches.default` is a Cloudflare extension. This project's generated
 * `worker-configuration.d.ts` declares `CacheStorage` with a `default`
 * property, so plain `caches.default` compiles under `bun run check-types`.
 * The DOM lib's `CacheStorage` has no such property - it only has `open()` -
 * and an editor that resolves that one instead (which happens easily in a
 * monorepo where another package pulls in `lib.dom`) reports an error on code
 * that builds perfectly well.
 *
 * Neither obvious spelling survives both:
 *
 *   caches.default                        -> TS2339 under the DOM lib
 *   globalThis as { caches?: {...} }      -> TS2352 under the DOM lib, since
 *                                            the two types "do not
 *                                            sufficiently overlap"
 *
 * So the assertion goes through `unknown`, which is what TypeScript itself
 * suggests for the second case, and is honest about what is happening: this
 * is knowledge about the runtime that the ambient types may or may not have.
 * The optional chain then keeps it true even when the property really is
 * absent.
 *
 * That last part is not hypothetical. Unit tests run outside the Workers
 * runtime, and every caller already treats a missing cache as a cache miss
 * rather than an error.
 */
export function getEdgeCache(): Cache | undefined {
  const store = globalThis as unknown as { caches?: { default?: Cache } };
  return store.caches?.default;
}
