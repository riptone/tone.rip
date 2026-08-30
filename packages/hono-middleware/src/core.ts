/**
 * Framework-agnostic building blocks behind the Hono middlewares in this
 * package, kept separate so the security-header/CSP policy is one pure
 * function with its own tests, independent of the middleware that applies it.
 *
 * Every app reaches them through that middleware now. The Astro apps used to
 * import these directly, because a Worker-per-app project could not run Hono
 * middleware; Astro 7's `astro/hono` ended that. See apps/web/src/fetch.ts.
 */

export const DEFAULT_PERMISSIONS_POLICY = [
  "accelerometer=()",
  "autoplay=()",
  "camera=()",
  "clipboard-read=()",
  "clipboard-write=(self)",
  "display-capture=()",
  "encrypted-media=()",
  "fullscreen=(self)",
  "geolocation=()",
  "gyroscope=()",
  "magnetometer=()",
  "microphone=()",
  "midi=()",
  "payment=()",
  "picture-in-picture=()",
  "publickey-credentials-get=()",
  "screen-wake-lock=()",
  "sync-xhr=()",
  "usb=()",
  "xr-spatial-tracking=()",
].join(", ");

export interface BuildSecurityHeadersOptions {
  url: URL;
  nonce: string;
  devHostnames?: string[];
  connectSrc?: string[];
  imgSrc?: string[];
  reportPath?: string;
  permissionsPolicy?: string;
  links?: string[];
  /** @default "same-origin" - apps/api sets "cross-origin" since it's deliberately consumed by multiple frontend origins. */
  crossOriginResourcePolicy?: string;
  /**
   * Enforce Trusted Types (production only).
   *
   * **Off, and it cannot be turned on while Cloudflare injects into the
   * HTML.** See the comment at the directive below - this is a zone-settings
   * dependency, not a code one.
   *
   * @default false
   */
  trustedTypes?: boolean;
}

export interface BuiltSecurityHeaders {
  isLocalDev: boolean;
  /** Every header except Content-Security-Policy, which is split out since callers often set it last. */
  headers: Record<string, string>;
  csp: string;
}

