/* The site's request pipeline, as a Hono app.
 *
 * Astro 7 builds `src/fetch.ts`'s default export into the server and
 * dispatches to it (`BaseApp.render` hands off to the custom fetch handler
 * instead of running its own pipeline), so this file *is* the request path.
 * `astro()` at the bottom runs everything Astro would otherwise have run on
 * its own: routing, sessions, user middleware, redirects, actions, pages.
 *
 * **Why this replaced `src/middleware.ts`.** That file opened by explaining
 * that this app "can't use the Hono middlewares in @repo/hono-middleware
 * directly" because it is its own Worker rather than part of apps/api's Hono
 * app - so it hand-wrote the www redirect, the dev robots.txt, the RFC 9727
 * catalog and the markdown negotiation, all four of which already existed in
 * that package as Hono middleware with tests. Astro 7's `astro/hono` made
 * that false: the package's middlewares run here now, and the three that had
 * no consumer at all finally have one.
 *
 * The same change retired `@repo/hono-middleware/astro-security`, which was a
 * second implementation of `securityHeaders()` shaped for Astro's middleware
 * signature. Both Astro apps now run the one that apps/api runs.
 *
 * Ordering is deliberate and matches what `src/middleware.ts` did, so this is
 * a refactor rather than a behaviour change: the short-circuits answer before
 * `securityHeaders` is reached, exactly as they did before. The consequence
 * worth knowing is that a redirect, the catalog and the markdown homepage
 * carry their own headers rather than the full baseline set - unchanged from
 * before, and each of those three sets its own `nosniff`.
 *
 * Note what is *not* delegated: `astro/hono` exports a `trailingSlash()`, and
 * it is a no-op here. It acts on `config.trailingSlash`, which is `"ignore"`
 * on purpose - `"never"` makes Astro's router answer /cv/ with a 404, turning
 * duplicate content into a dead link for anyone following an older URL. So
 * the 308 below stays hand-written; it is a different rule than Astro's.
 */

import { TONE_INFO } from "@repo/content";
import {
  apiCatalog,
  type CspNonceEnv,
  devRobots,
  markdownNegotiation,
  securityHeaders,
  wwwRedirect,
} from "@repo/hono-middleware";
import { astro, getFetchState } from "astro/hono";
import { type Context, Hono, type MiddlewareHandler } from "hono";

const APEX_HOST = "tone.rip";
const DEV_HOSTNAME = "dev.tone.rip";
const API_ORIGIN = "https://api.tone.rip";
// Centralized in apps/api - this app does not serve its own CSP-report
// endpoint.
const CSP_REPORT_URL = `${API_ORIGIN}/csp-report`;

const LINKS = [
  '</.well-known/api-catalog>; rel="api-catalog"',
  '</llms.txt>; rel="describedby"; type="text/markdown"',
];

/**
 * One canonical URL per page.
 *
 * /cv/ and /cv both used to answer 200 and each named itself canonical, which
 * is textbook duplicate content. In production this rarely runs - Workers
 * Assets normalises the trailing slash before the Worker is invoked at all -
 * but `astro dev` has no assets layer, and a dev server that 200s a URL
 * production redirects is how the difference goes unnoticed until it is live.
 */
const trailingSlashRedirect: MiddlewareHandler = async (c, next) => {
  const url = new URL(c.req.url);
  if (url.pathname.length > 1 && url.pathname.endsWith("/")) {
    url.pathname = url.pathname.replace(/\/+$/, "");
    return c.redirect(url.toString(), 308);
  }
  await next();
};

/**
 * Publish the request's nonce where the templates can read it.
 *
 * `securityHeaders` puts the nonce on the Hono context; `BaseHead.astro` and
 * `SiteLayout.astro` read `Astro.locals.cspNonce`. `FetchState.locals` is the
 * same object Astro hands to a page as `Astro.locals`, and `getFetchState`
 * caches its state on the context - so the instance seeded here is the one
 * `astro()` renders with.
 */
function bindNonceToAstroLocals(c: Context, nonce: string): void {
  getFetchState(c).locals.cspNonce = nonce;
}

