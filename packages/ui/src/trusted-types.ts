/* The default Trusted Types policy.
 *
 * The production CSP carries `require-trusted-types-for 'script'` (see
 * @repo/hono-middleware's core.ts). Under it, every DOM injection sink -
 * `innerHTML`, `outerHTML`, `insertAdjacentHTML`, `document.write`, `eval`,
 * the `Worker` constructor - rejects a plain string. A *default* policy is
 * what a string is routed through when it reaches a sink without having been
 * marked trusted first.
 *
 * The shape of this one is the whole design, so it is worth being explicit
 * about what it does and does not implement:
 *
 *   createScriptURL   implemented, and it validates. Same-origin or nothing.
 *   createHTML        deliberately absent.
 *   createScript      deliberately absent.
 *
 * A default policy that omits a member does not wave the sink through - it
 * makes that sink throw. So `element.innerHTML = someString` is a TypeError
 * from the moment this exists, in production, in Chromium. That is the point.
 * The codebase currently has no HTML sink anywhere in it; this is not a
 * cleanup, it is a ratchet that fails loudly the first time somebody adds
 * one, instead of quietly at whatever audit finds it later.
 *
 * The one sink that is genuinely needed is the gradient's module worker, and
 * a default policy is what lets that keep the literal
 * `new Worker(new URL(…, import.meta.url))` form Vite has to see in order to
 * bundle the worker at all. Wrapping the argument in a named policy was tried
 * first and broke the build in the quietest possible way - Vite stopped
 * recognising the pattern and shipped the raw TypeScript source as an asset.
 *
 * Browsers without Trusted Types (everything outside Chromium, at time of
 * writing) have no `window.trustedTypes`, ignore the directive, and are
 * unaffected either way.
 */

interface TrustedTypesFactory {
  createPolicy(
    name: string,
    rules: { createScriptURL(input: string): string },
  ): unknown;
  readonly defaultPolicy: unknown;
}

/* The policy name the CSP's `trusted-types` directive has to allow.
   Not exported: the directive is written in @repo/hono-middleware's core.ts,
   which must not import this package - the dependency runs the other way,
   and both Astro apps call that core directly. The two are kept in step by
   the comment there naming this file. */
const DEFAULT_POLICY_NAME = "default";

let installed = false;

/**
 * Create the default policy, once.
 *
 * Idempotent and safe to call from anywhere: creating a second policy of the
 * same name throws, and this is reachable from two entry points (the site
 * shell and the 404 pages) that never share a module instance.
 *
 * Called immediately before the `Worker` construction rather than at module
 * scope, so the ordering is visible at the point it matters. Timing is not
 * load-bearing for the HTML sinks - a sink used before this runs has no
 * default policy at all and throws too, which is the same answer.
 */
export function installDefaultTrustedTypesPolicy(): void {
  if (installed) return;
  installed = true;

  const factory = (window as unknown as { trustedTypes?: TrustedTypesFactory })
    .trustedTypes;
  // Absent: the browser does not implement Trusted Types, so nothing enforces
  // them either. Already set: another module instance got here first.
  if (!factory || factory.defaultPolicy) return;

  factory.createPolicy(DEFAULT_POLICY_NAME, {
    createScriptURL(input: string): string {
      const resolved = new URL(input, window.location.href);
      if (resolved.origin !== window.location.origin) {
        throw new TypeError(
          `refusing to load a script from ${resolved.origin}: same-origin only`,
        );
      }
      return resolved.href;
    },
  });
}