/** Pure computation of the security-header set + CSP for a given request URL and nonce. */
export function buildSecurityHeaders(
  options: BuildSecurityHeadersOptions,
): BuiltSecurityHeaders {
  const devHostnames = new Set(
    options.devHostnames ?? ["localhost", "127.0.0.1"],
  );
  const connectSrc = ["'self'", ...(options.connectSrc ?? [])].join(" ");
  const imgSrc = ["'self'", "data:", ...(options.imgSrc ?? [])].join(" ");
  const reportPath = options.reportPath ?? "/api/csp-report";
  const permissionsPolicy =
    options.permissionsPolicy ?? DEFAULT_PERMISSIONS_POLICY;
  const isLocalDev = devHostnames.has(options.url.hostname);

  const cspReportUrl = new URL(reportPath, options.url).toString();
  const headers: Record<string, string> = {
    "X-Content-Type-Options": "nosniff",
    "Referrer-Policy": "no-referrer",
    "Permissions-Policy": permissionsPolicy,
    "Cross-Origin-Opener-Policy": "same-origin",
    "Cross-Origin-Resource-Policy":
      options.crossOriginResourcePolicy ?? "same-origin",
    "Cross-Origin-Embedder-Policy": "unsafe-none",
    "X-Frame-Options": "SAMEORIGIN",
    "Reporting-Endpoints": `csp="${cspReportUrl}"`,
  };
  if (options.links?.length) {
    headers.Link = options.links.join(", ");
  }
  if (!isLocalDev) {
    headers["Strict-Transport-Security"] =
      "max-age=31536000; includeSubDomains; preload";
  }

  const scriptSrc = isLocalDev
    ? "script-src 'self' 'unsafe-inline'"
    : `script-src 'self' 'nonce-${options.nonce}'`;
  const styleSrc = isLocalDev
    ? "style-src 'self' 'unsafe-inline'"
    : `style-src 'self' 'nonce-${options.nonce}'`;

  const directives = [
    "default-src 'none'",
    scriptSrc,
    styleSrc,
    `img-src ${imgSrc}`,
    // The gradient field runs its pixel loop in a module worker (see
    // @repo/ui/gradient). Without this, worker-src falls back through
    // child-src to script-src, which carries a nonce a worker URL can't
    // satisfy - so state it directly rather than rely on the fallback.
    "worker-src 'self'",
    // Self-hosted woff2 only. `https:` here was left over from a Google
    // Fonts dependency that no longer exists.
    "font-src 'self'",
    `connect-src ${connectSrc}`,
    "frame-src 'self'",
    "frame-ancestors 'self'",
    "base-uri 'none'",
    "form-action 'self'",
    "object-src 'none'",
    `report-uri ${reportPath}`,
    "report-to csp",
  ];
  if (!isLocalDev) {
    directives.push("upgrade-insecure-requests");
  }
  if (!isLocalDev && options.trustedTypes) {
    /* Trusted Types: a ratchet, not a repair.
     *
     * `require-trusted-types-for 'script'` makes the DOM's injection sinks -
     * innerHTML, outerHTML, insertAdjacentHTML, document.write, eval, the
     * Worker constructor - reject plain strings. It is worth turning on here
     * because there is currently nothing to fix: a sweep of every client
     * module found no HTML sink at all. So this does not clean anything up,
     * it stops one from arriving, and it fails loudly at the moment someone
     * writes the first `innerHTML =` rather than quietly at some later audit.
     *
     * The one sink that does exist is the gradient's module worker. It is
     * covered by a *default* policy (@repo/ui/src/trusted-types.ts) that
     * implements `createScriptURL` - validating same-origin - and
     * deliberately implements neither `createHTML` nor `createScript`, so
     * those sinks throw rather than being waved through.
     *
     * A default policy rather than a named one, and that is a build
     * constraint rather than a preference: Vite recognises a worker by the
     * literal `new Worker(new URL(…, import.meta.url))` shape, so the
     * argument cannot be wrapped in a named policy's `createScriptURL(…)`
     * call without Vite losing the pattern and shipping the raw TypeScript
     * as an asset. `trusted-types default` names exactly one policy, so a
     * second cannot be created to launder strings past the first.
     *
     * Production only. Astro's dev server builds its HMR client and error
     * overlay out of innerHTML, so enforcing this under `astro dev` would
     * break the dev server rather than the site.
     *
     * Ignored entirely by browsers without Trusted Types, which is most of
     * them outside Chromium. That is the nature of a defence-in-depth header:
     * it costs nothing where it does not apply.
     *
     * ---
     *
     * ⚠️ OFF BY DEFAULT, AND THE BLOCKER IS NOT IN THIS REPOSITORY.
     *
     * Cloudflare's JavaScript Detections injects a bootstrap script into the
     * HTML *after* the Worker has run, and that script does:
     *
     *     var d = b.createElement('script');
     *     d.nonce = '<our nonce>';
     *     d.innerHTML = "window.__CF$cv$params={…}";
     *
     * Note the second line. The injector reads the CSP off the response and
     * reuses the nonce, so `script-src 'self' 'nonce-…'` admits it - by
     * design. What it cannot do is produce a TrustedHTML, so the third line
     * throws under this directive and the detection script dies on every page
     * load, with a TypeError in the console for every visitor.
     *
     * There is no fix on this side worth having. A `createHTML` that passes
     * strings through would defeat the whole point, and one that allowlists
     * Cloudflare's current payload would be blessing a third-party script
     * body we do not control and cannot pin.
     *
     * It is also not a one-off: an edge that reserves the right to inject
     * markup is structurally incompatible with Trusted Types, and Speed Brain
     * and Web Analytics inject too.
     *
     * To turn this on, first turn those off in the Cloudflare dashboard -
     * Security → Bots → JavaScript Detections, and Speed → Optimization →
     * Speed Brain - then pass `trustedTypes: true`. See docs/deployment.md.
     * Nothing local can verify it: `wrangler dev` does not run the edge
     * injectors, which is exactly how this shipped once already. */
    directives.push("require-trusted-types-for 'script'");
    directives.push("trusted-types default");
  }

  return { isLocalDev, headers, csp: directives.join("; ") };
}

export interface ApiCatalogEntryInput {
  href: string;
  type?: string;
}

/** Pure RFC 9727 linkset+json body builder, shared by the Hono middleware and any Astro app serving its own catalog. */
export function buildApiCatalogBody(entries: ApiCatalogEntryInput[]): string {
  return JSON.stringify({
    linkset: entries.map((entry) => ({
      anchor: entry.href,
      "service-desc": [
        { href: entry.href, type: entry.type ?? "application/json" },
      ],
    })),
  });
}