/**
 * Stamp the request nonce onto inline <style>/<script> tags that lack one.
 *
 * A backstop, and deliberately kept as one after the thing it was written for
 * went away. It arrived because `<ClientRouter />` injected an unnonced
 * stylesheet; the site now uses browser-native cross-document transitions,
 * which inject nothing, and every tag the templates emit already carries the
 * nonce. What it still covers is everything Astro may decide to inline on its
 * own - `build.inlineStylesheets` puts small component stylesheets in the
 * document by default, so adding one <style> block to any .astro file three
 * months from now would silently produce a page whose CSS the browser
 * refuses. In production only: `astro dev` serves 'unsafe-inline', so no
 * local test can catch it.
 *
 * (Astro's own `security.csp` is the other way to solve this and is still not
 * it: checked again against 7.2.9, it emits a second policy in a <meta> tag,
 * and two enforced policies intersect - so the effective rules would live in
 * two places that have to be reasoned about together. A meta policy also
 * cannot carry `frame-ancestors` or `report-uri`, both of which are in ours.)
 *
 * HTMLRewriter is a Workers primitive and does not exist under `astro dev`,
 * which is fine for the same reason: dev serves 'unsafe-inline'.
 */
function nonceInlineTags(response: Response, nonce?: string): Response {
  if (!nonce || typeof HTMLRewriter === "undefined") return response;
  if (!(response.headers.get("Content-Type") ?? "").includes("text/html")) {
    return response;
  }

  // `:not([nonce])` would be the obvious selector, but HTMLRewriter supports
  // only a subset of CSS and `:not()` is not in it - hence the check inside.
  const stamp = {
    element(element: {
      getAttribute(n: string): string | null;
      setAttribute(n: string, v: string): void;
    }) {
      if (element.getAttribute("nonce") === null) {
        element.setAttribute("nonce", nonce);
      }
    },
  };

  return (
    new HTMLRewriter()
      .on("style", stamp)
      // External scripts are already covered by 'self'; this is for anything
      // inline. Stamping a src'd script too would be harmless but untrue.
      .on("script", stamp)
      .transform(response)
  );
}

export interface SiteAppOptions {
  /**
   * What answers once the middleware chain has had its say. Defaults to
   * Astro's own pipeline.
   */
  render?: MiddlewareHandler<CspNonceEnv>;
  /**
   * How the nonce reaches `Astro.locals`.
   *
   * Injectable because the default cannot run outside a built Worker:
   * `getFetchState` constructs a `FetchState` from the ambient manifest, and
   * `getAmbientManifest()` throws when there is no build to read one from.
   * Unit tests pass their own recorder; nothing else should override this.
   */
  bindNonce?: (c: Context, nonce: string) => void;
}

/** The pipeline, with its two production defaults injectable for tests. */
export function createSiteApp(options: SiteAppOptions = {}): Hono<CspNonceEnv> {
  const render = options.render ?? (astro() as MiddlewareHandler<CspNonceEnv>);
  const bindNonce = options.bindNonce ?? bindNonceToAstroLocals;

  const app = new Hono<CspNonceEnv>();

  app.use(wwwRedirect({ apexHost: APEX_HOST }));
  app.use(trailingSlashRedirect);
  app.use(devRobots({ devHostnames: [DEV_HOSTNAME] }));
  app.use(apiCatalog({ entries: [{ href: `${API_ORIGIN}/projects` }] }));
  app.use(markdownNegotiation({ markdown: TONE_INFO.markdown }));

  app.use(
    securityHeaders({
      devHostnames: ["localhost", "127.0.0.1"],
      // api.tone.rip only. The project list is fetched server-side (see
      // services/projects.ts), so no page actually needs this at the moment -
      // but the API is the one origin this site is ever expected to talk to,
      // and naming it is cheaper than rediscovering why a fetch is blocked.
      connectSrc: [API_ORIGIN],
      reportPath: CSP_REPORT_URL,
      links: LINKS,
    }),
  );

  app.use(async (c, next) => {
    const nonce = c.get("cspNonce");
    bindNonce(c, nonce);
    await next();
    c.res = nonceInlineTags(c.res, nonce);
  });

  app.all("*", render);

  return app;
}

export default createSiteApp();
